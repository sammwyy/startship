package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/sammwyy/startship/internal/firewall"
)

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
	banned, changed := false, false
	scope := activityScope{kind: scopeWorkspace, id: workspace, workspace: workspace}
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
			changed = true
			l.Entries = live
			if err = putFirewallList(tx, workspace, *l); err != nil {
				return p, nil, err
			}
			if err = writeAudit(tx, sessionIdentity{}, "", "firewall.list.expire", l.ID, "firewall", scope, nil); err != nil {
				return p, nil, err
			}
		}
	}
	if !banned {
		changed = true
		l := firewall.List{ID: "banned", Name: "Banned", Kind: "block", Entries: []firewall.Entry{}}
		lists = append(lists, l)
		if err = putFirewallList(tx, workspace, l); err != nil {
			return p, nil, err
		}
		if err = writeAudit(tx, sessionIdentity{}, "", "firewall.list.create", "banned", "firewall", scope, l); err != nil {
			return p, nil, err
		}
	}
	if err = tx.Commit(); err == nil && changed {
		a.publishActivity("", scope)
	}
	return p, lists, err
}
func (a *Server) firewallConfig(workspace string) (firewall.Policy, []firewall.List, error) {
	a.routerRefresh.Lock()
	defer a.routerRefresh.Unlock()
	return a.loadFirewall(workspace)
}
func (a *Server) saveFirewallPolicy(ctx context.Context, workspace, uid, action, target string, p firewall.Policy) error {
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
	scope := activityScope{kind: scopeWorkspace, id: workspace, workspace: workspace}
	if err = writeAudit(tx, requestSession(ctx), uid, action, target, "firewall", scope, p); err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		a.publishActivity(uid, scope)
	}
	return err
}
func (a *Server) saveFirewallList(ctx context.Context, workspace, uid, action string, list firewall.List) error {
	tx, err := a.store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = putFirewallList(tx, workspace, list); err != nil {
		return err
	}
	scope := activityScope{kind: scopeWorkspace, id: workspace, workspace: workspace}
	if err = writeAudit(tx, requestSession(ctx), uid, action, list.ID, "firewall", scope, list); err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		a.publishActivity(uid, scope)
	}
	return err
}
func (a *Server) deleteFirewallList(ctx context.Context, workspace, uid, id string) error {
	tx, err := a.store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM firewall_lists WHERE workspace_id=? AND id=?", workspace, id); err != nil {
		return err
	}
	scope := activityScope{kind: scopeWorkspace, id: workspace, workspace: workspace}
	if err = writeAudit(tx, requestSession(ctx), uid, "firewall.list.delete", id, "firewall", scope, nil); err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		a.publishActivity(uid, scope)
	}
	return err
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
