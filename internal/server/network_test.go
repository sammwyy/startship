package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sammwyy/startship/internal/netfw"
)

type networkTestExecutor struct {
	fail     bool
	restores int
	missing  bool
}

func (f *networkTestExecutor) LookPath(s string) (string, error) {
	if f.missing {
		return "", errors.New("missing ipset")
	}
	return s, nil
}
func (f *networkTestExecutor) Run(input, name string, args ...string) ([]byte, error) {
	if f.fail {
		return nil, errors.New("kernel failure")
	}
	if name == "ipset" && args[0] == "restore" {
		f.restores++
	}
	return nil, nil
}

func TestNetworkAPI(t *testing.T) {
	f := newRBACFixture(t)
	const path = "system/network-firewall"
	ex := &networkTestExecutor{}
	f.a.network.manager = &netfw.Manager{Executor: ex, Ports: []string{"18081", "18443"}}
	for _, cookie := range []*http.Cookie{f.alice, f.bob} {
		accountRequest(t, f.h, "GET", path, "", cookie, 403)
		accountRequest(t, f.h, "POST", path+"/rules", `{"kind":"ip","value":"127.0.0.2"}`, cookie, 403)
		accountRequest(t, f.h, "DELETE", path+"/rules/x", "", cookie, 403)
	}
	w := accountRequest(t, f.h, "GET", path, "", f.root, 200)
	if !strings.Contains(w.Body.String(), `"enabled":false`) || !strings.Contains(w.Body.String(), `"rules":[]`) {
		t.Fatal(w.Body)
	}
	w = accountRequest(t, f.h, "POST", path+"/rules", `{}`, f.root, 409)
	if !strings.Contains(w.Body.String(), networkDisabled) {
		t.Fatal(w.Body)
	}
	accountRequest(t, f.h, "DELETE", path+"/rules/x", "", f.root, 409)
	f.a.network.enabled = true
	for _, body := range []string{`{`, `{"kind":"ip","value":"oops"}`, `{"kind":"ip","value":"127.0.0.2","ports":"bad"}`, `{"kind":"country","value":"US"}`, `{"kind":"ip","value":"127.0.0.2","ttl_seconds":-1}`, `{"kind":"ip","value":"127.0.0.2","ttl_seconds":31536001}`, `{"kind":"ip","value":"127.0.0.2","expires_at":"2000-01-01T00:00:00Z"}`} {
		accountRequest(t, f.h, "POST", path+"/rules", body, f.root, 400)
	}
	rule := decodeBody[netfw.Rule](t, accountRequest(t, f.h, "POST", path+"/rules", `{"kind":"cidr","value":"192.0.2.1/24","ports":"router","ttl_seconds":30,"note":"test"}`, f.root, 200).Body.Bytes())
	if rule.ID == "" || rule.Value != "192.0.2.0/24" || rule.Note != "test" || rule.ExpiresAt == nil || time.Until(*rule.ExpiresAt) < 25*time.Second {
		t.Fatal(rule)
	}
	f.a.network.manager.Country = func(code string) ([]string, error) { return []string{"203.0.113.0/24"}, nil }
	country := decodeBody[netfw.Rule](t, accountRequest(t, f.h, "POST", path+"/rules", `{"kind":"country","value":"us"}`, f.root, 200).Body.Bytes())
	if country.Value != "US" || country.Ports != "all" {
		t.Fatal(country)
	}
	rules, err := f.a.networkRules()
	if err != nil || len(rules) != 2 {
		t.Fatal(rules, err)
	}
	// A second connection sees durable rules through the migration path.
	var seq int
	var name, dbpath string
	if err = f.a.store.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &dbpath); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	other := &Server{store: reopened}
	persisted, err := other.networkRules()
	reopened.DB.Close()
	if err != nil || len(persisted) != 2 {
		t.Fatal(persisted, err)
	}
	accountRequest(t, f.h, "DELETE", path+"/rules/unknown", "", f.root, 404)
	accountRequest(t, f.h, "DELETE", path+"/rules/"+rule.ID, "", f.root, 200)
	var audits int
	if err = f.a.store.DB.QueryRow("SELECT count(*) FROM audit_log WHERE action LIKE 'network.%' AND scope='platform'").Scan(&audits); err != nil || audits != 3 {
		t.Fatal(audits, err)
	}
	// Socket authorization uses the same platform permission checks.
	socketPath := filepath.Join(t.TempDir(), "network.sock")
	srv, err := f.a.serveSocket(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}}
	resp, err := client.Get("http://startship/api/" + path)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.Status)
	}
	// Expiration is purged and audited, and reconciles the kernel.
	expired := time.Now().Add(-time.Second)
	country.ExpiresAt = &expired
	b := firewallJSON(t, country)
	if _, err = f.a.store.DB.Exec("UPDATE network_rules SET body=? WHERE id=?", b, country.ID); err != nil {
		t.Fatal(err)
	}
	accountRequest(t, f.h, "GET", path, "", f.root, 200)
	rules, err = f.a.networkRules()
	if err != nil || len(rules) != 0 {
		t.Fatal(rules, err)
	}
	if ex.restores < 4 {
		t.Fatal("not reconciled", ex.restores)
	}
	ex.fail = true
	accountRequest(t, f.h, "POST", path+"/rules", `{"kind":"ip","value":"127.0.0.2"}`, f.root, 500)
	rules, err = f.a.networkRules()
	if err != nil || len(rules) != 0 {
		t.Fatal("failed mutation persisted", rules, err)
	}
	w = accountRequest(t, f.h, "GET", path, "", f.root, 200)
	if !strings.Contains(w.Body.String(), `"available":false`) || !strings.Contains(w.Body.String(), "kernel failure") {
		t.Fatal(w.Body)
	}
	ex.fail = false
	ex.missing = true
	w = accountRequest(t, f.h, "GET", path, "", f.root, 200)
	if !strings.Contains(w.Body.String(), "missing ipset") {
		t.Fatal(w.Body)
	}
}

func TestNetworkAuditFailureDoesNotPublish(t *testing.T) {
	f := newRBACFixture(t)
	ex := &networkTestExecutor{}
	f.a.network.enabled = true
	f.a.network.manager = &netfw.Manager{Executor: ex, Ports: []string{"80"}}
	if _, err := f.a.store.DB.Exec("CREATE TRIGGER reject_network BEFORE INSERT ON audit_log WHEN NEW.action LIKE 'network.%' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	accountRequest(t, f.h, "POST", "system/network-firewall/rules", `{"kind":"ip","value":"127.0.0.2"}`, f.root, 500)
	rules, err := f.a.networkRules()
	if err != nil || len(rules) != 0 || ex.restores != 0 {
		t.Fatal(rules, err, ex.restores)
	}
}
