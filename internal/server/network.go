package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sammwyy/startship/internal/config"
	"github.com/sammwyy/startship/internal/netfw"
)

const networkDisabled = "network firewall is disabled in the daemon config (network_firewall = true)"

type networkRuntime struct {
	mu      sync.Mutex
	enabled bool
	manager *netfw.Manager
	err     error
}

func migrateNetwork(s *Store) error {
	_, err := s.DB.Exec("CREATE TABLE IF NOT EXISTS network_rules(id TEXT PRIMARY KEY, body TEXT NOT NULL)")
	return err
}

func networkAudit(tx *sql.Tx, uid, action, target string, detail any) error {
	actor := "network firewall"
	if uid != "" {
		if err := tx.QueryRow("SELECT username FROM users WHERE id=?", uid).Scan(&actor); err != nil {
			return err
		}
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO audit_log(at,actor_id,actor,action,target,scope,scope_id,detail) VALUES(?,?,?,?,?,?,?,?)", nowStamp(), uid, actor, action, target, scopePlatform, "", string(b))
	return err
}

func (a *Server) networkRules() ([]netfw.Rule, error) {
	rows, err := a.store.DB.Query("SELECT body FROM network_rules ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := []netfw.Rule{}
	for rows.Next() {
		var body string
		var rule netfw.Rule
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(body), &rule); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (a *Server) networkManager() *netfw.Manager {
	if a.network.manager == nil {
		a.network.manager = &netfw.Manager{Executor: netfw.OSExecutor{}}
	}
	return a.network.manager
}

// Caller holds network.mu across storage and kernel publication.
func (a *Server) publishNetwork(old, next []netfw.Rule, uid, action, target string) error {
	tx, err := a.store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM network_rules"); err != nil {
		return err
	}
	for _, r := range next {
		b, e := json.Marshal(r)
		if e != nil {
			return e
		}
		if _, err = tx.Exec("INSERT INTO network_rules(id,body) VALUES(?,?)", r.ID, string(b)); err != nil {
			return err
		}
	}
	if err = networkAudit(tx, uid, action, target, next); err != nil {
		return err
	}
	manager := a.networkManager()
	if err = manager.Reconcile(next, time.Now()); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		rollback := manager.Reconcile(old, time.Now())
		if rollback != nil {
			err = errors.Join(err, fmt.Errorf("restore previous network rules: %w", rollback))
		}
	}
	a.network.err = err
	return err
}

func (a *Server) expireNetwork(rules []netfw.Rule) ([]netfw.Rule, error) {
	live := []netfw.Rule{}
	for _, r := range rules {
		if r.ExpiresAt == nil || time.Now().Before(*r.ExpiresAt) {
			live = append(live, r)
		}
	}
	if len(live) == len(rules) {
		return rules, nil
	}
	if err := a.publishNetwork(rules, live, "", "network.rule.expire", ""); err != nil {
		return nil, err
	}
	return live, nil
}

func (a *Server) initNetwork(settings config.Daemon) {
	n := &a.network
	n.enabled = settings.NetworkFirewall
	ports := []string{}
	for _, entry := range append(slices.Clone(settings.HTTPListen), settings.HTTPSListen...) {
		_, port, _ := net.SplitHostPort(entry)
		if port != "0" {
			ports = append(ports, port)
		}
	}
	n.manager = &netfw.Manager{Executor: netfw.OSExecutor{}, Ports: ports, Country: func(code string) ([]string, error) { return netfw.ExpandCountry(settings.GeoIPDatabase, code) }}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.enabled {
		n.err = n.manager.Teardown()
	} else {
		rules, err := a.networkRules()
		if err == nil {
			rules, err = a.expireNetwork(rules)
		}
		if err == nil {
			err = n.manager.Reconcile(rules, time.Now())
		}
		n.err = err
	}
	if n.err != nil {
		log.Printf("network firewall: %v", n.err)
	}
}

func (a *Server) watchNetwork(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			func() {
				a.network.mu.Lock()
				defer a.network.mu.Unlock()
				if !a.network.enabled {
					return
				}
				rules, err := a.networkRules()
				if err == nil {
					rules, err = a.expireNetwork(rules)
				}
				if err == nil {
					err = a.networkManager().Reconcile(rules, time.Now())
				}
				a.network.err = err
				if err != nil {
					log.Printf("network firewall: %v", err)
				}
			}()
		}
	}
}

func (a *Server) networkAPI(w http.ResponseWriter, r *http.Request, path []string, uid string, x *access) bool {
	if len(path) < 3 || path[1] != "system" || path[2] != "network-firewall" {
		return false
	}
	if !x.platform("network.firewall") {
		forbidden(w)
		return true
	}
	n := &a.network
	n.mu.Lock()
	defer n.mu.Unlock()
	manager := a.networkManager()
	if r.Method == "GET" && len(path) == 3 {
		availableErr := manager.Available()
		rules := []netfw.Rule{}
		err := n.err
		if n.enabled {
			rules, err = a.networkRules()
			if err == nil {
				rules, err = a.expireNetwork(rules)
			}
			if err == nil {
				err = n.err
			}
		}
		if availableErr != nil {
			err = availableErr
		}
		result := map[string]any{"enabled": n.enabled, "available": availableErr == nil, "rules": rules}
		if rules == nil {
			result["rules"] = []netfw.Rule{}
		}
		if err != nil {
			result["error"] = err.Error()
		}
		reply(w, 200, result)
		return true
	}
	if !n.enabled {
		fail(w, 409, networkDisabled)
		return true
	}
	if len(path) < 4 || path[3] != "rules" {
		fail(w, 404, "not found")
		return true
	}
	rules, err := a.networkRules()
	if err != nil {
		fail(w, 500, err.Error())
		return true
	}
	next := slices.Clone(rules)
	action, target := "", ""
	var result any
	if r.Method == "POST" && len(path) == 4 {
		var input struct {
			netfw.Rule
			TTLSeconds *int `json:"ttl_seconds"`
		}
		if err = decode(w, r, &input); err != nil {
			fail(w, 400, err.Error())
			return true
		}
		rule := input.Rule
		if err = netfw.Normalize(&rule); err != nil {
			fail(w, 400, err.Error())
			return true
		}
		if input.TTLSeconds != nil {
			if *input.TTLSeconds < 0 || *input.TTLSeconds > 365*24*3600 {
				fail(w, 400, "ttl_seconds: expected 0..31536000")
				return true
			}
			if rule.ExpiresAt != nil {
				fail(w, 400, "use ttl_seconds or expires_at, not both")
				return true
			}
			rule.ExpiresAt = firewallExpiry(*input.TTLSeconds)
		}
		if rule.ExpiresAt != nil && !rule.ExpiresAt.After(time.Now()) {
			fail(w, 400, "expires_at: must be in the future")
			return true
		}
		if rule.Kind == "country" && manager.Country == nil {
			fail(w, 400, "country rules require geoip_database")
			return true
		}
		if rule.Kind == "country" {
			if _, err = manager.Country(rule.Value); err != nil {
				fail(w, 400, err.Error())
				return true
			}
		}
		rule.ID = randomID()
		next = append(next, rule)
		action, target = "network.rule.create", rule.ID
		result = rule
	} else if r.Method == "DELETE" && len(path) == 5 {
		index := slices.IndexFunc(rules, func(rule netfw.Rule) bool { return rule.ID == path[4] })
		if index < 0 {
			fail(w, 404, "network rule not found")
			return true
		}
		next = slices.Delete(next, index, index+1)
		action, target = "network.rule.delete", path[4]
		result = map[string]bool{"ok": true}
	} else {
		w.Header().Set("Allow", "POST, DELETE")
		fail(w, 405, "method not allowed")
		return true
	}
	// Never republish an expired entry during an unrelated mutation.
	next = slices.DeleteFunc(next, func(rule netfw.Rule) bool { return rule.ExpiresAt != nil && !time.Now().Before(*rule.ExpiresAt) })
	if err = a.publishNetwork(rules, next, uid, action, target); err != nil {
		fail(w, 500, "network firewall: "+strings.TrimSpace(err.Error()))
		return true
	}
	reply(w, 200, result)
	return true
}
