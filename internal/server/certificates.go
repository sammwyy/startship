package server

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"github.com/sammwyy/startship/internal/routing"
	"golang.org/x/crypto/acme"
	"net/http"
	"os"
	"strings"
	"time"
)

type Certificate struct {
	PreviewID   string    `json:"preview_id,omitempty"`
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Domains     []string  `json:"domains"`
	Source      string    `json:"source"`
	CertPEM     string    `json:"cert_pem"`
	KeyPEM      string    `json:"-"`
	Issuer      string    `json:"issuer"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	Status      string    `json:"status"`
	LastError   string    `json:"last_error"`
	AutoRenew   bool      `json:"auto_renew"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
	CreatedBy   string    `json:"created_by"`
}
type ACMESettings struct {
	Email        string `json:"email"`
	DirectoryURL string `json:"directory_url"`
}

func (a *Server) certificates(ws string) []Certificate {
	out := []Certificate{}
	rows, err := a.store.DB.Query("SELECT body,private_key FROM certificates WHERE (?='' OR workspace_id=?) ORDER BY id", ws, ws)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b, k string
		var c Certificate
		if rows.Scan(&b, &k) == nil && json.Unmarshal([]byte(b), &c) == nil {
			c.KeyPEM = k
			out = append(out, c)
		}
	}
	return out
}
func (a *Server) certificate(id string) (Certificate, error) {
	var b string
	var c Certificate
	err := a.store.DB.QueryRow("SELECT body,private_key FROM certificates WHERE id=?", id).Scan(&b, &c.KeyPEM)
	if err == nil {
		err = json.Unmarshal([]byte(b), &c)
	}
	return c, err
}
func (a *Server) saveCertificate(c Certificate) error {
	c.Updated = time.Now().UTC()
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = a.store.DB.Exec("INSERT INTO certificates VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body,private_key=excluded.private_key", c.ID, c.WorkspaceID, string(b), c.KeyPEM)
	if err == nil {
		a.store.Notify(Record{ID: c.ID, Kind: "certificates", WorkspaceID: c.WorkspaceID})
	}
	return err
}
func parseCertificate(cert, key string) (Certificate, error) {
	c := Certificate{CertPEM: cert, KeyPEM: key}
	pair, err := tls.X509KeyPair([]byte(cert), []byte(key))
	if err != nil {
		return c, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return c, err
	}
	if len(leaf.DNSNames) == 0 {
		return c, fmt.Errorf("certificate requires DNS SANs")
	}
	if leaf.IsCA {
		return c, fmt.Errorf("CA certificates cannot serve routes")
	}
	c.Domains = leaf.DNSNames
	c.Issuer = leaf.Issuer.String()
	c.NotBefore = leaf.NotBefore
	c.NotAfter = leaf.NotAfter
	c.Status = "active"
	if time.Now().Before(c.NotBefore) || !time.Now().Before(c.NotAfter) {
		return c, fmt.Errorf("certificate is not currently valid")
	}
	return c, nil
}
func (c Certificate) covers(host string) bool {
	host = routing.NormalizeHost(host)
	for _, d := range c.Domains {
		d = routing.NormalizeHost(d)
		if d == host {
			return true
		}
		if strings.HasPrefix(d, "*.") && strings.HasSuffix(host, d[1:]) && !strings.Contains(strings.TrimSuffix(host, d[1:]), ".") && len(host) > len(d)-1 {
			return true
		}
	}
	return false
}
func (c Certificate) usable() bool {
	return c.CertPEM != "" && !time.Now().Before(c.NotBefore) && time.Now().Before(c.NotAfter) && (c.Status == "active" || c.Status == "issuing" || c.Status == "failed")
}
func (a *Server) acmeSettings() ACMESettings {
	s := ACMESettings{Email: os.Getenv("STARTSHIP_ACME_EMAIL"), DirectoryURL: os.Getenv("STARTSHIP_ACME_DIRECTORY")}
	if s.DirectoryURL == "" {
		s.DirectoryURL = acme.LetsEncryptURL
	}
	var b string
	if a.store.DB.QueryRow("SELECT value FROM settings WHERE key='acme'").Scan(&b) == nil {
		json.Unmarshal([]byte(b), &s)
	}
	return s
}
func (a *Server) certificateDomainsAllowed(ws string, domains []string, admin bool) bool {
	if len(domains) == 0 {
		return false
	}
	for _, d := range domains {
		d = routing.NormalizeHost(d)
		if admin {
			continue
		}
		if strings.HasPrefix(d, "*.") {
			ok := false
			for _, claim := range a.domains(ws) {
				if claim.Subdomains && (strings.TrimPrefix(d, "*.") == claim.Name || strings.HasSuffix(strings.TrimPrefix(d, "*."), "."+claim.Name)) {
					ok = true
				}
			}
			if !ok {
				return false
			}
		} else if !a.routeAllowed(ws, d) {
			return false
		}
	}
	return true
}
func (a *Server) certificatesAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) bool {
	if len(p) == 3 && p[1] == "system" && p[2] == "acme" {
		if !x.admin {
			forbidden(w)
			return true
		}
		switch r.Method {
		case "GET":
			reply(w, 200, a.acmeSettings())
		case "PUT":
			var s ACMESettings
			if decode(w, r, &s) != nil || s.Email == "" || !strings.Contains(s.Email, "@") || !strings.HasPrefix(s.DirectoryURL, "https://") {
				fail(w, 400, "email and HTTPS directory_url required")
				return true
			}
			b, _ := json.Marshal(s)
			if _, err := a.store.DB.Exec("INSERT INTO settings VALUES('acme',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(b)); err != nil {
				fail(w, 500, "save failed")
			} else {
				a.audit(r.Context(), uid, "acme.settings", "ACME settings", scopePlatform, "", nil)
				reply(w, 200, s)
			}
		default:
			fail(w, 405, "method not allowed")
		}
		return true
	}
	if len(p) < 4 || p[1] != "workspaces" || p[3] != "certificates" {
		return false
	}
	ws, err := a.store.Get(p[2])
	if err != nil || ws.Kind != "workspaces" || !x.visible(ws) {
		fail(w, 404, "not found")
		return true
	}
	if !x.at(ws.ID, "")["workspace.domains"] {
		forbidden(w)
		return true
	}
	if len(p) == 4 {
		switch r.Method {
		case "GET":
			reply(w, 200, map[string]any{"certificates": a.certificates(ws.ID)})
		case "POST":
			var v struct {
				Source    string   `json:"source"`
				CertPEM   string   `json:"cert_pem"`
				KeyPEM    string   `json:"key_pem"`
				Domains   []string `json:"domains"`
				AutoRenew *bool    `json:"auto_renew"`
			}
			if decode(w, r, &v) != nil {
				fail(w, 400, "invalid JSON")
				return true
			}
			c := Certificate{Source: v.Source}
			if v.Source == "upload" {
				c, err = parseCertificate(v.CertPEM, v.KeyPEM)
				c.Source = "upload"
			} else if v.Source == "acme" {
				for _, d := range v.Domains {
					d = routing.NormalizeHost(strings.TrimSpace(d))
					if !domainPattern.MatchString(d) || strings.Contains(d, "*") {
						err = fmt.Errorf("HTTP-01 requires DNS names without wildcards")
						break
					}
					c.Domains = append(c.Domains, d)
				}
				c.Status = "pending"
				c.AutoRenew = v.AutoRenew == nil || *v.AutoRenew
				if a.acmeSettings().Email == "" {
					err = fmt.Errorf("configure ACME account email first")
				}
			} else {
				err = fmt.Errorf("source must be upload or acme")
			}
			if err != nil {
				fail(w, 400, err.Error())
				return true
			}
			if !a.certificateDomainsAllowed(ws.ID, c.Domains, x.admin) {
				fail(w, 403, "certificate names are not permitted by workspace domains")
				return true
			}
			c.ID = randomID()
			c.WorkspaceID = ws.ID
			c.Created = time.Now().UTC()
			c.Updated = c.Created
			c.CreatedBy = uid
			if err = a.saveCertificate(c); err != nil {
				fail(w, 500, "save failed")
				return true
			}
			if c.Source == "acme" {
				a.startIssuance(c.ID)
			}
			a.refresh()
			a.audit(r.Context(), uid, "certificate.create", c.ID, scopeWorkspace, ws.ID, nil)
			reply(w, 201, c)
		default:
			fail(w, 405, "method not allowed")
		}
		return true
	}
	c, err := a.certificate(p[4])
	if err != nil || c.WorkspaceID != ws.ID {
		fail(w, 404, "not found")
		return true
	}
	switch {
	case len(p) == 5 && r.Method == "GET":
		reply(w, 200, c)
	case len(p) == 5 && r.Method == "DELETE":
		a.certMu.Lock()
		defer a.certMu.Unlock()
		if _, busy := a.issuing.Load(c.ID); busy {
			fail(w, 409, "certificate is issuing")
			return true
		}
		for _, route := range a.publicRouteRecords() {
			if route.CertificateID == c.ID {
				fail(w, 409, "certificate is bound to a route")
				return true
			}
		}
		if _, err = a.store.DB.Exec("DELETE FROM certificates WHERE id=?", c.ID); err != nil {
			fail(w, 500, "delete failed")
			return true
		}
		a.store.Notify(Record{ID: c.ID, Kind: "certificates", WorkspaceID: ws.ID})
		a.refresh()
		a.audit(r.Context(), uid, "certificate.delete", c.ID, scopeWorkspace, ws.ID, nil)
		reply(w, 200, map[string]bool{"ok": true})
	case len(p) == 6 && p[5] == "renew" && r.Method == "POST":
		if c.Source != "acme" {
			fail(w, 400, "uploaded certificates cannot be renewed")
			return true
		}
		if !a.startIssuance(c.ID) {
			fail(w, 409, "certificate is issuing")
			return true
		}
		a.audit(r.Context(), uid, "certificate.renew", c.ID, scopeWorkspace, ws.ID, nil)
		reply(w, 202, map[string]string{"status": "issuing"})
	default:
		fail(w, 405, "method not allowed")
	}
	return true
}
