package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/sammwyy/startship/internal/firewall"
)

type firewallAPIError struct {
	status  int
	message string
}

func (e *firewallAPIError) Error() string            { return e.message }
func firewallError(status int, message string) error { return &firewallAPIError{status, message} }
func readFirewallJSON(r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 2<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return firewallError(400, "invalid JSON: "+err.Error())
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return firewallError(400, "invalid JSON: expected one value")
	}
	return nil
}

func (a *Server) firewallAPI(w http.ResponseWriter, r *http.Request, path []string, uid string, x *access) bool {
	path = strings.Split(strings.Trim(r.URL.EscapedPath(), "/"), "/")
	for i, part := range path {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			fail(w, 400, "invalid path")
			return true
		}
		path[i] = decoded
	}
	if len(path) == 3 && path[1] == "firewall" && path[2] == "catalog" {
		if r.Method != "GET" {
			fail(w, 405, "method not allowed")
		} else {
			c := firewall.Catalog()
			c.GeoIP = a.routerOptions.GeoIP != nil
			reply(w, 200, c)
		}
		return true
	}
	if len(path) < 4 || path[1] != "workspaces" || path[3] != "firewall" {
		return false
	}
	ws, err := a.store.Get(path[2])
	if err != nil || ws.Kind != "workspaces" || !x.visible(ws) {
		fail(w, 404, "not found")
		return true
	}
	perm := "firewall.view"
	if r.Method != "GET" {
		perm = "firewall.manage"
	}
	if !x.can(ws, perm) {
		forbidden(w)
		return true
	}
	if r.Method == "GET" {
		if len(path) == 5 && path[4] == "events" {
			events, _ := a.firewallData(ws.ID, time.Now())
			reply(w, 200, events)
			return true
		}
		if len(path) != 4 && (len(path) != 5 || path[4] != "lists") {
			fail(w, 404, "not found")
			return true
		}
		p, lists, err := a.firewallConfig(ws.ID)
		if err != nil {
			fail(w, 500, "firewall storage unavailable")
		} else if len(path) == 4 {
			reply(w, 200, p)
		} else {
			reply(w, 200, lists)
		}
		return true
	}
	a.routerRefresh.Lock()
	defer a.routerRefresh.Unlock()
	p, lists, err := a.loadFirewall(ws.ID)
	if err != nil {
		fail(w, 500, "firewall storage unavailable")
		return true
	}
	var result any
	status := 200
	switch {
	case len(path) == 4:
		if r.Method != "PUT" {
			err = firewallError(405, "method not allowed")
			break
		}
		var policy firewall.Policy
		if err = readFirewallJSON(r, &policy); err != nil {
			break
		}
		if err = firewall.Validate(policy); err != nil {
			err = firewallError(400, err.Error())
			break
		}
		err = a.saveFirewallPolicy(ws.ID, uid, "firewall.policy.update", ws.ID, policy)
	case path[4] == "rules":
		result, status, err = a.firewallRules(r, path[5:], ws.ID, uid, p)
	case path[4] == "lists":
		result, status, err = a.firewallLists(r, path[5:], ws.ID, uid, lists)
	case path[4] == "ban":
		result, err = a.firewallBanAPI(r, path[5:], ws.ID, uid, lists)
	default:
		err = firewallError(404, "not found")
	}
	if err != nil {
		if e, ok := err.(*firewallAPIError); ok {
			fail(w, e.status, e.message)
		} else {
			fail(w, 500, "firewall storage unavailable")
		}
		return true
	}
	if err = a.refreshLocked(); err != nil {
		fail(w, 500, "firewall refresh failed")
		return true
	}
	a.firewallChanged(ws.ID)
	if result == nil {
		p, _, err = a.loadFirewall(ws.ID)
		if err != nil {
			fail(w, 500, "firewall storage unavailable")
			return true
		}
		result = p
	}
	reply(w, status, result)
	return true
}
func (a *Server) firewallRules(r *http.Request, path []string, workspace, uid string, p firewall.Policy) (any, int, error) {
	action, target := "", ""
	switch {
	case len(path) == 1 && path[0] == "order" && r.Method == "POST":
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := readFirewallJSON(r, &in); err != nil {
			return nil, 0, err
		}
		if len(in.IDs) != len(p.Rules) {
			return nil, 0, firewallError(400, "ids must contain every rule exactly once")
		}
		byID := map[string]firewall.Rule{}
		for _, v := range p.Rules {
			byID[v.ID] = v
		}
		ordered := []firewall.Rule{}
		for _, id := range in.IDs {
			v, ok := byID[id]
			if !ok {
				return nil, 0, firewallError(400, "ids must contain every rule exactly once")
			}
			ordered = append(ordered, v)
			delete(byID, id)
		}
		p.Rules = ordered
		action = "firewall.rule.order"
	case len(path) == 0 && r.Method == "POST" || len(path) == 1 && r.Method == "PUT":
		var rule firewall.Rule
		if err := readFirewallJSON(r, &rule); err != nil {
			return nil, 0, err
		}
		if len(path) == 0 {
			if rule.ID == "" {
				rule.ID = randomID()
			}
			p.Rules = append(p.Rules, rule)
			action = "firewall.rule.create"
		} else {
			if rule.ID != "" && rule.ID != path[0] {
				return nil, 0, firewallError(400, "rule id does not match URL")
			}
			rule.ID = path[0]
			found := false
			for i, v := range p.Rules {
				if v.ID == rule.ID {
					p.Rules[i] = rule
					found = true
					break
				}
			}
			if !found {
				return nil, 0, firewallError(404, "rule not found")
			}
			action = "firewall.rule.update"
		}
		target = rule.ID
	case len(path) == 1 && r.Method == "DELETE":
		found := false
		for i, v := range p.Rules {
			if v.ID == path[0] {
				p.Rules = append(p.Rules[:i], p.Rules[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return nil, 0, firewallError(404, "rule not found")
		}
		action, target = "firewall.rule.delete", path[0]
	default:
		return nil, 0, firewallError(405, "method not allowed")
	}
	if err := firewall.Validate(p); err != nil {
		return nil, 0, firewallError(400, err.Error())
	}
	return nil, 200, a.saveFirewallPolicy(workspace, uid, action, target, p)
}

func validateFirewallList(l firewall.List) error {
	if l.ID == "" || len(l.ID) > 128 || strings.ContainsAny(l.ID, "/\\%?#") || strings.TrimSpace(l.ID) != l.ID {
		return fmt.Errorf("lists: invalid id")
	}
	if strings.TrimSpace(l.Name) == "" || len(l.Name) > 200 {
		return fmt.Errorf("lists: name must be 1-200 characters")
	}
	if l.Kind != "block" && l.Kind != "allow" {
		return fmt.Errorf("lists: kind must be block or allow")
	}
	if l.ID == "banned" && l.Kind != "block" {
		return fmt.Errorf("lists: banned must be a block list")
	}
	seen := map[string]bool{}
	for i, e := range l.Entries {
		if _, err := netip.ParseAddr(e.Value); err != nil {
			if _, err = netip.ParsePrefix(e.Value); err != nil {
				return fmt.Errorf("lists.entries[%d].value: invalid IP or CIDR", i)
			}
		}
		if seen[e.Value] {
			return fmt.Errorf("lists.entries[%d].value: duplicate entry", i)
		}
		seen[e.Value] = true
	}
	return nil
}
func readFirewallEntry(r *http.Request, uid string, ipOnly bool) (firewall.Entry, error) {
	var in struct {
		Value string `json:"value"`
		IP    string `json:"ip"`
		TTL   int    `json:"ttl_seconds"`
		Note  string `json:"note"`
	}
	if err := readFirewallJSON(r, &in); err != nil {
		return firewall.Entry{}, err
	}
	if in.TTL < 0 || in.TTL > 365*24*60*60 {
		return firewall.Entry{}, firewallError(400, "ttl_seconds: must be between 0 and 31536000")
	}
	if ipOnly {
		in.Value = in.IP
		if _, err := netip.ParseAddr(in.Value); err != nil {
			return firewall.Entry{}, firewallError(400, "ip: invalid IP address")
		}
	}
	e := firewall.Entry{Value: in.Value, ExpiresAt: firewallExpiry(in.TTL), Note: in.Note, CreatedBy: uid}
	if err := validateFirewallList(firewall.List{ID: "input", Name: "input", Kind: "block", Entries: []firewall.Entry{e}}); err != nil {
		return e, firewallError(400, err.Error())
	}
	return e, nil
}
func deleteFirewallEntry(list *firewall.List, value string) bool {
	for i, e := range list.Entries {
		if e.Value == value {
			list.Entries = append(list.Entries[:i], list.Entries[i+1:]...)
			return true
		}
	}
	return false
}
func (a *Server) firewallLists(r *http.Request, path []string, workspace, uid string, lists []firewall.List) (any, int, error) {
	if len(path) == 0 && r.Method == "POST" || len(path) == 1 && r.Method == "PUT" {
		var l firewall.List
		if err := readFirewallJSON(r, &l); err != nil {
			return nil, 0, err
		}
		action := "firewall.list.create"
		if len(path) == 0 {
			if l.ID == "" {
				l.ID = randomID()
			}
			if findFirewallList(lists, l.ID) != nil {
				return nil, 0, firewallError(409, "list already exists")
			}
		} else {
			if l.ID != "" && l.ID != path[0] {
				return nil, 0, firewallError(400, "list id does not match URL")
			}
			l.ID = path[0]
			if findFirewallList(lists, l.ID) == nil {
				return nil, 0, firewallError(404, "list not found")
			}
			action = "firewall.list.update"
		}
		if l.Entries == nil {
			l.Entries = []firewall.Entry{}
		}
		for i := range l.Entries {
			l.Entries[i].CreatedBy = uid
		}
		if err := validateFirewallList(l); err != nil {
			return nil, 0, firewallError(400, err.Error())
		}
		return l, 200, a.saveFirewallList(workspace, uid, action, l)
	}
	if len(path) == 0 {
		return nil, 0, firewallError(405, "method not allowed")
	}
	l := findFirewallList(lists, path[0])
	if l == nil {
		return nil, 0, firewallError(404, "list not found")
	}
	if len(path) == 1 && r.Method == "DELETE" {
		if l.ID == "banned" {
			return nil, 0, firewallError(400, "the implicit banned list cannot be deleted")
		}
		return map[string]bool{"ok": true}, 200, a.deleteFirewallList(workspace, uid, l.ID)
	}
	if len(path) >= 2 && path[1] == "entries" {
		action := ""
		if len(path) == 2 && r.Method == "POST" {
			e, err := readFirewallEntry(r, uid, false)
			if err != nil {
				return nil, 0, err
			}
			upsertFirewallEntry(l, e)
			action = "firewall.entry.create"
		} else if len(path) >= 3 && r.Method == "DELETE" {
			// URL.Path is decoded by net/http; CIDRs may contain an escaped slash.
			if !deleteFirewallEntry(l, strings.Join(path[2:], "/")) {
				return nil, 0, firewallError(404, "entry not found")
			}
			action = "firewall.entry.delete"
		} else {
			return nil, 0, firewallError(405, "method not allowed")
		}
		return *l, 200, a.saveFirewallList(workspace, uid, action, *l)
	}
	return nil, 0, firewallError(405, "method not allowed")
}
func (a *Server) firewallBanAPI(r *http.Request, path []string, workspace, uid string, lists []firewall.List) (any, error) {
	l := findFirewallList(lists, "banned")
	action := ""
	if len(path) == 0 && r.Method == "POST" {
		e, err := readFirewallEntry(r, uid, true)
		if err != nil {
			return nil, err
		}
		upsertFirewallEntry(l, e)
		action = "firewall.ban.create"
	} else if len(path) == 1 && r.Method == "DELETE" {
		if _, err := netip.ParseAddr(path[0]); err != nil {
			return nil, firewallError(400, "ip: invalid IP address")
		}
		if !deleteFirewallEntry(l, path[0]) {
			return nil, firewallError(404, "ban not found")
		}
		action = "firewall.ban.delete"
	} else {
		return nil, firewallError(405, "method not allowed")
	}
	return *l, a.saveFirewallList(workspace, uid, action, *l)
}
