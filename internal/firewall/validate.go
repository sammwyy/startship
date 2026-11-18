package firewall

import (
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
)

func values(v any) ([]string, bool) {
	switch v := v.(type) {
	case string:
		return []string{v}, true
	case []string:
		return v, true
	case []any:
		out := make([]string, len(v))
		for i, x := range v {
			s, ok := x.(string)
			if !ok {
				return nil, false
			}
			out[i] = s
		}
		return out, true
	}
	return nil, false
}
func member[T comparable](v T, vs []T) bool {
	for _, x := range vs {
		if x == v {
			return true
		}
	}
	return false
}
func fieldInfo(name string) (FieldInfo, bool) {
	for _, f := range Catalog().Fields {
		if f.ID == name && f.ID != "header.<name>" || f.ID == "header.<name>" && strings.HasPrefix(name, "header.") && validHeader(strings.TrimPrefix(name, "header.")) {
			return f, true
		}
	}
	return FieldInfo{}, false
}
func validHeader(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return false
		}
	}
	return http.CanonicalHeaderKey(s) != ""
}
func Validate(p Policy) error {
	if !member(p.Mode, []string{"off", "monitor", "enforce"}) {
		return fmt.Errorf("mode: must be off, monitor or enforce")
	}
	for id, m := range p.Managed {
		found := false
		for _, v := range Catalog().ManagedRules {
			if v.ID == id {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("managed.%s: unknown managed rule", id)
		}
		if !member(m.Action, []Action{Block, Allow, Log, Ban}) {
			return fmt.Errorf("managed.%s.action: invalid action %q", id, m.Action)
		}
	}
	l := p.Limits
	for _, v := range []struct {
		name string
		n    int64
	}{{"max_body_bytes", l.MaxBodyBytes}, {"max_url_bytes", int64(l.MaxURLBytes)}, {"max_header_bytes", int64(l.MaxHeaderBytes)}, {"concurrent_per_ip", int64(l.ConcurrentPerIP)}} {
		if v.n < 0 {
			return fmt.Errorf("limits.%s: must not be negative", v.name)
		}
	}
	if r := l.Rate; r != nil {
		if r.Requests <= 0 {
			return fmt.Errorf("limits.rate.requests: must be positive")
		}
		if r.WindowSeconds <= 0 || r.WindowSeconds > 31536000 {
			return fmt.Errorf("limits.rate.window_seconds: must be between 1 and 31536000")
		}
		if !member(r.Action, []Action{Block, Ban}) {
			return fmt.Errorf("limits.rate.action: must be block or ban")
		}
		if r.BanSeconds < 0 || r.BanSeconds > 31536000 {
			return fmt.Errorf("limits.rate.ban_seconds: must be between 0 and 31536000")
		}
	}
	ids := map[string]bool{}
	for i, r := range p.Rules {
		path := fmt.Sprintf("rules[%d]", i)
		if r.ID == "" || ids[r.ID] || strings.HasPrefix(r.ID, "__") {
			return fmt.Errorf("%s.id: must be nonempty, unique and not start with __", path)
		}
		ids[r.ID] = true
		if !member(r.Match, []string{"all", "any"}) {
			return fmt.Errorf("%s.match: must be all or any", path)
		}
		if !member(r.Action, Catalog().Actions) {
			return fmt.Errorf("%s.action: invalid action %q", path, r.Action)
		}
		if r.BanSeconds < 0 || r.BanSeconds > 31536000 {
			return fmt.Errorf("%s.ban_seconds: must be between 0 and 31536000", path)
		}
		if r.Action == RateLimit && r.Rate == nil {
			return fmt.Errorf("%s.rate: required for rate_limit", path)
		}
		if r.Rate != nil {
			if r.Action != RateLimit {
				return fmt.Errorf("%s.rate: only valid for rate_limit", path)
			}
			if r.Rate.Requests <= 0 {
				return fmt.Errorf("%s.rate.requests: must be positive", path)
			}
			if r.Rate.WindowSeconds <= 0 || r.Rate.WindowSeconds > 31536000 {
				return fmt.Errorf("%s.rate.window_seconds: must be between 1 and 31536000", path)
			}
			if !member(r.Rate.By, []string{"ip", "ip_path"}) {
				return fmt.Errorf("%s.rate.by: must be ip or ip_path", path)
			}
		}
		if len(r.Conditions) < 1 || len(r.Conditions) > 16 {
			return fmt.Errorf("%s.conditions: must contain 1..16 conditions", path)
		}
		for j, c := range r.Conditions {
			cp := fmt.Sprintf("%s.conditions[%d]", path, j)
			f, ok := fieldInfo(c.Field)
			if !ok {
				return fmt.Errorf("%s: unknown field %q", cp, c.Field)
			}
			if !member(c.Op, f.Ops) {
				return fmt.Errorf("%s.op: unsupported operator %q for %s", cp, c.Op, c.Field)
			}
			vs, ok := values(c.Value)
			if !ok {
				return fmt.Errorf("%s.value: must be a string or string array", cp)
			}
			if c.Op == "in" {
				if len(vs) == 0 {
					return fmt.Errorf("%s.value: must not be empty", cp)
				}
			} else {
				if _, ok := c.Value.(string); !ok {
					return fmt.Errorf("%s.value: must be a string for %s", cp, c.Op)
				}
			}
			for _, v := range vs {
				if len(f.Values) > 0 && c.Op != "exists" && !member(strings.ToLower(v), f.Values) && member(c.Op, []string{"eq", "in"}) {
					return fmt.Errorf("%s.value: invalid %s value %q", cp, c.Field, v)
				}
				if c.Field == "country" && !validCountry(v) {
					return fmt.Errorf("%s.value: country must be ISO-2", cp)
				}
				switch c.Op {
				case "matches":
					if len(v) > 512 {
						return fmt.Errorf("%s.value: regex exceeds 512 characters", cp)
					}
					if _, err := regexp.Compile(v); err != nil {
						return fmt.Errorf("%s.value: invalid regex: %w", cp, err)
					}
				case "in_cidr":
					if _, err := netip.ParsePrefix(v); err != nil {
						if _, err = netip.ParseAddr(v); err != nil {
							return fmt.Errorf("%s.value: invalid IP or CIDR", cp)
						}
					}
				case "in_list":
					if v == "" {
						return fmt.Errorf("%s.value: list id is required", cp)
					}
				}
			}
		}
	}
	return nil
}
