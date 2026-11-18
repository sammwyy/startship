package firewall

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func policy(rules ...Rule) Policy {
	p := DefaultPolicy()
	p.Mode = "enforce"
	p.Managed = map[string]ManagedRule{}
	p.Rules = rules
	return p
}
func rule(c ...Condition) Rule {
	return Rule{ID: "test", Name: "Test", Enabled: true, Match: "all", Conditions: c, Action: Block}
}
func compile(t *testing.T, p Policy, lists []List, s *State) *Engine {
	t.Helper()
	e, err := Compile(p, lists, s)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestValidation(t *testing.T) {
	base := func() Policy { return policy(rule(Condition{Field: "path", Op: "eq", Value: "/"})) }
	tests := []struct {
		name, path string
		change     func(*Policy)
	}{
		{"mode", "mode", func(p *Policy) { p.Mode = "bad" }},
		{"managed", "managed.foo", func(p *Policy) { p.Managed["foo"] = ManagedRule{true, Block} }},
		{"managed action", "managed.sqli.action", func(p *Policy) { p.Managed["sqli"] = ManagedRule{true, RateLimit} }},
		{"negative limit", "limits.max_body_bytes", func(p *Policy) { p.Limits.MaxBodyBytes = -1 }},
		{"negative url", "limits.max_url_bytes", func(p *Policy) { p.Limits.MaxURLBytes = -1 }},
		{"negative headers", "limits.max_header_bytes", func(p *Policy) { p.Limits.MaxHeaderBytes = -1 }},
		{"negative concurrent", "limits.concurrent_per_ip", func(p *Policy) { p.Limits.ConcurrentPerIP = -1 }},
		{"workspace requests", "limits.rate.requests", func(p *Policy) { p.Limits.Rate = &WorkspaceRate{Action: Block} }},
		{"workspace window", "limits.rate.window_seconds", func(p *Policy) { p.Limits.Rate = &WorkspaceRate{Requests: 1, Action: Block} }},
		{"workspace action", "limits.rate.action", func(p *Policy) { p.Limits.Rate = &WorkspaceRate{Requests: 1, WindowSeconds: 1, Action: Allow} }},
		{"workspace ban", "limits.rate.ban_seconds", func(p *Policy) {
			p.Limits.Rate = &WorkspaceRate{Requests: 1, WindowSeconds: 1, Action: Ban, BanSeconds: -1}
		}},
		{"id", "rules[0].id", func(p *Policy) { p.Rules[0].ID = "" }},
		{"duplicate", "rules[1].id", func(p *Policy) { p.Rules = append(p.Rules, p.Rules[0]) }},
		{"match", "rules[0].match", func(p *Policy) { p.Rules[0].Match = "bad" }},
		{"action", "rules[0].action", func(p *Policy) { p.Rules[0].Action = "bad" }},
		{"ban", "rules[0].ban_seconds", func(p *Policy) { p.Rules[0].BanSeconds = -1 }},
		{"rate required", "rules[0].rate", func(p *Policy) { p.Rules[0].Action = RateLimit }},
		{"rate unexpected", "rules[0].rate", func(p *Policy) { p.Rules[0].Rate = &Rate{1, 1, "ip"} }},
		{"rate requests", "rules[0].rate.requests", func(p *Policy) { p.Rules[0].Action = RateLimit; p.Rules[0].Rate = &Rate{0, 1, "ip"} }},
		{"rate window", "rules[0].rate.window_seconds", func(p *Policy) { p.Rules[0].Action = RateLimit; p.Rules[0].Rate = &Rate{1, 0, "ip"} }},
		{"rate by", "rules[0].rate.by", func(p *Policy) { p.Rules[0].Action = RateLimit; p.Rules[0].Rate = &Rate{1, 1, "bad"} }},
		{"empty", "rules[0].conditions", func(p *Policy) { p.Rules[0].Conditions = nil }},
		{"too many", "rules[0].conditions", func(p *Policy) {
			for len(p.Rules[0].Conditions) < 17 {
				p.Rules[0].Conditions = append(p.Rules[0].Conditions, p.Rules[0].Conditions[0])
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := base()
			tt.change(&p)
			if err := Validate(p); err == nil || !strings.Contains(err.Error(), tt.path) {
				t.Fatalf("got %v, want %s", err, tt.path)
			}
		})
	}
	for _, tt := range []struct {
		c    Condition
		want string
	}{
		{Condition{"foo", "eq", "x", false}, `unknown field "foo"`},
		{Condition{"header.", "eq", "x", false}, "unknown field"},
		{Condition{"header.<name>", "eq", "x", false}, "unknown field"},
		{Condition{"path", "wat", "x", false}, ".op:"},
		{Condition{"ip", "eq", "x", false}, ".op:"},
		{Condition{"path", "eq", 5, false}, ".value:"},
		{Condition{"path", "eq", []string{"x"}, false}, "must be a string for eq"},
		{Condition{"path", "in", []string{}, false}, "must not be empty"},
		{Condition{"path", "in", []any{1}, false}, ".value:"},
		{Condition{"path", "matches", "[", false}, "invalid regex"},
		{Condition{"path", "matches", strings.Repeat("a", 513), false}, "512"},
		{Condition{"ip", "in_cidr", "bogus", false}, "invalid IP"},
		{Condition{"ip", "in_list", "", false}, "list id"},
		{Condition{"country", "eq", "1!", false}, "ISO-2"},
		{Condition{"ua_class", "eq", "bad", false}, "invalid ua_class"},
	} {
		p := policy(rule(tt.c))
		err := Validate(p)
		if err == nil || !strings.Contains(err.Error(), "rules[0].conditions[0]") || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%+v: %v", tt.c, err)
		}
	}
	if err := Validate(DefaultPolicy()); err != nil {
		t.Fatal(err)
	}
}
func TestFieldsAndOperators(t *testing.T) {
	req := &Request{IP: "192.0.2.4", Country: "us", Host: "Example.COM", Method: "GET", Path: "/Hello", RawQuery: "q=hello", Scheme: "https", UA: "Mozilla/5.0 Chrome/120.0 Safari/537.36", Headers: http.Header{"X-Test": []string{"one", "two"}}}
	tests := []Condition{
		{"ip", "in_cidr", "192.0.2.0/24", false}, {"ip", "in_cidr", "192.0.2.4", false}, {"ip", "in_list", "friends", false},
		{"country", "eq", "US", false}, {"country", "in", []string{"GB", "US"}, false},
		{"host", "eq", "EXAMPLE.com", false}, {"host", "matches", "^EXAMPLE\\.COM$", false},
		{"method", "in", []string{"get", "POST"}, false}, {"path", "contains", "Hell", false},
		{"path", "prefix", "/He", false}, {"path", "suffix", "llo", false}, {"path", "matches", "^/H.*o$", false},
		{"query", "eq", "q=hello", false}, {"header.x-test", "contains", "two", false}, {"header.x-test", "exists", "", false},
		{"user_agent", "contains", "Chrome", false}, {"ua_class", "eq", "BROWSER", false}, {"device", "eq", "DESKTOP", false},
		{"scheme", "eq", "https", false}, {"path", "eq", "/hello", true},
	}
	lists := []List{{ID: "friends", Kind: "allow", Entries: []Entry{{Value: req.IP}}}}
	// Keep the list available to in_list without allow-list short-circuiting the tested rule.
	for _, c := range tests {
		t.Run(c.Field+"/"+c.Op, func(t *testing.T) {
			e := compile(t, policy(rule(c)), nil, nil)
			if c.Op == "in_list" {
				e = compile(t, policy(rule(c)), lists, nil)
				if !e.condition(e.rules[0].conditions[0], req) {
					t.Fatal("list condition false")
				}
				return
			}
			if d := e.Evaluate(req); d.RuleID != "test" {
				t.Fatalf("no match: %+v", d)
			}
			cc := e.rules[0].conditions[0]
			cc.Negate = !cc.Negate
			if e.condition(cc, req) {
				t.Fatal("negation failed")
			}
		})
	}
	for _, c := range []Condition{{"path", "eq", "/hello", false}, {"header.missing", "exists", "", false}, {"ip", "in_cidr", "2001:db8::/32", false}, {"country", "eq", "GB", false}} {
		e := compile(t, policy(rule(c)), nil, nil)
		if e.Evaluate(req).Action != Allow {
			t.Errorf("unexpected match %+v", c)
		}
	}
	req.Country = ""
	e := compile(t, policy(rule(Condition{"country", "eq", "US", true})), nil, nil)
	if e.Evaluate(req).Action != Allow {
		t.Fatal("missing country matched")
	}
}
func TestOrderingListsAndMonitor(t *testing.T) {
	req := &Request{IP: "192.0.2.1", Path: "/.env"}
	r := rule(Condition{"path", "prefix", "/", false})
	r.Action = Allow
	p := DefaultPolicy()
	p.Mode = "enforce"
	p.Rules = []Rule{r}
	p.Limits.MaxURLBytes = 1
	if d := compile(t, p, nil, nil).Evaluate(req); d.Action != Allow || d.RuleID != "test" {
		t.Fatal(d)
	}
	block := r
	block.ID = "second"
	block.Action = Block
	p.Rules = append(p.Rules, block)
	if d := compile(t, p, nil, nil).Evaluate(req); d.Action != Allow {
		t.Fatal(d)
	}
	p.Rules[0].Action = Log
	p.Limits = Limits{}
	if d := compile(t, p, nil, nil).Evaluate(req); d.RuleID != "second" || len(d.Events) != 2 {
		t.Fatal(d)
	}
	p.Mode = "monitor"
	if d := compile(t, p, nil, nil).Evaluate(req); !d.Monitored || d.StatusCode != 0 || !d.Events[1].Monitored {
		t.Fatal(d)
	}
	p.Mode = "off"
	if d := compile(t, p, nil, nil).Evaluate(req); d.Action != Allow || len(d.Events) != 0 {
		t.Fatal(d)
	}
	p = policy(block)
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	allow := List{ID: "allow", Kind: "allow", Entries: []Entry{{Value: "192.0.2.0/24", ExpiresAt: &future}}}
	deny := List{ID: "deny", Kind: "block", Entries: []Entry{{Value: req.IP, ExpiresAt: &past}}}
	if d := compile(t, p, []List{allow, deny}, nil).Evaluate(req); d.Action != Allow || d.RuleID != "allow" {
		t.Fatal(d)
	}
	deny.Entries[0].ExpiresAt = nil
	if d := compile(t, p, []List{allow, deny}, nil).Evaluate(req); d.Action != Block || d.RuleID != "deny" {
		t.Fatal(d)
	}
	allow.Entries[0].ExpiresAt = &past
	if d := compile(t, p, []List{allow}, nil).Evaluate(req); d.RuleID != "second" {
		t.Fatal(d)
	}
	for _, ls := range [][]List{{{Kind: "bad", ID: "a"}}, {{Kind: "block", ID: "a", Entries: []Entry{{Value: "no"}}}}, {{Kind: "block"}}, {{Kind: "block", ID: "a"}, {Kind: "allow", ID: "a"}}} {
		if _, err := Compile(p, ls, nil); err == nil {
			t.Fatal("accepted invalid list")
		}
	}
	r.Action = Block
	r.Match = "any"
	r.Conditions = append(r.Conditions, Condition{"method", "eq", "POST", false})
	req.Path = "other"
	if compile(t, policy(r), nil, nil).Evaluate(req).Action != Allow {
		t.Fatal("any matched")
	}
	req.Method = "POST"
	if compile(t, policy(r), nil, nil).Evaluate(req).Action != Block {
		t.Fatal("any failed")
	}
	r.Enabled = false
	if compile(t, policy(r), nil, nil).Evaluate(req).Action != Allow {
		t.Fatal("disabled matched")
	}
}
func TestRateBanAndConcurrency(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewState()
	s.now = func() time.Time { return now }
	r := rule(Condition{"path", "prefix", "/", false})
	r.Action = RateLimit
	r.Rate = &Rate{2, 10, "ip_path"}
	p := policy(r)
	e := compile(t, p, nil, s)
	req := &Request{IP: "192.0.2.1", Path: "/a"}
	for i := 0; i < 2; i++ {
		if d := e.Evaluate(req); d.Action != Allow {
			t.Fatal(d)
		}
	}
	e = compile(t, p, nil, s)
	if d := e.Evaluate(req); d.Action != RateLimit || d.StatusCode != 429 {
		t.Fatal(d)
	}
	req.Path = "/b"
	if d := e.Evaluate(req); d.Action != Allow {
		t.Fatal(d)
	}
	req.Path = "/a"
	now = now.Add(10 * time.Second)
	if d := e.Evaluate(req); d.Action != Allow {
		t.Fatal(d)
	}
	p.Rules[0].BanSeconds = 5
	e = compile(t, p, nil, s)
	e.Evaluate(req)
	if d := e.Evaluate(req); d.Action != Ban {
		t.Fatal(d)
	}
	e = compile(t, policy(), nil, s)
	if d := e.Evaluate(req); d.RuleID != "banned" {
		t.Fatal(d)
	}
	now = now.Add(5 * time.Second)
	if d := e.Evaluate(req); d.Action != Allow {
		t.Fatal(d)
	}
	p = policy()
	p.Limits.Rate = &WorkspaceRate{1, 2, Ban, 3}
	s = NewState()
	s.now = func() time.Time { return now }
	e = compile(t, p, nil, s)
	e.Evaluate(req)
	if d := e.Evaluate(req); d.Action != Ban {
		t.Fatal(d)
	}
	now = now.Add(3 * time.Second)
	if d := e.Evaluate(req); d.Action != Allow {
		t.Fatal(d)
	}
	p.Mode = "monitor"
	s = NewState()
	e = compile(t, p, nil, s)
	e.Evaluate(req)
	if d := e.Evaluate(req); !d.Monitored || d.StatusCode != 0 {
		t.Fatal(d)
	}
	if _, ok := s.banned(req.IP); ok {
		t.Fatal("monitor wrote ban")
	}
	p = policy()
	p.Limits.ConcurrentPerIP = 1
	e = compile(t, p, nil, s)
	if !e.Acquire(req.IP) || e.Acquire(req.IP) {
		t.Fatal("concurrency")
	}
	e = compile(t, p, nil, s)
	if e.Acquire(req.IP) {
		t.Fatal("lost concurrency on recompile")
	}
	e.Release(req.IP)
	if !e.Acquire(req.IP) {
		t.Fatal("release")
	}
	e.Release(req.IP)
	e.Release(req.IP)
	// State maps and matching are safe under simultaneous requests and recompiles.
	p.Limits.Rate = &WorkspaceRate{10000, 10, Block, 0}
	e = compile(t, p, nil, s)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				e.Evaluate(req)
				if e.Acquire(req.IP) {
					e.Release(req.IP)
				}
			}
		}()
	}
	wg.Wait()
}
func TestLimitsAndImmutability(t *testing.T) {
	for _, tt := range []struct {
		limits Limits
		req    Request
		status int
	}{{Limits{MaxBodyBytes: 2}, Request{ContentLength: 3}, 413}, {Limits{MaxBodyBytes: 2}, Request{BodySample: []byte("abc")}, 413}, {Limits{MaxURLBytes: 2}, Request{Path: "/", RawQuery: "x"}, 414}, {Limits{MaxHeaderBytes: 2}, Request{Headers: http.Header{"X": []string{"y"}}}, 431}} {
		e := compile(t, policy(), nil, nil)
		e.policy.Limits = tt.limits
		if d := e.Evaluate(&tt.req); d.StatusCode != tt.status {
			t.Fatal(d)
		}
	}
	p := policy(rule(Condition{"path", "in", []string{"/a"}, false}))
	e := compile(t, p, nil, nil)
	p.Rules[0].Conditions[0].Value.([]string)[0] = "/b"
	p.Rules[0].Enabled = false
	if e.Evaluate(&Request{Path: "/a"}).Action != Block {
		t.Fatal("policy mutated")
	}
	if e.Evaluate(nil).Action != Allow {
		t.Fatal("nil request")
	}
	data, err := json.Marshal(DefaultPolicy())
	if err != nil || !strings.Contains(string(data), `"max_body_bytes"`) {
		t.Fatal(string(data), err)
	}
	c := Catalog()
	if c.GeoIP || len(c.ManagedRules) != 7 || len(c.Actions) != 5 {
		t.Fatal(c)
	}
}
func TestManagedRules(t *testing.T) {
	cases := map[string][]string{"sqli": {"' OR 1=1--", "UNION SELECT password FROM users"}, "xss": {"<script>alert(1)</script>", "javascript:alert(1)", `<img src=x onerror=alert(1)>`}, "path_traversal": {"../../etc/passwd", "%2e%2e%2fetc/passwd", "abc%00"}, "scanner_paths": {"/.env", "/wp-login.php", "/.git/config"}}
	for id, vs := range cases {
		for _, v := range vs {
			t.Run(id+"/"+v, func(t *testing.T) {
				p := policy()
				p.Managed[id] = ManagedRule{true, Block}
				e := compile(t, p, nil, nil)
				r := &Request{Path: v}
				if id == "sqli" || id == "xss" {
					r.Path = "/"
					r.RawQuery = "q=" + v
				}
				if d := e.Evaluate(r); d.RuleID != id {
					t.Fatalf("not detected %+v", d)
				}
			})
		}
	}
	for _, ua := range []string{"Googlebot/2.1", "GPTBot/1.0", "curl/8.0", "sqlmap/1.7", "", "weird-agent"} {
		id := "bad_bots"
		class, _ := ClassifyUA(ua)
		if class == "crawler" {
			id = "crawlers"
		}
		if class == "unknown" || class == "empty" {
			id = "unknown_user_agents"
		}
		p := policy()
		p.Managed[id] = ManagedRule{true, Block}
		if d := compile(t, p, nil, nil).Evaluate(&Request{UA: ua}); d.RuleID != id {
			t.Fatalf("%q: %+v", ua, d)
		}
	}
	p := DefaultPolicy()
	p.Mode = "enforce"
	e := compile(t, p, nil, nil)
	for _, q := range []string{"q=select a plan", "name=O'Brien", "q=how to select a plan", "q=normal&count=10"} {
		if d := e.Evaluate(&Request{Path: "/api/search", RawQuery: q}); d.Action != Allow {
			t.Fatalf("benign %s: %+v", q, d)
		}
	}
	for _, body := range []string{`{"name":"O'Brien","query":"select a plan"}`, `{"enabled":true,"items":[1,2,3]}`} {
		if d := e.Evaluate(&Request{Path: "/api", Headers: http.Header{"Content-Type": []string{"application/json"}}, BodySample: []byte(body)}); d.Action != Allow {
			t.Fatal(d)
		}
	}
	for _, ct := range []string{"application/json", "application/problem+json", "application/x-www-form-urlencoded"} {
		body := []byte(`{"nested":["<script>alert(1)</script>"]}`)
		if ct == "application/x-www-form-urlencoded" {
			body = []byte("q=%3Cscript%3E")
		}
		req := &Request{Path: "/api", Headers: http.Header{"Content-Type": []string{ct}}, BodySample: body}
		if d := e.Evaluate(req); d.RuleID != "xss" {
			t.Fatal(d)
		}
		req.Headers.Set("Content-Type", "text/plain")
		if e.Evaluate(req).Action != Allow {
			t.Fatal("scanned plain body")
		}
	}
	req := &Request{Headers: http.Header{"Content-Type": []string{"application/json"}}, BodySample: []byte(`{"x":"<script>"}`), ContentLength: 65537}
	if e.Evaluate(req).Action != Allow {
		t.Fatal("oversized body scanned")
	}
}
func TestUAClassification(t *testing.T) {
	for _, tt := range []struct{ ua, class, device string }{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36", "browser", "desktop"},
		{"Mozilla/5.0 (Android 14; Mobile) Gecko/120 Firefox/120.0", "browser", "mobile"},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/120.0.0.0 Mobile Safari/537.36", "browser", "mobile"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15 Safari/604.1", "browser", "mobile"},
		{"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15 Safari/604.1", "browser", "tablet"},
		{"Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 Chrome/120 Safari/537.36", "browser", "tablet"},
		{"Googlebot/2.1", "crawler", "bot"}, {"GPTBot/1.0", "crawler", "bot"}, {"sqlmap/1", "bot", "bot"}, {"curl/8", "cli", "unknown"}, {"python-requests/2", "library", "unknown"}, {"something", "unknown", "unknown"}, {"", "empty", "unknown"},
	} {
		t.Run(tt.class+"/"+tt.device+"/"+tt.ua, func(t *testing.T) {
			c, d := ClassifyUA(tt.ua)
			if c != tt.class || d != tt.device {
				t.Fatalf("got %s %s", c, d)
			}
		})
	}
}

type fakeGeo struct{}

func (fakeGeo) Country(ip net.IP) string {
	if ip.String() == "192.0.2.1" {
		return "US"
	}
	return "GB"
}
func TestClientResolution(t *testing.T) {
	proxies, err := ParseTrustedProxies([]string{"10.0.0.0/8", "2001:db8::/32"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ remote, xff, cf, country, ip, wantCountry string }{
		{"192.0.2.1:123", "198.51.100.1", "198.51.100.2", "DE", "192.0.2.1", "US"},
		{"10.0.0.1:123", "198.51.100.9, 192.0.2.1, 10.0.0.2", "", "", "192.0.2.1", "US"},
		{"10.0.0.1:123", "", "192.0.2.1", "DE", "192.0.2.1", "DE"},
		{"10.0.0.1:123", "invalid, 192.0.2.1", "192.0.2.1", "", "10.0.0.1", "GB"},
		{"[2001:db8::1]:123", "2001:4860::1, 2001:db8::2", "", "", "2001:4860::1", "GB"},
		{"bad", "", "", "", "", ""},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			h := http.Header{"X-Forwarded-For": []string{tt.xff}, "Cf-Connecting-Ip": []string{tt.cf}, "Cf-Ipcountry": []string{tt.country}}
			ip, c := ResolveClient(tt.remote, h, proxies, fakeGeo{})
			if ip != tt.ip || c != tt.wantCountry {
				t.Fatalf("%s %s", ip, c)
			}
		})
	}
	if _, err := ParseTrustedProxies([]string{"bad"}); err == nil {
		t.Fatal("accepted invalid proxy")
	}
	var nilDB *MMDB
	if nilDB.Country(net.ParseIP("192.0.2.1")) != "" || nilDB.Close() != nil {
		t.Fatal("nil database")
	}
	db, err := OpenMMDB("")
	if err != nil || db.Country(nil) != "" {
		t.Fatal(err)
	}
	if _, err := OpenMMDB("does-not-exist.mmdb"); err == nil {
		t.Fatal("opened missing database")
	}
	_, c := ResolveClient("192.0.2.1:1", nil, nil, nil)
	if c != "" {
		t.Fatal(c)
	}
}
