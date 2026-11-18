package server

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/sammwyy/startship/internal/firewall"
)

func migrateFirewall(s *Store) error {
	_, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS firewall_policies(workspace_id TEXT PRIMARY KEY, body TEXT NOT NULL, updated TEXT NOT NULL, updated_by TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS firewall_lists(id TEXT NOT NULL, workspace_id TEXT NOT NULL, body TEXT NOT NULL, PRIMARY KEY(workspace_id,id));`)
	return err
}
func firewallAudit(tx *sql.Tx, uid, action, target, workspace string, detail any) error {
	actor := "firewall"
	if uid != "" {
		if err := tx.QueryRow("SELECT username FROM users WHERE id=?", uid).Scan(&actor); err != nil {
			return err
		}
	}
	body, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO audit_log(at,actor_id,actor,action,target,scope,scope_id,detail) VALUES(?,?,?,?,?,?,?,?)", nowStamp(), uid, actor, action, target, scopeWorkspace, workspace, string(body))
	return err
}
func putFirewallList(tx *sql.Tx, workspace string, list firewall.List) error {
	body, err := json.Marshal(list)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO firewall_lists(id,workspace_id,body) VALUES(?,?,?) ON CONFLICT(workspace_id,id) DO UPDATE SET body=excluded.body", list.ID, workspace, string(body))
	return err
}

// loadFirewall runs under routerRefresh so reads, lazy expiry and mutations serialize.
func (a *Server) loadFirewall(workspace string) (firewall.Policy, []firewall.List, error) {
	p := firewall.DefaultPolicy()
	lists := []firewall.List{}
	tx, err := a.store.DB.Begin()
	if err != nil {
		return p, nil, err
	}
	defer tx.Rollback()
	var body string
	err = tx.QueryRow("SELECT body FROM firewall_policies WHERE workspace_id=?", workspace).Scan(&body)
	if err == nil {
		var stored firewall.Policy
		err = json.Unmarshal([]byte(body), &stored)
		p = stored
	} else if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
		return p, nil, err
	}
	rows, err := tx.Query("SELECT body FROM firewall_lists WHERE workspace_id=? ORDER BY id", workspace)
	if err != nil {
		return p, nil, err
	}
	for rows.Next() {
		var b string
		var l firewall.List
		if err = rows.Scan(&b); err == nil {
			err = json.Unmarshal([]byte(b), &l)
		}
		if err != nil {
			rows.Close()
			return p, nil, err
		}
		lists = append(lists, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return p, nil, err
	}
	banned := false
	now := time.Now()
	for i := range lists {
		l := &lists[i]
		if l.ID == "banned" {
			banned = true
		}
		live := make([]firewall.Entry, 0, len(l.Entries))
		for _, entry := range l.Entries {
			if entry.ExpiresAt == nil || now.Before(*entry.ExpiresAt) {
				live = append(live, entry)
			}
		}
		if len(live) != len(l.Entries) {
			l.Entries = live
			if err = putFirewallList(tx, workspace, *l); err != nil {
				return p, nil, err
			}
			if err = firewallAudit(tx, "", "firewall.list.expire", l.ID, workspace, nil); err != nil {
				return p, nil, err
			}
		}
	}
	if !banned {
		l := firewall.List{ID: "banned", Name: "Banned", Kind: "block", Entries: []firewall.Entry{}}
		lists = append(lists, l)
		if err = putFirewallList(tx, workspace, l); err != nil {
			return p, nil, err
		}
		if err = firewallAudit(tx, "", "firewall.list.create", "banned", workspace, l); err != nil {
			return p, nil, err
		}
	}
	return p, lists, tx.Commit()
}
func (a *Server) firewallConfig(workspace string) (firewall.Policy, []firewall.List, error) {
	a.routerRefresh.Lock()
	defer a.routerRefresh.Unlock()
	return a.loadFirewall(workspace)
}
func (a *Server) saveFirewallPolicy(workspace, uid, action, target string, p firewall.Policy) error {
	if err := firewall.Validate(p); err != nil {
		return err
	}
	p.Updated, p.UpdatedBy = nowStamp(), uid
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tx, err := a.store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO firewall_policies(workspace_id,body,updated,updated_by) VALUES(?,?,?,?) ON CONFLICT(workspace_id) DO UPDATE SET body=excluded.body,updated=excluded.updated,updated_by=excluded.updated_by", workspace, string(body), p.Updated, p.UpdatedBy); err != nil {
		return err
	}
	if err = firewallAudit(tx, uid, action, target, workspace, p); err != nil {
		return err
	}
	return tx.Commit()
}
func (a *Server) saveFirewallList(workspace, uid, action string, list firewall.List) error {
	tx, err := a.store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = putFirewallList(tx, workspace, list); err != nil {
		return err
	}
	if err = firewallAudit(tx, uid, action, list.ID, workspace, list); err != nil {
		return err
	}
	return tx.Commit()
}
func (a *Server) deleteFirewallList(workspace, uid, id string) error {
	tx, err := a.store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM firewall_lists WHERE workspace_id=? AND id=?", workspace, id); err != nil {
		return err
	}
	if err = firewallAudit(tx, uid, "firewall.list.delete", id, workspace, nil); err != nil {
		return err
	}
	return tx.Commit()
}
func findFirewallList(lists []firewall.List, id string) *firewall.List {
	for i := range lists {
		if lists[i].ID == id {
			return &lists[i]
		}
	}
	return nil
}
func firewallExpiry(seconds int) *time.Time {
	if seconds == 0 {
		return nil
	}
	t := time.Now().UTC().Add(time.Duration(seconds) * time.Second)
	return &t
}
func upsertFirewallEntry(list *firewall.List, entry firewall.Entry) {
	for i, e := range list.Entries {
		if e.Value == entry.Value {
			list.Entries[i] = entry
			return
		}
	}
	list.Entries = append(list.Entries, entry)
}
