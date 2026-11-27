package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"golang.org/x/crypto/acme"
	"net/http"
	"strings"
	"time"
)

type httpChallenge struct{ Host, Response string }

func (a *Server) serveChallenge(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
		return false
	}
	if r.TLS == nil {
		if v, ok := a.challenges.Load(r.URL.Path); ok {
			c := v.(httpChallenge)
			if strings.EqualFold(strings.TrimSuffix(strings.Split(r.Host, ":")[0], "."), c.Host) {
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, c.Response)
				return true
			}
		}
	}
	http.NotFound(w, r)
	return true
}
func (a *Server) accountKey(directory string) (*ecdsa.PrivateKey, error) {
	a.certMu.Lock()
	defer a.certMu.Unlock()
	var raw string
	err := a.store.DB.QueryRow("SELECT private_key FROM acme_accounts WHERE directory=?", directory).Scan(&raw)
	if err == nil {
		b, _ := pem.Decode([]byte(raw))
		if b == nil {
			return nil, fmt.Errorf("invalid stored account key")
		}
		return x509.ParseECPrivateKey(b.Bytes)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	_, err = a.store.DB.Exec("INSERT INTO acme_accounts VALUES(?,?)", directory, string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})))
	return key, err
}
func (a *Server) issue(ctx context.Context, c Certificate) (Certificate, error) {
	s := a.acmeSettings()
	if s.Email == "" {
		return c, fmt.Errorf("ACME email is not configured")
	}
	key, err := a.accountKey(s.DirectoryURL)
	if err != nil {
		return c, err
	}
	client := &acme.Client{Key: key, DirectoryURL: s.DirectoryURL}
	_, err = client.Register(ctx, &acme.Account{Contact: []string{"mailto:" + s.Email}}, acme.AcceptTOS)
	if err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return c, err
	}
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(c.Domains...))
	if err != nil {
		return c, err
	}
	orderURL := order.URI
	for _, u := range order.AuthzURLs {
		auth, err := client.GetAuthorization(ctx, u)
		if err != nil {
			return c, err
		}
		if auth.Status == acme.StatusValid {
			continue
		}
		var challenge *acme.Challenge
		for _, ch := range auth.Challenges {
			if ch.Type == "http-01" {
				challenge = ch
				break
			}
		}
		if challenge == nil {
			return c, fmt.Errorf("CA did not offer HTTP-01")
		}
		response, err := client.HTTP01ChallengeResponse(challenge.Token)
		if err != nil {
			return c, err
		}
		path := client.HTTP01ChallengePath(challenge.Token)
		a.challenges.Store(path, httpChallenge{Host: auth.Identifier.Value, Response: response})
		defer a.challenges.Delete(path)
		if _, err = client.Accept(ctx, challenge); err != nil {
			return c, err
		}
		if _, err = client.WaitAuthorization(ctx, u); err != nil {
			return c, err
		}
	}
	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		return c, err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return c, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: c.Domains}, leafKey)
	if err != nil {
		return c, err
	}
	chain, err := finalizeCertificate(ctx, client, orderURL, order.FinalizeURL, csr)
	if err != nil {
		return c, err
	}
	var certPEM []byte
	for _, der := range chain {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	der, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		return c, err
	}
	parsed, err := parseCertificate(string(certPEM), string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})))
	if err != nil {
		return c, err
	}
	for _, name := range c.Domains {
		if !parsed.covers(name) {
			return c, fmt.Errorf("issued certificate does not cover %s", name)
		}
	}
	c.CertPEM, c.KeyPEM, c.Issuer, c.NotBefore, c.NotAfter = parsed.CertPEM, parsed.KeyPEM, parsed.Issuer, parsed.NotBefore, parsed.NotAfter
	c.Status, c.LastError = "active", ""
	return c, nil
}

type orderClient interface {
	CreateOrderCert(context.Context, string, []byte, bool) ([][]byte, string, error)
	GetOrder(context.Context, string) (*acme.Order, error)
	WaitOrder(context.Context, string) (*acme.Order, error)
	FetchCert(context.Context, string, bool) ([][]byte, error)
}

func finalizeCertificate(ctx context.Context, client orderClient, orderURL, finalizeURL string, csr []byte) ([][]byte, error) {
	chain, _, err := client.CreateOrderCert(ctx, finalizeURL, csr, true)
	if err == nil {
		return chain, nil
	}
	// Some CAs omit Location on finalize; x/crypto then loses the order URL.
	order, lookupErr := client.GetOrder(ctx, orderURL)
	if lookupErr != nil || order.Status != acme.StatusProcessing && order.Status != acme.StatusValid {
		return nil, err
	}
	if order.Status == acme.StatusProcessing {
		order, lookupErr = client.WaitOrder(ctx, orderURL)
		if lookupErr != nil {
			return nil, lookupErr
		}
	}
	if order.Status != acme.StatusValid {
		return nil, err
	}
	return client.FetchCert(ctx, order.CertURL, true)
}
func (a *Server) startIssuance(id string) bool {
	a.certMu.Lock()
	defer a.certMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	if _, loaded := a.issuing.LoadOrStore(id, cancel); loaded {
		cancel()
		return false
	}
	c, err := a.certificate(id)
	if err != nil {
		cancel()
		a.issuing.Delete(id)
		return false
	}
	c.Status = "issuing"
	c.LastError = ""
	if a.saveCertificate(c) != nil {
		cancel()
		a.issuing.Delete(id)
		return false
	}
	go func() {
		defer a.issuing.Delete(id)
		defer cancel()
		updated, err := a.issue(ctx, c)
		a.certMu.Lock()
		if _, lookupErr := a.certificate(c.ID); lookupErr != nil {
			a.certMu.Unlock()
			return
		}
		if err != nil {
			c.Status = "failed"
			c.LastError = err.Error()
			a.saveCertificate(c)
		} else {
			a.saveCertificate(updated)
		}
		a.certMu.Unlock()
		a.refresh()
	}()
	return true
}

func (a *Server) ensurePreviewCertificate(previewID, workspace, host string) {
	if !domainPattern.MatchString(host) {
		return
	}
	id := "preview:" + previewID + ":" + host
	a.certMu.Lock()
	p, err := a.store.Get(previewID)
	if err != nil || p.Kind != "previews" || p.Active == "" {
		a.certMu.Unlock()
		return
	}
	if project, err := a.store.Get(p.ProjectID); err == nil && project.Kind == "projects" {
		workspace = projectWorkspace(project)
	}
	if _, err := a.certificate(id); err == nil {
		a.certMu.Unlock()
		return
	}
	now := time.Now().UTC()
	c := Certificate{ID: id, PreviewID: previewID, WorkspaceID: workspace, Domains: []string{host}, Source: "acme", Status: "pending", AutoRenew: true, Created: now}
	err = a.saveCertificate(c)
	a.certMu.Unlock()
	if err == nil {
		a.startIssuance(id)
	}
}
func renewalDue(c Certificate, now time.Time) bool {
	return c.Source == "acme" && c.AutoRenew && c.Status != "issuing" && (c.NotAfter.IsZero() || !c.NotAfter.After(now.Add(30*24*time.Hour)))
}
func (a *Server) renewCertificates(now time.Time) {
	for _, c := range a.certificates("") {
		a.certMu.Lock()
		current, err := a.certificate(c.ID)
		if err != nil {
			a.certMu.Unlock()
			continue
		}
		c = current
		if !c.NotAfter.IsZero() && !now.Before(c.NotAfter) && c.Status != "issuing" {
			c.Status = "expired"
			a.saveCertificate(c)
		}
		a.certMu.Unlock()
		if renewalDue(c, now) {
			a.startIssuance(c.ID)
		}
	}
	a.refresh()
}
func (a *Server) watchCertificates(ctx context.Context) {
	for _, c := range a.certificates("") {
		if c.Status == "issuing" || c.Status == "pending" {
			c.Status = "failed"
			c.LastError = "issuance interrupted by restart"
			a.saveCertificate(c)
			if c.Source == "acme" {
				a.startIssuance(c.ID)
			}
		}
	}
	a.renewCertificates(time.Now())
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			a.renewCertificates(now)
		}
	}
}
