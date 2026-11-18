package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/sammwyy/startship/internal/firewall"
	"github.com/sammwyy/startship/internal/router"
)

func firewallJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type firewallTestGeo struct{}

func (firewallTestGeo) Country(net.IP) string { return "US" }
func firewallRoute(t *testing.T, a *Server, ws, project string) {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "upstream") }))
	t.Cleanup(up.Close)
	u, _ := url.Parse(up.URL)
	port, _ := strconv.Atoi(u.Port())
	for _, r := range []Record{{ID: "fw-app", Kind: "apps", ProjectID: project, Active: "fw-deployment", Port: port}, {ID: "fw-deployment", Kind: "deployments", IP: u.Hostname(), Status: "running"}, {ID: "fw-route", Kind: "routes", ProjectID: project, AppID: "fw-app", Host: "firewall.test", PathPrefix: "/"}} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	a.refresh()
}
func firewallProxy(a *Server, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "http://firewall.test"+path, nil))
	return w
}

func TestFirewallAPICRUDAndRouter(t *testing.T) {
	f := newRBACFixture(t)
	base := "workspaces/" + f.workspace.ID + "/firewall"
	call := func(method, path, body string, status int) *httptest.ResponseRecorder {
		return accountRequest(t, f.h, method, base+path, body, f.alice, status)
	}
	p := decodeBody[firewall.Policy](t, call("GET", "", "", 200).Body.Bytes())
	if p.Mode != "off" {
		t.Fatal(p)
	}
	lists := decodeBody[[]firewall.List](t, call("GET", "/lists", "", 200).Body.Bytes())
	if len(lists) != 1 || lists[0].ID != "banned" || len(lists[0].Entries) != 0 {
		t.Fatal(lists)
	}
	catalog := decodeBody[firewall.CatalogInfo](t, accountRequest(t, f.h, "GET", "firewall/catalog", "", f.alice, 200).Body.Bytes())
	if catalog.GeoIP || len(catalog.ManagedRules) != 7 {
		t.Fatal(catalog)
	}
	f.a.routerOptions.GeoIP = firewallTestGeo{}
	catalog = decodeBody[firewall.CatalogInfo](t, accountRequest(t, f.h, "GET", "firewall/catalog", "", f.alice, 200).Body.Bytes())
	if !catalog.GeoIP {
		t.Fatal("GeoIP availability missing")
	}
	f.a.routerOptions.GeoIP = nil
	p.Mode = "enforce"
	p.Managed = map[string]firewall.ManagedRule{}
	p = decodeBody[firewall.Policy](t, call("PUT", "", firewallJSON(t, p), 200).Body.Bytes())
	if p.Updated == "" || p.UpdatedBy != f.aliceID || len(p.Managed) != 0 {
		t.Fatal(p)
	}
	firewallRoute(t, f.a, f.workspace.ID, f.project.ID)
	rule := firewall.Rule{ID: "deny", Name: "Deny path", Enabled: true, Match: "all", Conditions: []firewall.Condition{{Field: "path", Op: "eq", Value: "/blocked"}}, Action: firewall.Block}
	call("POST", "/rules", firewallJSON(t, rule), 200)
	if w := firewallProxy(f.a, "/blocked"); w.Code != 403 || !strings.Contains(w.Body.String(), "Blocked by Startship firewall") {
		t.Fatal(w.Code, w.Body)
	}
	if w := firewallProxy(f.a, "/normal"); w.Code != 200 || w.Body.String() != "upstream" {
		t.Fatal(w.Code, w.Body)
	}
	events := decodeBody[[]firewall.Event](t, call("GET", "/events", "", 200).Body.Bytes())
	if len(events) != 1 || events[0].RuleID != "deny" || events[0].WorkspaceID != f.workspace.ID {
		t.Fatal(events)
	}
	second := rule
	second.ID = "second"
	second.Action = firewall.Log
	call("POST", "/rules", firewallJSON(t, second), 200)
	p = decodeBody[firewall.Policy](t, call("POST", "/rules/order", `{"ids":["second","deny"]}`, 200).Body.Bytes())
	if p.Rules[0].ID != "second" {
		t.Fatal(p.Rules)
	}
	rule.Name = "Updated"
	call("PUT", "/rules/deny", firewallJSON(t, rule), 200)
	call("DELETE", "/rules/second", "", 200)
	call("POST", "/lists", `{"id":"office","name":"Office","kind":"allow","entries":[]}`, 200)
	l := decodeBody[firewall.List](t, call("POST", "/lists/office/entries", `{"value":"203.0.113.0/24","ttl_seconds":120,"note":"temporary"}`, 200).Body.Bytes())
	if len(l.Entries) != 1 || l.Entries[0].CreatedBy != f.aliceID || l.Entries[0].ExpiresAt == nil || time.Until(*l.Entries[0].ExpiresAt) > 121*time.Second {
		t.Fatal(l)
	}
	call("DELETE", "/lists/office/entries/"+url.PathEscape("203.0.113.0/24"), "", 200)
	l.Name = "New office"
	l.Kind = "block"
	l.Entries = []firewall.Entry{}
	call("PUT", "/lists/office", firewallJSON(t, l), 200)
	call("DELETE", "/lists/office", "", 200)
	ban := decodeBody[firewall.List](t, call("POST", "/ban", `{"ip":"192.0.2.1","ttl_seconds":60}`, 200).Body.Bytes())
	if len(ban.Entries) != 1 || ban.Entries[0].ExpiresAt == nil {
		t.Fatal(ban)
	}
	if w := firewallProxy(f.a, "/normal"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	call("DELETE", "/ban/192.0.2.1", "", 200)
	if w := firewallProxy(f.a, "/normal"); w.Code != 200 {
		t.Fatal("unban", w.Code)
	}
	auto := rule
	auto.ID = "auto"
	auto.Conditions[0].Value = "/auto"
	auto.Action = firewall.Ban
	auto.BanSeconds = 60
	call("POST", "/rules", firewallJSON(t, auto), 200)
	if w := firewallProxy(f.a, "/auto"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	lists = decodeBody[[]firewall.List](t, call("GET", "/lists", "", 200).Body.Bytes())
	if len(findFirewallList(lists, "banned").Entries) != 1 {
		t.Fatal(lists)
	}
	call("DELETE", "/ban/192.0.2.1", "", 200)
	if w := firewallProxy(f.a, "/normal"); w.Code != 200 {
		t.Fatal("state ban survived unban", w.Code)
	}
	call("DELETE", "/rules/auto", "", 200)
	call("DELETE", "/rules/deny", "", 200)
	rule.ID = "encoded/rule"
	call("POST", "/rules", firewallJSON(t, rule), 200)
	call("PUT", "/rules/"+url.PathEscape(rule.ID), firewallJSON(t, rule), 200)
	call("DELETE", "/rules/"+url.PathEscape(rule.ID), "", 200)
	call("POST", "/ban", `{"ip":"2001:db8::1"}`, 200)
	lists = decodeBody[[]firewall.List](t, call("GET", "/lists", "", 200).Body.Bytes())
	if e := findFirewallList(lists, "banned").Entries[0]; e.ExpiresAt != nil {
		t.Fatal(e)
	}
	call("DELETE", "/ban/2001:db8::1", "", 200)
	past := time.Now().Add(-time.Minute)
	l = firewall.List{ID: "expired", Name: "Expired", Kind: "block", Entries: []firewall.Entry{{Value: "203.0.113.1", ExpiresAt: &past}}}
	call("POST", "/lists", firewallJSON(t, l), 200)
	lists = decodeBody[[]firewall.List](t, call("GET", "/lists", "", 200).Body.Bytes())
	if len(findFirewallList(lists, "expired").Entries) != 0 {
		t.Fatal(lists)
	}
	var stored string
	if err := f.a.store.DB.QueryRow("SELECT body FROM firewall_lists WHERE workspace_id=? AND id='expired'", f.workspace.ID).Scan(&stored); err != nil || strings.Contains(stored, "203.0.113.1") {
		t.Fatal(stored, err)
	}
	rows, err := f.a.store.DB.Query("SELECT DISTINCT action FROM audit_log WHERE scope_id=? AND action LIKE 'firewall.%'", f.workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	actions := []string{}
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for _, want := range []string{"firewall.policy.update", "firewall.rule.create", "firewall.rule.update", "firewall.rule.delete", "firewall.rule.order", "firewall.list.create", "firewall.list.update", "firewall.list.delete", "firewall.list.expire", "firewall.entry.create", "firewall.entry.delete", "firewall.ban.create", "firewall.ban.delete"} {
		if !slices.Contains(actions, want) {
			t.Fatal("missing audit", want, actions)
		}
	}
}

func TestFirewallAPIValidationAndPermissions(t *testing.T) {
	f := newRBACFixture(t)
	base := "workspaces/" + f.workspace.ID + "/firewall"
	for _, tc := range []struct {
		method, path, body, want string
		status                   int
	}{
		{"PUT", "", `{"mode":"bad"}`, "mode: must be off, monitor or enforce", 400},
		{"PUT", "", `{"mode":"off","limits":{"concurrent_per_ip":-1}}`, "limits.concurrent_per_ip", 400},
		{"PUT", "", `{"mode":"off"} {}`, "invalid JSON", 400},
		{"POST", "/rules", `{"id":"bad","match":"all","action":"block","conditions":[{"field":"foo","op":"eq","value":"x"}]}`, `rules[0].conditions[0]: unknown field`, 400},
		{"POST", "/rules/order", `{"ids":["missing"]}`, "every rule exactly once", 400},
		{"POST", "/lists", `{"id":"invalid","name":"Invalid","kind":"block","entries":[{"value":"garbage"}]}`, "invalid IP or CIDR", 400},
		{"POST", "/lists", `{"id":"invalid","name":"Invalid","kind":"invalid"}`, "kind must be", 400},
		{"POST", "/lists/banned/entries", `{"value":"example.com"}`, "invalid IP or CIDR", 400},
		{"POST", "/lists/banned/entries", `{"value":"192.0.2.1","ttl_seconds":-1}`, "ttl_seconds", 400},
		{"POST", "/ban", `{"ip":"192.0.2.0/24"}`, "invalid IP", 400},
		{"POST", "/ban", `{"ip":"192.0.2.1","ttl_seconds":31536001}`, "ttl_seconds", 400},
		{"DELETE", "/lists/banned", "", "cannot be deleted", 400},
		{"PUT", "/lists/banned", `{"name":"Banned","kind":"allow","entries":[]}`, "banned must be a block list", 400},
		{"DELETE", "/rules/missing", "", "rule not found", 404},
		{"DELETE", "/ban/192.0.2.1", "", "ban not found", 404},
		{"DELETE", "/lists/banned/entries/192.0.2.1", "", "entry not found", 404},
		{"POST", "/lists/missing/entries", `{"value":"192.0.2.1"}`, "list not found", 404},
		{"PATCH", "", `{"mode":"off"}`, "method not allowed", 405},
	} {
		w := accountRequest(t, f.h, tc.method, base+tc.path, tc.body, f.alice, tc.status)
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Fatal(tc.path, w.Body)
		}
	}
	f.assign(t, f.alice, f.bobID, "contributor", scopeWorkspace, f.workspace.ID, 201)
	for _, path := range []string{"", "/lists", "/events"} {
		accountRequest(t, f.h, "GET", base+path, "", f.bob, 200)
	}
	for _, tc := range []struct{ method, path, body string }{{"PUT", "", `{"mode":"off"}`}, {"POST", "/rules", `{}`}, {"POST", "/lists", `{}`}, {"POST", "/ban", `{"ip":"192.0.2.1"}`}, {"DELETE", "/ban/192.0.2.1", ""}} {
		accountRequest(t, f.h, tc.method, base+tc.path, tc.body, f.bob, 403)
	}
	if _, err := f.a.store.DB.Exec("UPDATE role_assignments SET role_id='member' WHERE user_id=? AND scope_id=?", f.bobID, f.workspace.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "/lists", "/events"} {
		accountRequest(t, f.h, "GET", base+path, "", f.bob, 403)
	}
	accountRequest(t, f.h, "POST", base+"/ban", `{"ip":"192.0.2.1"}`, f.bob, 403)
	accountRequest(t, f.h, "GET", "workspaces/"+f.a.store.List("workspaces", f.rootID)[0].ID+"/firewall", "", f.alice, 404)
}

func TestFirewallPersistenceAndSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.db")
	s, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.DB.Close() }()
	a := &Server{store: s}
	uid, err := a.createUser("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	ws := s.List("workspaces", uid)[0]
	socket := filepath.Join(t.TempDir(), "api.sock")
	srv, err := a.serveSocket(socket)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	defer client.CloseIdleConnections()
	call := func(method, path, body string) []byte {
		t.Helper()
		r, _ := http.NewRequest(method, "http://startship/api/"+path, strings.NewReader(body))
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatal(resp.Status, string(b))
		}
		return b
	}
	base := "workspaces/" + ws.ID + "/firewall"
	call("PUT", base, `{"mode":"enforce","managed":{},"limits":{"concurrent_per_ip":2},"rules":[]}`)
	call("POST", base+"/lists", `{"id":"persisted","name":"Persisted","kind":"block","entries":[{"value":"203.0.113.0/24"}]}`)
	call("POST", base+"/ban", `{"ip":"192.0.2.1","ttl_seconds":120}`)
	call("GET", "firewall/catalog", "")
	if err = srv.Close(); err != nil {
		t.Fatal(err)
	}
	client.CloseIdleConnections()
	project := Record{ID: "persistent-project", Kind: "projects", WorkspaceID: ws.ID}
	if err = s.Save(project); err != nil {
		t.Fatal(err)
	}
	firewallRoute(t, a, ws.ID, project.ID)
	if err = s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	reopened := &Server{store: s}
	reopened.refresh()
	p, lists, err := reopened.firewallConfig(ws.ID)
	if err != nil || p.Mode != "enforce" || p.UpdatedBy != uid || p.Limits.ConcurrentPerIP != 2 || len(p.Managed) != 0 || len(lists) != 2 || len(findFirewallList(lists, "banned").Entries) != 1 {
		t.Fatal(p, lists, err)
	}
	if w := firewallProxy(reopened, "/normal"); w.Code != 403 {
		t.Fatal("startup did not load bans", w.Code)
	}
	// Logical list IDs, including the implicit banned list, are workspace scoped.
	if err = s.Save(Record{ID: "other", Kind: "workspaces"}); err != nil {
		t.Fatal(err)
	}
	if err = reopened.recordFirewallBan(router.Ban{WorkspaceID: "other", IP: "198.51.100.1"}); err != nil {
		t.Fatal(err)
	}
	_, other, err := reopened.firewallConfig("other")
	if err != nil || findFirewallList(other, "banned").Entries[0].Value != "198.51.100.1" {
		t.Fatal(other, err)
	}
	_, lists, err = reopened.firewallConfig(ws.ID)
	if err != nil || findFirewallList(lists, "banned").Entries[0].Value != "192.0.2.1" {
		t.Fatal("scope leak", lists, err)
	}
}

func TestFirewallLive(t *testing.T) {
	f := newRBACFixture(t)
	f.a.liveSessionInterval = 40 * time.Millisecond
	f.assign(t, f.alice, f.bobID, "contributor", scopeWorkspace, f.workspace.ID, 201)
	srv := httptest.NewServer(f.h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live", &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {f.bob.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.SetReadLimit(2 << 20)
	read := func() map[string]any {
		t.Helper()
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return decodeBody[map[string]any](t, b)
	}
	send := func(v any) {
		t.Helper()
		if err := c.Write(ctx, websocket.MessageText, []byte(firewallJSON(t, v))); err != nil {
			t.Fatal(err)
		}
	}
	if m := read(); m["type"] != "ready" {
		t.Fatal(m)
	}
	send(map[string]any{"type": "subscribe", "id": "bad", "topic": "firewall"})
	if m := read(); m["type"] != "error" || m["error"] != "invalid subscription" {
		t.Fatal(m)
	}
	now := time.Now()
	for i := 0; i < 110; i++ {
		f.a.recordFirewallEvent(firewall.Event{Time: now, WorkspaceID: f.workspace.ID, RuleID: fmt.Sprint(i)})
	}
	send(map[string]any{"type": "subscribe", "id": "fw", "topic": "firewall", "workspace_id": f.workspace.ID})
	m := read()
	data := m["data"].(map[string]any)
	events := data["events"].([]any)
	if m["type"] != "snapshot" || data["can_manage"] != false || len(events) != 100 || events[0].(map[string]any)["rule_id"] != "109" || len(data["stats"].(map[string]any)["minutes"].([]any)) != 60 {
		t.Fatal(m)
	}
	if own, _, _, why := f.a.liveData(ctx, f.aliceID, &liveSub{Topic: "firewall", WorkspaceID: f.workspace.ID}); why != "" || own.(map[string]any)["can_manage"] != true {
		t.Fatal(own, why)
	}
	base := "workspaces/" + f.workspace.ID + "/firewall"
	accountRequest(t, f.h, "PUT", base, `{"mode":"monitor","managed":{},"rules":[]}`, f.alice, 200)
	if m = read(); m["type"] != "snapshot" || m["data"].(map[string]any)["policy"].(map[string]any)["mode"] != "monitor" {
		t.Fatal(m)
	}
	accountRequest(t, f.h, "POST", base+"/lists", `{"id":"live","name":"Live","kind":"block"}`, f.alice, 200)
	if m = read(); m["type"] != "snapshot" || len(m["data"].(map[string]any)["lists"].([]any)) != 2 {
		t.Fatal(m)
	}
	last := time.Now()
	for i := 0; i < 80; i++ {
		f.a.observeFirewall(f.workspace.ID, nil, firewall.Decision{Action: firewall.Allow})
	}
	m = read()
	if elapsed := time.Since(last); elapsed < 850*time.Millisecond {
		t.Fatal("observation snapshots not throttled", elapsed)
	}
	if got := m["data"].(map[string]any)["stats"].(map[string]any)["total"]; got != float64(80) {
		t.Fatal(m)
	}
	last = time.Now()
	f.a.recordFirewallEvent(firewall.Event{Time: time.Now(), WorkspaceID: f.workspace.ID, RuleID: "new"})
	m = read()
	if elapsed := time.Since(last); elapsed < 850*time.Millisecond {
		t.Fatal("event snapshots not throttled", elapsed)
	}
	if m["data"].(map[string]any)["events"].([]any)[0].(map[string]any)["rule_id"] != "new" {
		t.Fatal(m)
	}
	if _, err := f.a.store.DB.Exec("UPDATE role_assignments SET role_id='member' WHERE user_id=? AND scope_id=?", f.bobID, f.workspace.ID); err != nil {
		t.Fatal(err)
	}
	if m = read(); m["type"] != "error" || m["error"] != "forbidden" {
		t.Fatal(m)
	}
	send(map[string]any{"type": "subscribe", "id": "denied", "topic": "firewall", "workspace_id": f.workspace.ID})
	if m = read(); m["type"] != "error" || m["error"] != "forbidden" {
		t.Fatal(m)
	}
}

func TestFirewallStorageFailures(t *testing.T) {
	f := newRBACFixture(t)
	base := "workspaces/" + f.workspace.ID + "/firewall"
	accountRequest(t, f.h, "GET", base, "", f.alice, 200)
	// Audit failure must roll back the policy/list mutation with it.
	if _, err := f.a.store.DB.Exec(`CREATE TRIGGER fail_firewall_audit BEFORE INSERT ON audit_log WHEN NEW.action LIKE 'firewall.%' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END;`); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct{ method, path, body string }{{"PUT", "", `{"mode":"enforce","managed":{},"rules":[]}`}, {"POST", "/lists", `{"id":"failed","name":"Failed","kind":"block"}`}} {
		accountRequest(t, f.h, request.method, base+request.path, request.body, f.alice, 500)
	}
	var count int
	if err := f.a.store.DB.QueryRow("SELECT count(*) FROM firewall_policies WHERE workspace_id=?", f.workspace.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("policy committed without audit", count, err)
	}
	if err := f.a.store.DB.QueryRow("SELECT count(*) FROM firewall_lists WHERE workspace_id=? AND id='failed'", f.workspace.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("list committed without audit", count, err)
	}
	if _, err := f.a.store.DB.Exec("DROP TRIGGER fail_firewall_audit"); err != nil {
		t.Fatal(err)
	}
	accountRequest(t, f.h, "GET", base, "", f.alice, 200)
	if _, err := f.a.store.DB.Exec("INSERT INTO firewall_policies VALUES(?,?,?,?)", f.workspace.ID, "not JSON", "", ""); err != nil {
		t.Fatal(err)
	}
	accountRequest(t, f.h, "GET", base, "", f.alice, 500)
	if err := f.a.recordFirewallBan(router.Ban{WorkspaceID: f.workspace.ID, IP: "192.0.2.1"}); err == nil {
		t.Fatal("ban storage failure ignored")
	}
	if _, _, _, why := f.a.liveData(context.Background(), f.aliceID, &liveSub{Topic: "firewall", WorkspaceID: f.workspace.ID}); why != "firewall storage unavailable" {
		t.Fatal(why)
	}
}
