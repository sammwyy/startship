package firewall

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"time"
)

type compiledCondition struct {
	Condition
	values []string
	regex  *regexp.Regexp
	prefix netip.Prefix
}
type compiledRule struct {
	Rule
	conditions []compiledCondition
}
type compiledEntry struct {
	prefix  netip.Prefix
	expires *time.Time
}
type compiledList struct {
	List
	mu      sync.Mutex
	entries []compiledEntry
}
type Engine struct {
	policy Policy
	rules  []compiledRule
	lists  []*compiledList
	state  *State
}

func ipPrefix(s string) (netip.Prefix, error) {
	if p, e := netip.ParsePrefix(s); e == nil {
		return p.Masked(), nil
	}
	a, e := netip.ParseAddr(s)
	if e != nil {
		return netip.Prefix{}, e
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}
func Compile(p Policy, lists []List, state *State) (*Engine, error) {
	if err := Validate(p); err != nil {
		return nil, err
	}
	if state == nil {
		state = NewState()
	}
	// Own the policy, including interface-valued condition arrays.
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var owned Policy
	if err = json.Unmarshal(data, &owned); err != nil {
		return nil, err
	}
	p = owned
	e := &Engine{policy: p, state: state}
	for _, r := range p.Rules {
		cr := compiledRule{Rule: r}
		for _, c := range r.Conditions {
			vs, _ := values(c.Value)
			cc := compiledCondition{Condition: c, values: vs}
			if c.Op == "matches" {
				pattern := vs[0]
				if member(c.Field, []string{"host", "method", "ua_class", "device", "country"}) {
					pattern = "(?i:" + pattern + ")"
				}
				cc.regex = regexp.MustCompile(pattern)
			}
			if c.Op == "in_cidr" {
				cc.prefix, _ = ipPrefix(vs[0])
			}
			cr.conditions = append(cr.conditions, cc)
		}
		e.rules = append(e.rules, cr)
	}
	ids := map[string]bool{}
	for i, l := range lists {
		if l.ID == "" || ids[l.ID] {
			return nil, fmt.Errorf("lists[%d].id: must be nonempty and unique", i)
		}
		ids[l.ID] = true
		if !member(l.Kind, []string{"block", "allow"}) {
			return nil, fmt.Errorf("lists[%d].kind: must be block or allow", i)
		}
		cl := &compiledList{List: List{ID: l.ID, Name: l.Name, Kind: l.Kind}}
		for j, v := range l.Entries {
			p, err := ipPrefix(v.Value)
			if err != nil {
				return nil, fmt.Errorf("lists[%d].entries[%d].value: invalid IP or CIDR", i, j)
			}
			var exp *time.Time
			if v.ExpiresAt != nil {
				t := *v.ExpiresAt
				exp = &t
			}
			cl.entries = append(cl.entries, compiledEntry{p, exp})
		}
		e.lists = append(e.lists, cl)
	}
	return e, nil
}
func (e *Engine) inList(id, ip string) bool {
	if id == "banned" {
		if _, ok := e.state.banned(ip); ok {
			return true
		}
	}
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	now := e.state.clock()
	for _, l := range e.lists {
		if l.ID != id {
			continue
		}
		l.mu.Lock()
		live := l.entries[:0]
		matched := false
		for _, v := range l.entries {
			if v.expires != nil && !now.Before(*v.expires) {
				continue
			}
			live = append(live, v)
			matched = matched || v.prefix.Contains(a.Unmap())
		}
		l.entries = live
		l.mu.Unlock()
		if matched {
			return true
		}
	}
	return false
}
func (e *Engine) condition(c compiledCondition, r *Request) bool {
	class, device := ClassifyUA(r.UA)
	v := ""
	switch c.Field {
	case "ip":
		v = r.IP
	case "country":
		v = r.Country
	case "host":
		v = r.Host
	case "path":
		v = r.Path
	case "method":
		v = r.Method
	case "query":
		v = r.RawQuery
	case "user_agent":
		v = r.UA
	case "ua_class":
		v = class
	case "device":
		v = device
	case "scheme":
		v = r.Scheme
	default:
		v = strings.Join(r.Headers.Values(strings.TrimPrefix(c.Field, "header.")), ", ")
	}
	// Missing geolocation must never turn into a match, even for negated conditions.
	if c.Field == "country" && v == "" {
		return false
	}
	fold := member(c.Field, []string{"host", "method", "ua_class", "device", "country"})
	if fold {
		v = strings.ToLower(v)
	}
	match := false
	if c.Op == "exists" {
		match = v != ""
		if strings.HasPrefix(c.Field, "header.") {
			match = len(r.Headers.Values(strings.TrimPrefix(c.Field, "header."))) > 0
		}
	} else {
		for _, want := range c.values {
			if fold {
				want = strings.ToLower(want)
			}
			switch c.Op {
			case "eq", "in":
				match = v == want
			case "contains":
				match = strings.Contains(v, want)
			case "prefix":
				match = strings.HasPrefix(v, want)
			case "suffix":
				match = strings.HasSuffix(v, want)
			case "matches":
				match = c.regex.MatchString(v)
			case "in_cidr":
				a, err := netip.ParseAddr(v)
				match = err == nil && c.prefix.Contains(a.Unmap())
			case "in_list":
				match = e.inList(want, r.IP)
			}
			if match {
				break
			}
		}
	}
	if c.Negate {
		return !match
	}
	return match
}

// Acquire must be paired with Release when it succeeds, including for allow-listed requests.
func (e *Engine) Acquire(ip string) bool {
	limit := e.policy.Limits.ConcurrentPerIP
	if e.policy.Mode != "enforce" {
		limit = 0
	}
	return e.state.Acquire(ip, limit)
}
func (e *Engine) Release(ip string) { e.state.Release(ip) }
func (e *Engine) Evaluate(r *Request) Decision {
	if r == nil || e.policy.Mode == "off" {
		return Decision{Action: Allow}
	}
	events := []Event{}
	result := func(a Action, id, name string, status, seconds int) Decision {
		monitor := e.policy.Mode == "monitor" && a != Allow
		if a == Ban && !monitor {
			e.state.ban(r.IP, id, name, seconds)
		}
		if a != Allow {
			class, _ := ClassifyUA(r.UA)
			events = append(events, Event{Time: e.state.clock(), WorkspaceID: r.WorkspaceID, ProjectID: r.ProjectID, RouteHost: r.RouteHost, IP: r.IP, Country: r.Country, Method: r.Method, Host: r.Host, Path: r.Path, UA: r.UA, UAClass: class, RuleID: id, RuleName: name, Action: a, Status: status, Monitored: monitor})
		}
		if monitor || a == Log {
			status = 0
		}
		return Decision{Action: a, RuleID: id, RuleName: name, StatusCode: status, BanSeconds: seconds, Monitored: monitor, Events: events}
	}
	if b, ok := e.state.banned(r.IP); ok {
		return result(Block, "banned", b.name, 403, 0)
	}
	for _, l := range e.lists {
		if l.Kind == "block" && e.inList(l.ID, r.IP) {
			return result(Block, l.ID, l.Name, 403, 0)
		}
	}
	for _, l := range e.lists {
		if l.Kind == "allow" && e.inList(l.ID, r.IP) {
			return result(Allow, l.ID, l.Name, 0, 0)
		}
	}
	for _, rul := range e.rules {
		if !rul.Enabled {
			continue
		}
		matched := rul.Match == "all"
		for _, c := range rul.conditions {
			m := e.condition(c, r)
			if rul.Match == "all" && !m {
				matched = false
				break
			}
			if rul.Match == "any" && m {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		switch rul.Action {
		case Log:
			result(Log, rul.ID, rul.Name, 0, 0)
			continue
		case Allow:
			return result(Allow, rul.ID, rul.Name, 0, 0)
		case RateLimit:
			key := rateKey{rule: rul.ID, ip: r.IP}
			if rul.Rate.By == "ip_path" {
				key.path = r.Path
			}
			if !e.state.over(key, rul.Rate.Requests, rul.Rate.WindowSeconds) {
				continue
			}
			if rul.BanSeconds > 0 {
				d := result(Ban, rul.ID, rul.Name, 429, rul.BanSeconds)
				d.RetryAfter = rul.BanSeconds
				return d
			}
			d := result(RateLimit, rul.ID, rul.Name, 429, 0)
			d.RetryAfter = rul.Rate.WindowSeconds
			return d
		case Ban:
			return result(Ban, rul.ID, rul.Name, 403, rul.BanSeconds)
		default:
			return result(Block, rul.ID, rul.Name, 403, 0)
		}
	}
	for _, m := range Catalog().ManagedRules {
		cfg, ok := e.policy.Managed[m.ID]
		if !ok || !cfg.Enabled || !managedMatch(m.ID, r) {
			continue
		}
		if cfg.Action == Log {
			result(Log, m.ID, m.Name, 0, 0)
			continue
		}
		status := 403
		if cfg.Action == Allow {
			status = 0
		}
		return result(cfg.Action, m.ID, m.Name, status, 0)
	}
	l := e.policy.Limits
	if l.MaxBodyBytes > 0 && (r.ContentLength > l.MaxBodyBytes || int64(len(r.BodySample)) > l.MaxBodyBytes) {
		return result(Block, "__body", "Body size limit", 413, 0)
	}
	urlSize := len(r.Path)
	if r.RawQuery != "" {
		urlSize += 1 + len(r.RawQuery)
	}
	if r.URLBytes > urlSize {
		urlSize = r.URLBytes
	}
	if l.MaxURLBytes > 0 && urlSize > l.MaxURLBytes {
		return result(Block, "__url", "URL size limit", 414, 0)
	}
	size := 0
	for k, vs := range r.Headers {
		for _, v := range vs {
			size += len(k) + len(v) + 4
		}
	}
	if l.MaxHeaderBytes > 0 && size > l.MaxHeaderBytes {
		return result(Block, "__headers", "Header size limit", 431, 0)
	}
	if rate := l.Rate; rate != nil && e.state.over(rateKey{rule: "__workspace", ip: r.IP}, rate.Requests, rate.WindowSeconds) {
		d := result(rate.Action, "__workspace", "Workspace rate limit", 429, rate.BanSeconds)
		d.RetryAfter = rate.WindowSeconds
		if rate.Action == Ban && rate.BanSeconds > 0 {
			d.RetryAfter = rate.BanSeconds
		}
		return d
	}
	return Decision{Action: Allow, Events: events}
}
