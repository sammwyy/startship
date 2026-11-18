package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/sammwyy/startship/internal/firewall"
)

func firewallDuration(value string) (int, error) {
	if strings.HasSuffix(value, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(value, "d"), 64)
		if err != nil || math.IsNaN(days) || math.IsInf(days, 0) || days < 0 || days > 365 {
			return 0, fmt.Errorf("invalid duration %q", value)
		}
		value = strconv.FormatFloat(days*24, 'f', -1, 64) + "h"
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 || d > 365*24*time.Hour || d%time.Second != 0 {
		return 0, fmt.Errorf("invalid duration %q: use whole seconds up to 365d", value)
	}
	return int(d / time.Second), nil
}
func firewallSize(value string) (int64, error) {
	upper := strings.ToUpper(value)
	mult := int64(1)
	for _, unit := range []struct {
		suffix string
		bytes  int64
	}{{"GB", 1000000000}, {"MB", 1000000}, {"KB", 1000}, {"B", 1}} {
		if strings.HasSuffix(upper, unit.suffix) {
			upper = strings.TrimSuffix(upper, unit.suffix)
			mult = unit.bytes
			break
		}
	}
	n, err := strconv.ParseInt(upper, 10, 64)
	if err != nil || n < 0 || n > math.MaxInt64/mult {
		return 0, fmt.Errorf("invalid size %q: use bytes, KB, MB or GB", value)
	}
	return n * mult, nil
}
func firewallRate(value string) (int, int, error) {
	count, window, ok := strings.Cut(value, "/")
	n, err := strconv.Atoi(count)
	if !ok || err != nil || n <= 0 {
		return 0, 0, fmt.Errorf("invalid rate %q: use N/DUR", value)
	}
	seconds, err := firewallDuration(window)
	if err != nil {
		return 0, 0, err
	}
	if seconds == 0 {
		return 0, 0, errors.New("rate window must be positive")
	}
	return n, seconds, nil
}
func parseFirewallArgs(args []string, allowed string) (accessArgs, error) {
	a := accessArgs{flags: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			a.positional = append(a.positional, arg)
			continue
		}
		key, value, has := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if key == "" || !strings.Contains(" "+allowed+" ", " "+key+" ") {
			return a, fmt.Errorf("unknown firewall option --%s", key)
		}
		if key == "any" || key == "follow" || key == "router-only" {
			if has {
				return a, fmt.Errorf("--%s does not take a value", key)
			}
			value = "true"
		} else if !has {
			i++
			if i == len(args) || strings.HasPrefix(args[i], "--") {
				return a, fmt.Errorf("--%s requires a value", key)
			}
			value = args[i]
		}
		if a.has(key) && key != "when" {
			return a, fmt.Errorf("--%s may only be supplied once", key)
		}
		a.flags[key] = append(a.flags[key], value)
	}
	return a, nil
}
func firewallCondition(value string) (firewall.Condition, error) {
	parts := strings.Fields(value)
	if len(parts) < 2 {
		return firewall.Condition{}, errors.New("--when requires 'FIELD OP VALUE'")
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), parts[0]))
	rest = strings.TrimSpace(strings.TrimPrefix(rest, parts[1]))
	if rest == "" && parts[1] != "exists" {
		return firewall.Condition{}, errors.New("--when requires 'FIELD OP VALUE'")
	}
	c := firewall.Condition{Field: parts[0], Op: parts[1], Value: rest}
	if c.Op == "in" {
		values := strings.Split(rest, ",")
		for i := range values {
			values[i] = strings.TrimSpace(values[i])
			if values[i] == "" {
				return c, errors.New("--when in requires nonempty comma-separated values")
			}
		}
		c.Value = values
	}
	return c, nil
}
func (c *cli) firewallPolicy(path string) (firewall.Policy, error) {
	var p firewall.Policy
	v, err := c.request("GET", path, nil)
	if err != nil {
		return p, err
	}
	b, err := json.Marshal(v)
	if err == nil {
		err = json.Unmarshal(b, &p)
	}
	if err != nil {
		return p, fmt.Errorf("invalid firewall policy: %w", err)
	}
	if err = firewall.Validate(p); err != nil {
		return p, fmt.Errorf("invalid firewall policy: %w", err)
	}
	return p, nil
}
func (c *cli) putFirewallPolicy(path string, p firewall.Policy) error {
	if err := firewall.Validate(p); err != nil {
		return err
	}
	v, err := c.request("PUT", path, p)
	if err != nil {
		return err
	}
	return c.print(v)
}
func (c *cli) firewallRequest(method, path string, body any) error {
	v, err := c.request(method, path, body)
	if err != nil {
		return err
	}
	return c.print(v)
}
func (c *cli) firewallTable(path, key string, columns ...string) error {
	v, err := c.request("GET", path, nil)
	if err != nil {
		return err
	}
	if key != "" {
		m, ok := v.(map[string]any)
		if !ok {
			return errors.New("expected firewall policy response")
		}
		v = m[key]
	}
	if v == nil {
		v = []any{}
	}
	if _, ok := v.([]any); !ok {
		return errors.New("expected firewall list response")
	}
	return c.printAccess(v, columns...)
}
func (c *cli) runFirewall(args []string) error {
	if len(args) == 0 {
		return errors.New("firewall action required; run startship help")
	}
	op := args[0]
	args = args[1:]
	if op == "rules" || op == "lists" {
		if len(args) == 0 {
			return errors.New("firewall rules/lists action required")
		}
		op += " " + args[0]
		args = args[1:]
		if op == "lists entry" {
			if len(args) == 0 {
				return errors.New("firewall lists entry action required")
			}
			op += " " + args[0]
			args = args[1:]
		}
	}
	allowed, exists := map[string]string{"show": "", "export": "", "apply": "", "mode": "", "managed": "action", "limits": "rate rate-action ban concurrent max-body max-url", "rules list": "", "rules add": "name when any action rate ban", "rules rm": "", "rules enable": "", "rules disable": "", "rules move": "", "lists list": "", "lists add": "kind", "lists rm": "", "lists entry add": "for", "lists entry rm": "", "ban": "for", "unban": "", "events": "follow"}[op]
	if !exists {
		return fmt.Errorf("unknown firewall action %q", op)
	}
	a, err := parseFirewallArgs(args, allowed)
	if err != nil {
		return err
	}
	p := a.positional
	n := 1
	switch op {
	case "apply", "mode", "rules rm", "rules enable", "rules disable", "lists add", "lists rm", "ban", "unban":
		n = 2
	case "managed", "rules move", "lists entry add", "lists entry rm":
		n = 3
	}
	if len(p) != n {
		return fmt.Errorf("invalid firewall %s arguments; run startship help", op)
	}
	// Validate flags before connecting or changing a policy.
	for _, key := range []string{"ban", "for"} {
		if a.has(key) {
			if _, err = firewallDuration(a.value(key)); err != nil {
				return err
			}
		}
	}
	if a.has("rate") {
		if _, _, err = firewallRate(a.value("rate")); err != nil {
			return err
		}
	}
	for _, key := range []string{"max-body", "max-url"} {
		if a.has(key) {
			if _, err = firewallSize(a.value(key)); err != nil {
				return err
			}
		}
	}
	if a.has("concurrent") {
		v, e := strconv.Atoi(a.value("concurrent"))
		if e != nil || v < 0 {
			return errors.New("--concurrent must be a nonnegative integer")
		}
	}
	if op == "mode" && p[1] != "off" && p[1] != "monitor" && p[1] != "enforce" {
		return errors.New("mode must be off, monitor or enforce")
	}
	if op == "managed" && p[2] != "on" && p[2] != "off" {
		return errors.New("managed state must be on or off")
	}
	if op == "ban" || op == "unban" {
		if _, err := netip.ParseAddr(p[1]); err != nil {
			return errors.New("ban/unban requires an IP address")
		}
	}
	if op == "lists entry add" || op == "lists entry rm" {
		if _, err := netip.ParseAddr(p[2]); err != nil {
			if _, err = netip.ParsePrefix(p[2]); err != nil {
				return errors.New("entry value must be an IP or CIDR")
			}
		}
	}
	ws, err := c.resolve("workspaces", p[0])
	if err != nil {
		return err
	}
	path := "workspaces/" + url.PathEscape(ws) + "/firewall"
	switch op {
	case "show":
		v, err := c.request("GET", path, nil)
		if err != nil {
			return err
		}
		return c.print(v)
	case "export":
		v, err := c.request("GET", path, nil)
		if err != nil {
			return err
		}
		e := json.NewEncoder(c.out)
		e.SetIndent("", "  ")
		return e.Encode(v)
	case "apply":
		b, err := os.ReadFile(p[1])
		if err != nil {
			return err
		}
		var policy firewall.Policy
		if err = json.Unmarshal(b, &policy); err != nil {
			return fmt.Errorf("invalid policy JSON: %w", err)
		}
		if err = firewall.Validate(policy); err != nil {
			return err
		}
		return c.firewallRequest("PUT", path, json.RawMessage(b))
	case "mode", "managed", "limits":
		policy, err := c.firewallPolicy(path)
		if err != nil {
			return err
		}
		switch op {
		case "mode":
			policy.Mode = p[1]
		case "managed":
			info, found := firewall.ManagedInfo{}, false
			for _, m := range firewall.Catalog().ManagedRules {
				if m.ID == p[1] {
					info, found = m, true
					break
				}
			}
			if !found {
				return fmt.Errorf("unknown managed rule %q", p[1])
			}
			if policy.Managed == nil {
				policy.Managed = map[string]firewall.ManagedRule{}
			}
			rule, ok := policy.Managed[p[1]]
			if !ok {
				rule.Action = info.DefaultAction
			}
			rule.Enabled = p[2] == "on"
			if a.has("action") {
				rule.Action = firewall.Action(a.value("action"))
			}
			policy.Managed[p[1]] = rule
		case "limits":
			if len(a.flags) == 0 {
				return c.print(policy.Limits)
			}
			l := &policy.Limits
			if a.has("rate") {
				n, seconds, _ := firewallRate(a.value("rate"))
				if l.Rate == nil {
					l.Rate = &firewall.WorkspaceRate{Action: firewall.Block}
				}
				l.Rate.Requests, l.Rate.WindowSeconds = n, seconds
			}
			if a.has("rate-action") || a.has("ban") {
				if l.Rate == nil {
					return errors.New("--rate-action/--ban requires an existing rate or --rate")
				}
			}
			if a.has("rate-action") {
				l.Rate.Action = firewall.Action(a.value("rate-action"))
			}
			if a.has("ban") {
				l.Rate.BanSeconds, _ = firewallDuration(a.value("ban"))
			}
			if a.has("concurrent") {
				l.ConcurrentPerIP, _ = strconv.Atoi(a.value("concurrent"))
			}
			if a.has("max-body") {
				l.MaxBodyBytes, _ = firewallSize(a.value("max-body"))
			}
			if a.has("max-url") {
				size, _ := firewallSize(a.value("max-url"))
				if size > int64(int(^uint(0)>>1)) {
					return errors.New("--max-url is too large")
				}
				l.MaxURLBytes = int(size)
			}
		}
		return c.putFirewallPolicy(path, policy)
	case "rules list":
		return c.firewallTable(path, "rules", "id", "name", "enabled", "match", "action", "conditions", "rate", "ban_seconds")
	case "rules add":
		if !a.has("name") || strings.TrimSpace(a.value("name")) == "" || !a.has("when") || !a.has("action") {
			return errors.New("rules add requires --name, --when and --action")
		}
		rule := firewall.Rule{Name: a.value("name"), Enabled: true, Match: "all", Action: firewall.Action(a.value("action"))}
		if a.has("any") {
			rule.Match = "any"
		}
		for _, value := range a.flags["when"] {
			cond, err := firewallCondition(value)
			if err != nil {
				return err
			}
			rule.Conditions = append(rule.Conditions, cond)
		}
		if a.has("rate") {
			n, seconds, _ := firewallRate(a.value("rate"))
			rule.Rate = &firewall.Rate{Requests: n, WindowSeconds: seconds, By: "ip"}
		}
		if a.has("ban") {
			rule.BanSeconds, _ = firewallDuration(a.value("ban"))
			if rule.Action != firewall.Ban && rule.Action != firewall.RateLimit {
				return errors.New("--ban requires action ban or rate_limit")
			}
		}
		test := firewall.DefaultPolicy()
		rule.ID = "cli-validation"
		test.Rules = []firewall.Rule{rule}
		if err = firewall.Validate(test); err != nil {
			return err
		}
		rule.ID = ""
		return c.firewallRequest("POST", path+"/rules", rule)
	case "rules rm":
		return c.firewallRequest("DELETE", path+"/rules/"+url.PathEscape(p[1]), nil)
	case "rules enable", "rules disable", "rules move":
		policy, err := c.firewallPolicy(path)
		if err != nil {
			return err
		}
		idx := -1
		for i, r := range policy.Rules {
			if r.ID == p[1] {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("rule %q not found", p[1])
		}
		if op != "rules move" {
			rule := policy.Rules[idx]
			rule.Enabled = op == "rules enable"
			return c.firewallRequest("PUT", path+"/rules/"+url.PathEscape(rule.ID), rule)
		}
		pos, err := strconv.Atoi(p[2])
		if err != nil || pos < 1 || pos > len(policy.Rules) {
			return fmt.Errorf("position must be between 1 and %d", len(policy.Rules))
		}
		rule := policy.Rules[idx]
		rules := append(policy.Rules[:idx], policy.Rules[idx+1:]...)
		rules = append(rules, firewall.Rule{})
		copy(rules[pos:], rules[pos-1:])
		rules[pos-1] = rule
		ids := []string{}
		for _, r := range rules {
			ids = append(ids, r.ID)
		}
		return c.firewallRequest("POST", path+"/rules/order", map[string]any{"ids": ids})
	case "lists list":
		return c.firewallTable(path+"/lists", "", "id", "name", "kind", "entries")
	case "lists add":
		kind := "block"
		if a.has("kind") {
			kind = a.value("kind")
		}
		if kind != "allow" && kind != "block" {
			return errors.New("--kind must be block or allow")
		}
		return c.firewallRequest("POST", path+"/lists", firewall.List{Name: p[1], Kind: kind, Entries: []firewall.Entry{}})
	case "lists rm":
		return c.firewallRequest("DELETE", path+"/lists/"+url.PathEscape(p[1]), nil)
	case "lists entry add":
		seconds, _ := firewallDuration(a.value("for"))
		return c.firewallRequest("POST", path+"/lists/"+url.PathEscape(p[1])+"/entries", map[string]any{"value": p[2], "ttl_seconds": seconds})
	case "lists entry rm":
		return c.firewallRequest("DELETE", path+"/lists/"+url.PathEscape(p[1])+"/entries/"+url.PathEscape(p[2]), nil)
	case "ban":
		seconds, _ := firewallDuration(a.value("for"))
		return c.firewallRequest("POST", path+"/ban", map[string]any{"ip": p[1], "ttl_seconds": seconds})
	case "unban":
		return c.firewallRequest("DELETE", path+"/ban/"+url.PathEscape(p[1]), nil)
	case "events":
		if !a.has("follow") {
			return c.firewallTable(path+"/events", "", "time", "ip", "country", "method", "path", "rule_id", "action", "status", "monitored")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return c.followFirewall(ctx, ws)
	}
	return errors.New("unsupported firewall command")
}

func (c *cli) followFirewall(ctx context.Context, workspace string) error {
	server, client, err := c.transport()
	if err != nil {
		return err
	}
	streamClient := *client
	streamClient.Timeout = 0
	headers := http.Header{}
	if c.Token != "" {
		headers.Set("Cookie", (&http.Cookie{Name: "startship_session", Value: c.Token}).String())
	}
	conn, resp, err := websocket.Dial(ctx, server+"/api/live", &websocket.DialOptions{HTTPClient: &streamClient, HTTPHeader: headers})
	if err != nil {
		if resp != nil {
			return fmt.Errorf("firewall live: %s: %w", resp.Status, err)
		}
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 20)
	subscribe := map[string]string{"type": "subscribe", "id": "firewall", "topic": "firewall", "workspace_id": workspace}
	b, _ := json.Marshal(subscribe)
	if err = conn.Write(ctx, websocket.MessageText, b); err != nil {
		return err
	}
	previous := map[string]int{}
	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) || websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return nil
			}
			return err
		}
		var message struct {
			Type  string `json:"type"`
			ID    string `json:"id"`
			Error string `json:"error"`
			Data  struct {
				Events []json.RawMessage `json:"events"`
			} `json:"data"`
		}
		if err = json.Unmarshal(b, &message); err != nil {
			return fmt.Errorf("invalid firewall live message: %w", err)
		}
		if message.Type == "error" {
			return fmt.Errorf("firewall subscription: %s", message.Error)
		}
		if message.Type != "snapshot" || message.ID != "firewall" {
			continue
		}
		counts := map[string]int{}
		rows := []any{}
		for i := len(message.Data.Events) - 1; i >= 0; i-- {
			raw := message.Data.Events[i]
			var event map[string]any
			if err = json.Unmarshal(raw, &event); err != nil {
				return err
			}
			keyBytes, _ := json.Marshal(event)
			key := string(keyBytes)
			counts[key]++
			if counts[key] <= previous[key] {
				continue
			}
			if c.raw {
				if err = json.NewEncoder(c.out).Encode(event); err != nil {
					return err
				}
			} else {
				rows = append(rows, event)
			}
		}
		if !c.raw && len(rows) > 0 {
			if err = c.printAccess(rows, "time", "ip", "country", "method", "path", "rule_id", "action", "status", "monitored"); err != nil {
				return err
			}
		}
		previous = counts
	}
}
