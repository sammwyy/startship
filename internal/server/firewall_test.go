package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sammwyy/startship/internal/firewall"
	"github.com/sammwyy/startship/internal/router"
)

func TestFirewallRingAndStats(t *testing.T) {
	a := &Server{}
	now := time.Now().UTC()
	for i := 0; i < 510; i++ {
		e := firewall.Event{Time: now, WorkspaceID: "ws", RuleID: fmt.Sprint(i % 7), IP: fmt.Sprint(i % 8), Country: "US", Action: firewall.Block, Status: 403}
		a.recordFirewallEvent(e)
		a.observeFirewall("ws", &firewall.Request{}, firewall.Decision{Action: firewall.Block, StatusCode: 403})
	}
	a.observeFirewall("ws", &firewall.Request{}, firewall.Decision{Action: firewall.Allow})
	a.observeFirewall("ws", &firewall.Request{}, firewall.Decision{Action: firewall.Block, Monitored: true})
	events, stats := a.firewallData("ws", now)
	if len(events) != 500 || stats.Total != 512 || stats.Blocked != 510 || len(stats.TopRules) != 5 || len(stats.TopIPs) != 5 || len(stats.TopCountries) != 1 || len(stats.Minutes) != 60 || stats.ByAction["allow"] != 1 {
		t.Fatalf("events %d stats %+v", len(events), stats)
	}
	if events[0].RuleID != fmt.Sprint(509%7) || events[499].RuleID != fmt.Sprint(10%7) {
		t.Fatal("ring ordering")
	}
	stats.ByAction["allow"] = 999
	stats.Minutes[59].ByAction["allow"] = 999
	events[0].RuleID = "mutated"
	events, stats = a.firewallData("ws", now)
	if stats.ByAction["allow"] != 1 || events[0].RuleID == "mutated" {
		t.Fatal("snapshot alias")
	}
	if _, stats = a.firewallData("ws", now.Add(61*time.Minute)); stats.Total != 0 || len(stats.TopIPs) != 0 {
		t.Fatal("old stats retained")
	}
	if events, stats = a.firewallData("other", now); len(events) != 0 || stats.Total != 0 {
		t.Fatal("workspace leak")
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				a.recordFirewallEvent(firewall.Event{Time: now, WorkspaceID: "concurrent"})
				a.observeFirewall("concurrent", nil, firewall.Decision{Action: firewall.Allow})
				a.firewallData("concurrent", now)
			}
		}()
	}
	wg.Wait()
}
func TestFirewallRefreshAndBanHook(t *testing.T) {
	a := testServer(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(u.Port())
	for _, r := range []Record{{ID: "ws", Kind: "workspaces"}, {ID: "project", Kind: "projects", WorkspaceID: "ws"}, {ID: "app", Kind: "apps", ProjectID: "project", Active: "deployment", Port: port}, {ID: "deployment", Kind: "deployments", Status: "running", IP: u.Hostname()}, {ID: "route", Kind: "routes", AppID: "app", ProjectID: "project", Host: "app.test", PathPrefix: "/"}} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	a.refresh()
	state := a.firewall.states["ws"]
	if state == nil {
		t.Fatal("missing workspace state")
	}
	call := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		a.ServeHTTP(w, httptest.NewRequest("GET", "http://app.test"+path, nil))
		return w
	}
	if w := call("/?q=%27+OR+1=1--"); w.Code != 200 {
		t.Fatal("default not off", w.Code)
	}
	p := firewall.DefaultPolicy()
	p.Mode = "enforce"
	p.Limits.Rate = &firewall.WorkspaceRate{Requests: 1, WindowSeconds: 60, Action: firewall.Ban, BanSeconds: 30}
	if err := a.saveFirewallPolicy("ws", "", "firewall.policy.update", "ws", p); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	if a.firewall.states["ws"] != state {
		t.Fatal("state replaced")
	}
	if w := call("/?q=%27+OR+1=1--"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := call("/"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	a.refresh()
	if w := call("/"); w.Code != 429 {
		t.Fatal("rate state reset", w.Code)
	}
	_, lists, err := a.firewallConfig("ws")
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 1 || lists[0].ID != "banned" || len(lists[0].Entries) != 1 || lists[0].Entries[0].ExpiresAt == nil {
		t.Fatal(lists)
	}
	a.refresh()
	if w := call("/"); w.Code != 403 {
		t.Fatal("ban not preserved", w.Code)
	}
	events, stats := a.firewallData("ws", time.Now())
	if len(events) != 3 || stats.Total != 5 || stats.Blocked != 3 {
		t.Fatal(len(events), stats.Total, stats.Blocked)
	}
	if err := a.recordFirewallBan(router.Ban{WorkspaceID: "ws", IP: "203.0.113.1", Seconds: 0}); err != nil {
		t.Fatal(err)
	}
	_, lists, err = a.firewallConfig("ws")
	if err != nil {
		t.Fatal(err)
	}
	entry := lists[0].Entries[1]
	if entry.ExpiresAt != nil {
		t.Fatal("permanent TTL")
	}
}
func TestFirewallDashboardBypass(t *testing.T) {
	a := testServer(t)
	a.dashboardDomain = "dashboard.test"
	a.https = true
	req := httptest.NewRequest("GET", "http://dashboard.test/?q=%27+OR+1=1--", nil)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, req)
	if w.Code != 308 || w.Header().Get("Location") != "https://dashboard.test/?q=%27+OR+1=1--" {
		t.Fatal(w.Code, w.Header())
	}
	req = httptest.NewRequest("GET", "https://dashboard.test/api/setup?q=%27+OR+1=1--", nil)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, req)
	if w.Code == 403 || w.Code == 502 {
		t.Fatal("dashboard routed through data plane", w.Code)
	}
}
