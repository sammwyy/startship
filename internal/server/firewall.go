package server

import (
	"sort"
	"sync"
	"time"

	"github.com/sammwyy/startship/internal/firewall"
	"github.com/sammwyy/startship/internal/router"
)

type firewallRuntime struct {
	mu         sync.Mutex
	states     map[string]*firewall.State
	workspaces map[string]*firewallMetrics
}
type firewallMetrics struct {
	events      [500]firewall.Event
	next, count int
	minutes     [60]firewallMinute
}
type firewallMinute struct {
	At                    int64            `json:"at"`
	Total                 int64            `json:"total"`
	Blocked               int64            `json:"blocked"`
	ByAction              map[string]int64 `json:"by_action"`
	rules, ips, countries map[string]int64
}
type firewallTop struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}
type firewallStats struct {
	Total        int64            `json:"total"`
	Blocked      int64            `json:"blocked"`
	ByAction     map[string]int64 `json:"by_action"`
	Minutes      []firewallMinute `json:"minutes"`
	TopRules     []firewallTop    `json:"top_rules"`
	TopIPs       []firewallTop    `json:"top_ips"`
	TopCountries []firewallTop    `json:"top_countries"`
}

func (f *firewallRuntime) workspace(id string) *firewallMetrics {
	if f.workspaces == nil {
		f.workspaces = map[string]*firewallMetrics{}
	}
	if f.workspaces[id] == nil {
		f.workspaces[id] = &firewallMetrics{}
	}
	return f.workspaces[id]
}
func (m *firewallMetrics) minute(at time.Time) *firewallMinute {
	stamp := at.Unix() / 60 * 60
	idx := (stamp / 60) % 60
	if idx < 0 {
		idx += 60
	}
	b := &m.minutes[idx]
	if b.At != stamp || b.ByAction == nil {
		*b = firewallMinute{At: stamp, ByAction: map[string]int64{}, rules: map[string]int64{}, ips: map[string]int64{}, countries: map[string]int64{}}
	}
	return b
}
func (a *Server) recordFirewallEvent(e firewall.Event) {
	a.firewall.mu.Lock()
	defer func() { a.firewall.mu.Unlock(); a.live.publish(liveEvent{firewallWorkspace: e.WorkspaceID}) }()
	m := a.firewall.workspace(e.WorkspaceID)
	m.events[m.next] = e
	m.next = (m.next + 1) % len(m.events)
	if m.count < len(m.events) {
		m.count++
	}
	b := m.minute(e.Time)
	if e.RuleID != "" {
		b.rules[e.RuleID]++
	}
	if e.IP != "" {
		b.ips[e.IP]++
	}
	if e.Country != "" {
		b.countries[e.Country]++
	}
}
func (a *Server) observeFirewall(workspace string, req *firewall.Request, d firewall.Decision) {
	a.firewall.mu.Lock()
	defer func() { a.firewall.mu.Unlock(); a.live.publish(liveEvent{firewallWorkspace: workspace}) }()
	b := a.firewall.workspace(workspace).minute(time.Now().UTC())
	b.Total++
	if d.StatusCode >= 400 && !d.Monitored {
		b.Blocked++
	}
	b.ByAction[string(d.Action)]++
}
func (a *Server) recordFirewallBan(b router.Ban) error {
	a.routerRefresh.Lock()
	defer a.routerRefresh.Unlock()
	_, lists, err := a.loadFirewall(b.WorkspaceID)
	if err != nil {
		return err
	}
	list := findFirewallList(lists, "banned")
	entry := firewall.Entry{Value: b.IP, ExpiresAt: firewallExpiry(b.Seconds), Note: b.RuleName, CreatedBy: "firewall"}
	upsertFirewallEntry(list, entry)
	if err = a.saveFirewallList(b.WorkspaceID, "", "firewall.ban.create", *list); err != nil {
		return err
	}
	if err = a.refreshLocked(); err != nil {
		return err
	}
	a.firewallChanged(b.WorkspaceID)
	return nil
}
func (a *Server) firewallChanged(workspace string) {
	a.live.publish(liveEvent{firewallWorkspace: workspace, firewallConfig: true})
}
func topFirewall(counts map[string]int64) []firewallTop {
	out := make([]firewallTop, 0, len(counts))
	for v, n := range counts {
		out = append(out, firewallTop{v, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Value < out[j].Value
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}
func (a *Server) firewallData(workspace string, now time.Time) ([]firewall.Event, firewallStats) {
	a.firewall.mu.Lock()
	defer a.firewall.mu.Unlock()
	m := a.firewall.workspace(workspace)
	events := make([]firewall.Event, 0, m.count)
	for i := 0; i < m.count; i++ {
		idx := (m.next - 1 - i + len(m.events)) % len(m.events)
		events = append(events, m.events[idx])
	}
	stats := firewallStats{ByAction: map[string]int64{}, Minutes: make([]firewallMinute, 60)}
	rules, ips, countries := map[string]int64{}, map[string]int64{}, map[string]int64{}
	end := now.Unix() / 60 * 60
	for i := range stats.Minutes {
		stamp := end - int64(59-i)*60
		b := firewallMinute{At: stamp, ByAction: map[string]int64{}}
		for _, stored := range m.minutes {
			if stored.At != stamp {
				continue
			}
			b.Total = stored.Total
			b.Blocked = stored.Blocked
			for k, v := range stored.ByAction {
				b.ByAction[k] = v
				stats.ByAction[k] += v
			}
			for k, v := range stored.rules {
				rules[k] += v
			}
			for k, v := range stored.ips {
				ips[k] += v
			}
			for k, v := range stored.countries {
				countries[k] += v
			}
			break
		}
		stats.Minutes[i] = b
		stats.Total += b.Total
		stats.Blocked += b.Blocked
	}
	stats.TopRules = topFirewall(rules)
	stats.TopIPs = topFirewall(ips)
	stats.TopCountries = topFirewall(countries)
	return events, stats
}
