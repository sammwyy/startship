package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"golang.org/x/crypto/acme"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type finalizeStub struct {
	status    string
	submitted bool
}

func (f *finalizeStub) CreateOrderCert(context.Context, string, []byte, bool) ([][]byte, string, error) {
	f.submitted = true
	return nil, "", fmt.Errorf("missing Location")
}
func (f *finalizeStub) GetOrder(context.Context, string) (*acme.Order, error) {
	return &acme.Order{Status: f.status, CertURL: "certificate"}, nil
}
func (f *finalizeStub) WaitOrder(context.Context, string) (*acme.Order, error) {
	return &acme.Order{Status: acme.StatusValid, CertURL: "certificate"}, nil
}
func (f *finalizeStub) FetchCert(context.Context, string, bool) ([][]byte, error) {
	return [][]byte{[]byte("chain")}, nil
}
func TestFinalizeWithoutLocation(t *testing.T) {
	for _, status := range []string{acme.StatusProcessing, acme.StatusValid, acme.StatusReady, acme.StatusInvalid} {
		f := &finalizeStub{status: status}
		chain, err := finalizeCertificate(context.Background(), f, "order", "finalize", nil)
		want := status == acme.StatusProcessing || status == acme.StatusValid
		if (err == nil) != want || !f.submitted || want && string(chain[0]) != "chain" {
			t.Fatal(status, err)
		}
	}
}

func testPEM(t *testing.T, names []string, before, after time.Time) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: names[0]}, DNSNames: names, NotBefore: before, NotAfter: after, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	k, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: k}))
}
func TestCertificateUploadValidation(t *testing.T) {
	now := time.Now()
	cert, key := testPEM(t, []string{"example.com", "*.example.com"}, now.Add(-time.Hour), now.Add(time.Hour))
	c, err := parseCertificate(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Domains) != 2 || c.Issuer == "" || c.Status != "active" {
		t.Fatal(c)
	}
	_, other := testPEM(t, []string{"example.com"}, now.Add(-time.Hour), now.Add(time.Hour))
	expired, expiredKey := testPEM(t, []string{"example.com"}, now.Add(-2*time.Hour), now.Add(-time.Hour))
	future, futureKey := testPEM(t, []string{"example.com"}, now.Add(time.Hour), now.Add(2*time.Hour))
	for _, v := range [][2]string{{"", ""}, {"garbage", key}, {cert, other}, {expired, expiredKey}, {future, futureKey}} {
		if _, err := parseCertificate(v[0], v[1]); err == nil {
			t.Fatal("accepted invalid upload")
		}
	}
	for host, want := range map[string]bool{"example.com": true, "www.example.com": true, "deep.www.example.com": false, "badexample.com": false, "": false} {
		if c.covers(host) != want {
			t.Fatal(host, want)
		}
	}
}
func TestCertificateAPIAndLive(t *testing.T) {
	f := newRBACFixture(t)
	base := "workspaces/" + f.workspace.ID + "/certificates"
	cert, key := testPEM(t, []string{"example.com", "*.example.com"}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	body, _ := json.Marshal(map[string]any{"source": "upload", "cert_pem": cert, "key_pem": key, "auto_renew": true})
	accountRequest(t, f.h, "GET", base, "", f.bob, 404)
	f.assign(t, f.alice, f.bobID, "contributor", scopeWorkspace, f.workspace.ID, 201)
	accountRequest(t, f.h, "GET", base, "", f.bob, 403)
	accountRequest(t, f.h, "POST", base, string(body), f.alice, 403)
	accountRequest(t, f.h, "POST", "workspaces/"+f.workspace.ID+"/domains", `{"name":"example.com","subdomains":true}`, f.alice, 201)
	f.a.initLive()
	w := accountRequest(t, f.h, "POST", base, string(body), f.alice, 201)
	if strings.Contains(w.Body.String(), "PRIVATE KEY") || strings.Contains(w.Body.String(), "key_pem") {
		t.Fatal("private key leaked")
	}
	c := decodeBody[Certificate](t, w.Body.Bytes())
	if c.AutoRenew || c.Source != "upload" {
		t.Fatal(c)
	}
	stored, err := f.a.certificate(c.ID)
	if err != nil || stored.KeyPEM != key {
		t.Fatal(err)
	}
	accountRequest(t, f.h, "GET", base+"/"+c.ID, "", f.alice, 200)
	accountRequest(t, f.h, "POST", base+"/"+c.ID+"/renew", "{}", f.alice, 400)
	accountRequest(t, f.h, "POST", base, `{"source":"acme","domains":["*.example.com"]}`, f.root, 400)
	accountRequest(t, f.h, "POST", base, `{"source":"unknown"}`, f.root, 400)
	accountRequest(t, f.h, "GET", "system/acme", "", f.alice, 403)
	accountRequest(t, f.h, "PUT", "system/acme", `{"email":"admin@example.com","directory_url":"https://acme.example.com/directory"}`, f.root, 200)
	accountRequest(t, f.h, "PUT", "system/acme", `{"email":"bad","directory_url":"http://bad"}`, f.root, 400)
	data, _, _, why := f.a.liveData(context.Background(), f.aliceID, &liveSub{Topic: "certificates", WorkspaceID: f.workspace.ID})
	if why != "" || len(data.(map[string]any)["certificates"].([]Certificate)) != 1 {
		t.Fatal(why, data)
	}
	_, _, _, why = f.a.liveData(context.Background(), f.bobID, &liveSub{Topic: "certificates", WorkspaceID: f.workspace.ID})
	if why != "forbidden" {
		t.Fatal(why)
	}
	f.a.store.Save(Record{ID: "web", Kind: "apps", Owner: f.aliceID, ProjectID: f.project.ID})
	route := decodeBody[Record](t, accountRequest(t, f.h, "POST", "routes", `{"project_id":"`+f.project.ID+`","app_id":"web","host":"www.example.com","certificate_id":"`+c.ID+`"}`, f.alice, 200).Body.Bytes())
	accountRequest(t, f.h, "DELETE", base+"/"+c.ID, "", f.alice, 409)
	accountRequest(t, f.h, "PATCH", "routes/"+route.ID, `{"host":"elsewhere.com"}`, f.root, 400)
	accountRequest(t, f.h, "DELETE", "routes/"+route.ID, "", f.alice, 200)
	accountRequest(t, f.h, "DELETE", base+"/"+c.ID, "", f.alice, 200)
	accountRequest(t, f.h, "GET", base+"/"+c.ID, "", f.alice, 404)
}
func TestCertificateSelection(t *testing.T) {
	a := testServer(t)
	if _, err := a.tlsCertificate(&tls.ClientHelloInfo{ServerName: "example.com"}); err == nil {
		t.Fatal("missing certificate accepted")
	}
	cert, key := testPEM(t, []string{"*.example.com"}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	c, err := parseCertificate(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = "a"
	c.WorkspaceID = "ws"
	a.saveCertificate(c)
	cert, key = testPEM(t, []string{"www.example.com"}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	bound, _ := parseCertificate(cert, key)
	bound.ID = "b"
	bound.WorkspaceID = "ws"
	a.saveCertificate(bound)
	a.store.Save(Record{ID: "route", Kind: "routes", Host: "www.example.com", CertificateID: "b"})
	if got := a.storedCertificate("WWW.EXAMPLE.COM."); got == nil || got.ID != "b" {
		t.Fatal(got)
	}
	if _, err := a.tlsCertificate(&tls.ClientHelloInfo{ServerName: "other.example.com"}); err != nil {
		t.Fatal(err)
	}
	if a.storedCertificate("deep.other.example.com") != nil {
		t.Fatal("wildcard overmatched")
	}
	bound.NotAfter = time.Now().Add(-time.Hour)
	a.saveCertificate(bound)
	if got := a.storedCertificate("www.example.com"); got == nil || got.ID != "a" {
		t.Fatal(got)
	}
	called := false
	a.legacyCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) { called = true; return &tls.Certificate{}, nil }
	a.tlsCertificate(&tls.ClientHelloInfo{ServerName: "legacy.test"})
	if !called {
		t.Fatal("legacy fallback not used")
	}
}
func TestChallengeAndHTTPSRedirect(t *testing.T) {
	a := testServer(t)
	a.challenges.Store("/.well-known/acme-challenge/token", httpChallenge{Host: "example.com", Response: "proof"})
	a.targets = []Target{{Route: Record{Host: "example.com", PathPrefix: "/", ForceHTTPS: true}}}
	for _, v := range []struct {
		path, host string
		status     int
		body       string
	}{{"/.well-known/acme-challenge/token", "example.com", 200, "proof"}, {"/.well-known/acme-challenge/token", "wrong.com", 404, "404"}, {"/.well-known/acme-challenge/missing", "example.com", 404, "404"}, {"/hello?x=1", "example.com", 301, ""}, {"/", "unknown.com", 404, "no route"}} {
		r := httptest.NewRequest("GET", "http://"+v.host+v.path, nil)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != v.status || !strings.Contains(w.Body.String(), v.body) {
			t.Fatal(w.Code, w.Body.String())
		}
		if v.status == 301 && w.Header().Get("Location") != "https://example.com/hello?x=1" {
			t.Fatal(w.Header())
		}
	}
}
func TestRenewalSelection(t *testing.T) {
	now := time.Now()
	c := Certificate{Source: "acme", AutoRenew: true, Status: "active", NotAfter: now.Add(29 * 24 * time.Hour)}
	if !renewalDue(c, now) {
		t.Fatal("due certificate missed")
	}
	c.NotAfter = now.Add(31 * 24 * time.Hour)
	if renewalDue(c, now) {
		t.Fatal("early renewal")
	}
	c.NotAfter = now.Add(-time.Hour)
	c.Source = "upload"
	if renewalDue(c, now) {
		t.Fatal("upload renewal")
	}
	c.Source = "acme"
	c.Status = "issuing"
	if renewalDue(c, now) {
		t.Fatal("duplicate renewal")
	}
	c.Status = "failed"
	if !renewalDue(c, now) {
		t.Fatal("failure not retried")
	}
	c.AutoRenew = false
	if renewalDue(c, now) {
		t.Fatal("disabled renewal")
	}
	a := testServer(t)
	c.ID = "expired"
	c.WorkspaceID = "ws"
	a.saveCertificate(c)
	a.renewCertificates(now)
	got, _ := a.certificate(c.ID)
	if got.Status != "expired" {
		t.Fatal(got)
	}
}
