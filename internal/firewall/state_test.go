package firewall

import (
	"testing"
	"time"
)

func TestClearPersistedBansKeepsCounters(t *testing.T) {
	s := NewState()
	s.ban("192.0.2.1", "rule", "Rule", 0)
	key := rateKey{rule: "rule", ip: "192.0.2.1"}
	if s.over(key, 1, 60) || !s.Acquire("192.0.2.1", 1) {
		t.Fatal("initial admission")
	}
	s.ClearBans()
	if _, ok := s.banned("192.0.2.1"); ok {
		t.Fatal("ban retained")
	}
	if !s.over(key, 1, 60) || s.Acquire("192.0.2.1", 1) {
		t.Fatal("counters reset")
	}
	s.Release("192.0.2.1")
	s.ClearBans()
	if !s.Acquire("192.0.2.1", 1) {
		t.Fatal("release")
	}
	s.Release("192.0.2.1")
}

func TestSlidingWindowAndIPScope(t *testing.T) {
	now := time.Unix(1000, 0)
	s := NewState()
	s.now = func() time.Time { return now }
	r := rule(Condition{"path", "prefix", "/", false})
	r.Action = RateLimit
	r.Rate = &Rate{2, 10, "ip"}
	e := compile(t, policy(r), nil, s)
	req := &Request{IP: "192.0.2.1", Path: "/one"}
	e.Evaluate(req)
	now = now.Add(5 * time.Second)
	req.Path = "/two"
	e.Evaluate(req)
	if e.Evaluate(req).StatusCode != 429 {
		t.Fatal("IP paths did not share counter")
	}
	req.IP = "192.0.2.2"
	if e.Evaluate(req).Action != Allow {
		t.Fatal("IPs shared counter")
	}
	req.IP = "192.0.2.1"
	now = now.Add(5 * time.Second)
	if e.Evaluate(req).StatusCode != 429 {
		t.Fatal("window was fixed rather than sliding")
	}
	now = now.Add(10 * time.Second)
	if e.Evaluate(req).Action != Allow {
		t.Fatal("window did not expire")
	}
	r.ID = "other"
	if compile(t, policy(r), nil, s).Evaluate(req).Action != Allow {
		t.Fatal("rule IDs shared counters")
	}
	r.Action = Ban
	r.Rate = nil
	r.BanSeconds = 0
	e = compile(t, policy(r), nil, s)
	if e.Evaluate(req).Action != Ban {
		t.Fatal("ban not applied")
	}
	now = now.Add(365 * 24 * time.Hour)
	e = compile(t, policy(), nil, s)
	if e.Evaluate(req).RuleID != "banned" || !e.inList("banned", req.IP) {
		t.Fatal("permanent ban lost")
	}
	p := policy()
	p.Mode = "monitor"
	p.Limits.ConcurrentPerIP = 1
	e = compile(t, p, nil, nil)
	if !e.Acquire("ip") || !e.Acquire("ip") {
		t.Fatal("monitor enforced concurrency")
	}
	e.Release("ip")
	e.Release("ip")
}

func TestManagedLogAndAllow(t *testing.T) {
	p := policy()
	p.Managed["scanner_paths"] = ManagedRule{true, Log}
	d := compile(t, p, nil, nil).Evaluate(&Request{Path: "/.env"})
	if d.Action != Allow || len(d.Events) != 1 || d.Events[0].Action != Log {
		t.Fatal(d)
	}
	p.Managed["scanner_paths"] = ManagedRule{true, Allow}
	p.Limits.MaxURLBytes = 1
	d = compile(t, p, nil, nil).Evaluate(&Request{Path: "/.env"})
	if d.Action != Allow || d.RuleID != "scanner_paths" {
		t.Fatal(d)
	}
}

func TestLazyExpirationAndEmptyHeader(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	e := compile(t, policy(), []List{{ID: "expired", Kind: "block", Entries: []Entry{{Value: "192.0.2.1", ExpiresAt: &past}}}}, nil)
	if e.Evaluate(&Request{IP: "192.0.2.1"}).Action != Allow || len(e.lists[0].entries) != 0 {
		t.Fatal("expired entries not purged")
	}
	c := Condition{"header.x-empty", "exists", "", false}
	e = compile(t, policy(rule(c)), nil, nil)
	req := &Request{Headers: make(map[string][]string)}
	req.Headers.Set("X-Empty", "")
	if e.Evaluate(req).Action != Block {
		t.Fatal("empty header still exists")
	}
}
