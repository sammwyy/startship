// Package firewall compiles workspace firewall policies independently of HTTP routing.
package firewall

import (
	"net/http"
	"time"
)

type Action string

const (
	Block     Action = "block"
	Allow     Action = "allow"
	Log       Action = "log"
	RateLimit Action = "rate_limit"
	Ban       Action = "ban"
)

type Policy struct {
	Mode      string                 `json:"mode"`
	Managed   map[string]ManagedRule `json:"managed"`
	Limits    Limits                 `json:"limits"`
	Rules     []Rule                 `json:"rules"`
	Updated   string                 `json:"updated"`
	UpdatedBy string                 `json:"updated_by"`
}
type ManagedRule struct {
	Enabled bool   `json:"enabled"`
	Action  Action `json:"action"`
}
type Limits struct {
	MaxBodyBytes    int64          `json:"max_body_bytes"`
	MaxURLBytes     int            `json:"max_url_bytes"`
	MaxHeaderBytes  int            `json:"max_header_bytes"`
	Rate            *WorkspaceRate `json:"rate"`
	ConcurrentPerIP int            `json:"concurrent_per_ip"`
}
type WorkspaceRate struct {
	Requests      int    `json:"requests"`
	WindowSeconds int    `json:"window_seconds"`
	Action        Action `json:"action"`
	BanSeconds    int    `json:"ban_seconds"`
}
type Rate struct {
	Requests      int    `json:"requests"`
	WindowSeconds int    `json:"window_seconds"`
	By            string `json:"by"`
}
type Rule struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Enabled    bool        `json:"enabled"`
	Match      string      `json:"match"`
	Conditions []Condition `json:"conditions"`
	Action     Action      `json:"action"`
	Rate       *Rate       `json:"rate,omitempty"`
	BanSeconds int         `json:"ban_seconds,omitempty"`
}
type Condition struct {
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
	Negate bool   `json:"negate,omitempty"`
}
type List struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Kind    string  `json:"kind"`
	Entries []Entry `json:"entries"`
}
type Entry struct {
	Value     string     `json:"value"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Note      string     `json:"note"`
	CreatedBy string     `json:"created_by"`
}
type Event struct {
	Time        time.Time `json:"time"`
	WorkspaceID string    `json:"workspace_id"`
	ProjectID   string    `json:"project_id"`
	RouteHost   string    `json:"route_host"`
	IP          string    `json:"ip"`
	Country     string    `json:"country"`
	Method      string    `json:"method"`
	Host        string    `json:"host"`
	Path        string    `json:"path"`
	UA          string    `json:"ua"`
	UAClass     string    `json:"ua_class"`
	RuleID      string    `json:"rule_id"`
	RuleName    string    `json:"rule_name"`
	Action      Action    `json:"action"`
	Status      int       `json:"status"`
	Monitored   bool      `json:"monitored"`
}
type Request struct {
	IP, Country, Method, Host, Path, RawQuery, UA, Scheme string
	Headers                                               http.Header
	BodySample                                            []byte
	ContentLength                                         int64
	WorkspaceID, ProjectID, RouteHost                     string
}
type Decision struct {
	Action                 Action
	RuleID, RuleName       string
	StatusCode, BanSeconds int
	Monitored              bool
	Events                 []Event
}

func DefaultPolicy() Policy {
	p := Policy{Mode: "off", Managed: map[string]ManagedRule{}, Rules: []Rule{}}
	for _, m := range Catalog().ManagedRules {
		p.Managed[m.ID] = ManagedRule{m.DefaultEnabled, m.DefaultAction}
	}
	return p
}
