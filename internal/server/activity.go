package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

const scopeAccount = "account"

func migrateActivity(s *Store) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var migrated int
	if err = tx.QueryRow("SELECT count(*) FROM settings WHERE key='audit_activity_v1'").Scan(&migrated); err != nil || migrated != 0 {
		return err
	}
	_, err = tx.Exec(`
		ALTER TABLE audit_log ADD COLUMN workspace_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE audit_log ADD COLUMN session_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE audit_log ADD COLUMN source TEXT NOT NULL DEFAULT '';
		UPDATE audit_log SET workspace_id=scope_id WHERE scope='workspace';
		UPDATE audit_log SET workspace_id=coalesce(
			(SELECT coalesce(nullif(json_extract(body,'$.workspace_id'),''),parent)
			 FROM records WHERE kind='projects' AND id=audit_log.scope_id),'') WHERE scope='project';
		CREATE INDEX audit_activity_actor ON audit_log(actor_id,id DESC);
		CREATE INDEX audit_activity_workspace ON audit_log(workspace_id,id DESC);
		CREATE INDEX audit_activity_scope ON audit_log(scope,scope_id,id DESC);
		INSERT INTO settings VALUES('audit_activity_v1','1');
	`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type activityScope struct {
	kind, id, workspace string
}

func (a *Server) auditScope(scope, id string) activityScope {
	v := activityScope{kind: scope, id: id}
	switch scope {
	case scopeWorkspace:
		v.workspace = id
	case scopeProject:
		p, _ := a.store.Get(id)
		v.workspace = projectWorkspace(p)
	}
	return v
}

type auditDB interface {
	QueryRow(string, ...any) *sql.Row
	Exec(string, ...any) (sql.Result, error)
}

func writeAudit(db auditDB, s sessionIdentity, uid, action, target, fallback string, scope activityScope, detail any) error {
	name := fallback
	if uid != "" {
		if err := db.QueryRow("SELECT username FROM users WHERE id=?", uid).Scan(&name); err != nil {
			return err
		}
	}
	source, tokenID, tokenName := "", "", ""
	switch {
	case s.token != nil:
		source, tokenID, tokenName = "token", s.token.id, s.token.name
	case s.sessionID != "":
		source = "session"
	case s.local:
		source = "local_socket"
	case uid == "":
		source = "system"
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO audit_log(at,actor_id,actor,action,target,scope,scope_id,detail,workspace_id,session_id,source,token_id,token_name)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, nowStamp(), uid, name, action, target, scope.kind, scope.id, string(b), scope.workspace, s.sessionID, source, tokenID, tokenName)
	return err
}

func (a *Server) audit(ctx context.Context, uid, action, target, scope, id string, detail any) {
	a.recordAudit(ctx, uid, action, target, a.auditScope(scope, id), detail)
}

func (a *Server) recordAudit(ctx context.Context, uid, action, target string, scope activityScope, detail any) {
	if err := writeAudit(a.store.DB, requestSession(ctx), uid, action, target, "system", scope, detail); err == nil {
		a.publishActivity(uid, scope)
	}
}

func (a *Server) publishActivity(uid string, scope activityScope) {
	if uid != "" {
		a.live.publish(liveEvent{changeKey: "account_activity:" + uid})
	}
	if scope.workspace != "" {
		a.live.publish(liveEvent{changeKey: "workspace_activity:" + scope.workspace})
	}
	if scope.kind == scopeProject {
		a.live.publish(liveEvent{changeKey: "project_activity:" + scope.id})
	}
	a.live.publish(liveEvent{changeKey: "audit_activity"})
}

func (a *Server) publishAccount(uid string) {
	a.live.publish(liveEvent{changeKey: "account_sessions:" + uid})
}

func (a *Server) auditRecord(ctx context.Context, uid, verb string, r Record, workspace, project string) {
	kind := map[string]string{"workspaces": "workspace", "projects": "project", "apps": "container", "routes": "route"}[r.Kind]
	target := r.Name
	if r.Kind == "routes" {
		target = r.Host + r.PathPrefix
	}
	scope := activityScope{kind: scopeProject, id: project, workspace: workspace}
	if r.Kind == "workspaces" {
		scope = activityScope{kind: scopeWorkspace, id: r.ID, workspace: r.ID}
	}
	a.recordAudit(ctx, uid, kind+"."+verb, target, scope, map[string]string{"id": r.ID})
}

type activityEntry struct {
	ID        int64           `json:"id"`
	At        string          `json:"at"`
	ActorID   string          `json:"actor_id"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	Scope     string          `json:"scope"`
	ScopeID   string          `json:"scope_id"`
	SessionID string          `json:"session_id,omitempty"`
	Source    string          `json:"source,omitempty"`
	TokenID   string          `json:"token_id,omitempty"`
	Token     string          `json:"token,omitempty"`
	Detail    json.RawMessage `json:"detail,omitempty"`
}

func (a *Server) activity(scope, id string, platform bool) ([]activityEntry, error) {
	query := "SELECT id,at,actor_id,actor,action,target,scope,scope_id,session_id,source,token_id,token_name,detail FROM audit_log"
	var args []any
	switch scope {
	case scopeAccount:
		query += " WHERE actor_id=?"
		args = append(args, id)
	case scopeWorkspace:
		query += " WHERE workspace_id=?"
		args = append(args, id)
	case scopeProject:
		query += " WHERE scope='project' AND scope_id=?"
		args = append(args, id)
	}
	limit := "100"
	if platform {
		limit = "500"
	}
	rows, err := a.store.DB.Query(query+" ORDER BY id DESC LIMIT "+limit, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []activityEntry{}
	for rows.Next() {
		var v activityEntry
		var detail string
		if err := rows.Scan(&v.ID, &v.At, &v.ActorID, &v.Actor, &v.Action, &v.Target, &v.Scope, &v.ScopeID, &v.SessionID, &v.Source, &v.TokenID, &v.Token, &detail); err != nil {
			return nil, err
		}
		if scope != scopeAccount {
			v.SessionID, v.TokenID = "", ""
		}
		if platform && json.Valid([]byte(detail)) {
			v.Detail = json.RawMessage(detail)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func activityAccess(x *access, r Record) bool {
	return x.visible(r) && x.can(r, "activity.view")
}

func (a *Server) activityAPI(w http.ResponseWriter, r *http.Request, p []string, x *access) bool {
	var scope, id string
	switch {
	case len(p) == 3 && p[1] == "me" && p[2] == "activity":
		scope, id = scopeAccount, x.uid
	case len(p) == 4 && p[3] == "activity" && (p[1] == "workspaces" || p[1] == "projects"):
		v, err := a.store.Get(p[2])
		if err != nil || v.Kind != p[1] || !x.visible(v) {
			fail(w, 404, "not found")
			return true
		}
		if !activityAccess(x, v) {
			forbidden(w)
			return true
		}
		scope = scopeWorkspace
		if v.Kind == "projects" {
			scope = scopeProject
		}
		id = v.ID
	default:
		return false
	}
	if r.Method != "GET" {
		fail(w, 405, "method not allowed")
		return true
	}
	a.replyActivity(w, scope, id, false)
	return true
}

func (a *Server) replyActivity(w http.ResponseWriter, scope, id string, platform bool) {
	entries, err := a.activity(scope, id, platform)
	if err != nil {
		fail(w, 500, "activity log unavailable")
		return
	}
	reply(w, 200, entries)
}

func (a *Server) activityLive(uid string, s *liveSub) (any, error) {
	switch s.Topic {
	case "account_sessions":
		return a.accountSessions(uid)
	case "account_tokens":
		return a.accountTokens(uid)
	case "account_activity":
		return a.activity(scopeAccount, uid, false)
	case "workspace_activity":
		return a.activity(scopeWorkspace, s.WorkspaceID, false)
	case "project_activity":
		return a.activity(scopeProject, s.ProjectID, false)
	case "audit_activity":
		return a.activity(scopePlatform, "", true)
	}
	return nil, errors.New("invalid activity topic")
}

func (s *liveSub) activityKey(uid string) string {
	switch s.Topic {
	case "account_sessions", "account_activity", "account_tokens":
		return s.Topic + ":" + uid
	case "workspace_activity":
		return s.Topic + ":" + s.WorkspaceID
	case "project_activity":
		return s.Topic + ":" + s.ProjectID
	case "audit_activity":
		return s.Topic
	default:
		return ""
	}
}
