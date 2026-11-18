package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/sammwyy/startship/internal/netfw"
)

func networkValue(value string) (netfw.Rule, error) {
	r := netfw.Rule{Value: value, Kind: "country"}
	if _, err := netip.ParseAddr(value); err == nil {
		r.Kind = "ip"
	} else if strings.Contains(value, "/") {
		r.Kind = "cidr"
	}
	err := netfw.Normalize(&r)
	return r, err
}

func (c *cli) runNetwork(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: network status|list|block|unblock")
	}
	allowed := ""
	if args[0] == "block" {
		allowed = "for router-only note"
	}
	a, err := parseFirewallArgs(args[1:], allowed)
	if err != nil {
		return err
	}
	p := a.positional
	const path = "system/network-firewall"
	switch args[0] {
	case "status", "list":
		if len(p) != 0 {
			return fmt.Errorf("usage: network %s", args[0])
		}
		v, err := c.request("GET", path, nil)
		if err != nil {
			return err
		}
		if args[0] == "status" {
			return c.print(v)
		}
		m, ok := v.(map[string]any)
		if !ok {
			return errors.New("invalid network firewall response")
		}
		return c.printAccess(m["rules"], "id", "kind", "value", "ports", "expires_at", "note")
	case "block":
		if len(p) != 1 {
			return errors.New("usage: network block VALUE [--for DUR] [--router-only] [--note TEXT]")
		}
		r, err := networkValue(p[0])
		if err != nil {
			return err
		}
		seconds := 0
		if a.has("for") {
			seconds, err = firewallDuration(a.value("for"))
			if err != nil {
				return err
			}
		}
		if a.has("router-only") {
			r.Ports = "router"
		}
		r.Note = a.value("note")
		if err = netfw.Normalize(&r); err != nil {
			return err
		}
		body := map[string]any{"kind": r.Kind, "value": r.Value, "ports": r.Ports, "note": r.Note, "ttl_seconds": seconds}
		v, err := c.request("POST", path+"/rules", body)
		if err != nil {
			return err
		}
		return c.print(v)
	case "unblock":
		if len(p) != 1 {
			return errors.New("usage: network unblock ID|VALUE")
		}
		v, err := c.request("GET", path, nil)
		if err != nil {
			return err
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var status struct {
			Rules []netfw.Rule `json:"rules"`
		}
		if err = json.Unmarshal(b, &status); err != nil {
			return err
		}
		ids := []string{}
		for _, r := range status.Rules {
			if r.ID == p[0] {
				ids = []string{r.ID}
				break
			}
		}
		if len(ids) == 0 {
			match, err := networkValue(p[0])
			if err == nil {
				for _, r := range status.Rules {
					if r.Kind == match.Kind && r.Value == match.Value {
						ids = append(ids, r.ID)
					}
				}
			}
		}
		if len(ids) == 0 {
			return fmt.Errorf("network rule %q not found", p[0])
		}
		for _, id := range ids {
			v, err = c.request("DELETE", path+"/rules/"+url.PathEscape(id), nil)
			if err != nil {
				return err
			}
		}
		return c.print(v)
	default:
		return fmt.Errorf("unknown network command %q", args[0])
	}
}
