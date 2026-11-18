package server

import (
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	tokenPrefix    = "sst_"
	tokenTouch     = time.Minute
	tokenMaxExpiry = 3650
)

// tokenOperatePermissions is what an "operate" token keeps of its owner's permissions:
// reading, deploying and configuring projects, never members, roles or workspace settings.
var tokenOperatePermissions = []string{"project.view", "logs.view", "traffic.view", "activity.view", "firewall.view",
	"projects.create", "deploy", "project.settings", "project.environment", "project.source", "domains.manage"}

// tokenScope narrows its owner's permissions to the workspaces and access level chosen at creation.
type tokenScope struct {
	id, name   string
	all, admin bool
	api, mcp   bool
	workspaces map[string]bool
}

func (t *tokenScope) reaches(workspaceID string) bool {
	return workspaceID != "" && (t.all || t.workspaces[workspaceID])
}

func (t *tokenScope) allows(perm, workspaceID string) bool {
	if permissionByKey[perm].Scope == scopePlatform {
		return workspaceID == "" && perm == "workspaces.create" && t.all && t.admin
	}
	return t.reaches(workspaceID) && (t.admin || slices.Contains(tokenOperatePermissions, perm))
}

type accessToken struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Prefix        string   `json:"prefix"`
	AllWorkspaces bool     `json:"all_workspaces"`
	Workspaces    []string `json:"workspaces"`
	Access        string   `json:"access"`
	API           bool     `json:"api"`
	MCP           bool     `json:"mcp"`
	CreatedAt     string   `json:"created_at"`
	ExpiresAt     string   `json:"expires_at,omitempty"`
	LastUsedAt    string   `json:"last_used_at,omitempty"`
	LastUsedIP    string   `json:"last_used_ip,omitempty"`
	LastUserAgent string   `json:"last_user_agent,omitempty"`
}

func peerIP(r *http.Request) string {
	// The peer address is trusted; user-controlled forwarding headers are not.
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

func bearerToken(r *http.Request) string {
	scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(value)
}

// authenticateToken resolves a personal access token and records where and when it was last used.
func (a *Server) authenticateToken(r *http.Request, secret string) (s sessionIdentity) {
	if !strings.HasPrefix(secret, tokenPrefix) {
		return s
	}
	now := time.Now()
	t := &tokenScope{}
	var list string
	var expires, last int64
	var lastIP, level string
	err := a.store.DB.QueryRow(`SELECT t.id,t.user_id,t.name,t.all_workspaces,t.workspaces,t.access,t.api,t.mcp,t.expires,t.last_used_at,t.last_used_ip
		FROM access_tokens t JOIN users u ON u.id=t.user_id WHERE t.digest=? AND (t.expires=0 OR t.expires>?)`, digestToken(secret), now.Unix()).
		Scan(&t.id, &s.userID, &t.name, &t.all, &list, &level, &t.api, &t.mcp, &expires, &last, &lastIP)
	if err != nil {
		return sessionIdentity{}
	}
	t.admin = level == "admin"
	var ids []string
	json.Unmarshal([]byte(list), &ids)
	t.workspaces = map[string]bool{}
	for _, id := range ids {
		t.workspaces[id] = true
	}
	s.token = t
	s.expires = expires
	if expires == 0 {
		s.expires = now.Add(24 * time.Hour).Unix()
	}
	ip := peerIP(r)
	if now.Unix()-last >= int64(tokenTouch/time.Second) || ip != lastIP {
		if res, err := a.store.DB.Exec("UPDATE access_tokens SET last_used_at=?,last_used_ip=?,last_user_agent=? WHERE id=?",
			now.Unix(), ip, trimMetadata(r.UserAgent(), 512), t.id); err == nil {
			if n, _ := res.RowsAffected(); n == 1 {
				a.publishTokens(s.userID)
			}
		}
	}
	return s
}

func (a *Server) publishTokens(uid string) {
	a.live.publish(liveEvent{changeKey: "account_tokens:" + uid})
}

func unixStamp(v int64) string {
	if v == 0 {
		return ""
	}
	return time.Unix(v, 0).UTC().Format(time.RFC3339)
}

func (a *Server) accountTokens(uid string) ([]accessToken, error) {
	rows, err := a.store.DB.Query(`SELECT id,name,prefix,all_workspaces,workspaces,access,api,mcp,created,expires,last_used_at,last_used_ip,last_user_agent
		FROM access_tokens WHERE user_id=? AND (expires=0 OR expires>?) ORDER BY created DESC,rowid DESC`, uid, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []accessToken{}
	for rows.Next() {
		var v accessToken
		var list string
		var created, expires, used int64
		if err := rows.Scan(&v.ID, &v.Name, &v.Prefix, &v.AllWorkspaces, &list, &v.Access, &v.API, &v.MCP, &created, &expires, &used, &v.LastUsedIP, &v.LastUserAgent); err != nil {
			return nil, err
		}
		v.Workspaces = []string{}
		json.Unmarshal([]byte(list), &v.Workspaces)
		v.CreatedAt, v.ExpiresAt, v.LastUsedAt = unixStamp(created), unixStamp(expires), unixStamp(used)
		out = append(out, v)
	}
	return out, rows.Err()
}

type tokenRequest struct {
	Name          string   `json:"name"`
	AllWorkspaces bool     `json:"all_workspaces"`
	Workspaces    []string `json:"workspaces"`
	Access        string   `json:"access"`
	API           *bool    `json:"api"`
	MCP           *bool    `json:"mcp"`
	ExpiresInDays int      `json:"expires_in_days"`
}

func (a *Server) tokensAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) {
	switch {
	case len(p) == 3 && r.Method == "GET":
		items, err := a.accountTokens(uid)
		if err != nil {
			fail(w, 500, "tokens unavailable")
			return
		}
		reply(w, 200, items)
	case len(p) == 3 && r.Method == "POST":
		a.createToken(w, r, uid, x)
	case len(p) == 4 && r.Method == "DELETE":
		var name string
		err := a.store.DB.QueryRow("SELECT name FROM access_tokens WHERE id=? AND user_id=?", p[3], uid).Scan(&name)
		if err == sql.ErrNoRows {
			fail(w, 404, "token not found")
			return
		}
		if err != nil {
			fail(w, 500, "token revoke failed")
			return
		}
		if _, err = a.store.DB.Exec("DELETE FROM access_tokens WHERE id=? AND user_id=?", p[3], uid); err != nil {
			fail(w, 500, "token revoke failed")
			return
		}
		a.publishTokens(uid)
		a.audit(r.Context(), uid, "token.revoke", name, scopeAccount, uid, map[string]string{"token_id": p[3]})
		w.WriteHeader(204)
	default:
		fail(w, 405, "method not allowed")
	}
}

func (a *Server) createToken(w http.ResponseWriter, r *http.Request, uid string, x *access) {
	var in tokenRequest
	if decode(w, r, &in) != nil {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 60 {
		fail(w, 400, "name must be 1-60 characters")
		return
	}
	if in.Access == "" {
		in.Access = "operate"
	}
	if in.Access != "operate" && in.Access != "admin" {
		fail(w, 400, "access must be operate or admin")
		return
	}
	api, mcp := in.API == nil || *in.API, in.MCP == nil || *in.MCP
	if !api && !mcp {
		fail(w, 400, "enable the token for the API, MCP or both")
		return
	}
	if in.ExpiresInDays < 0 || in.ExpiresInDays > tokenMaxExpiry {
		fail(w, 400, "expiry must be between 0 (never) and 3650 days")
		return
	}
	workspaces := []string{}
	if !in.AllWorkspaces {
		for _, id := range in.Workspaces {
			if slices.Contains(workspaces, id) {
				continue
			}
			v, err := a.store.Get(id)
			if err != nil || v.Kind != "workspaces" || !x.memberOf(id) {
				fail(w, 400, "unknown workspace "+id)
				return
			}
			workspaces = append(workspaces, id)
		}
		if len(workspaces) == 0 {
			fail(w, 400, "choose at least one workspace or all workspaces")
			return
		}
	}
	list, _ := json.Marshal(workspaces)
	now := time.Now()
	var expires int64
	if in.ExpiresInDays > 0 {
		expires = now.AddDate(0, 0, in.ExpiresInDays).Unix()
	}
	id, secret := randomID(), tokenPrefix+randomID()
	prefix := secret[:len(tokenPrefix)+6]
	if _, err := a.store.DB.Exec(`INSERT INTO access_tokens(id,user_id,name,digest,prefix,all_workspaces,workspaces,access,api,mcp,created,expires)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, uid, in.Name, digestToken(secret), prefix, in.AllWorkspaces, string(list), in.Access, api, mcp, now.Unix(), expires); err != nil {
		fail(w, 500, "token creation failed")
		return
	}
	a.publishTokens(uid)
	a.audit(r.Context(), uid, "token.create", in.Name, scopeAccount, uid, map[string]any{
		"token_id": id, "all_workspaces": in.AllWorkspaces, "workspaces": workspaces, "access": in.Access, "api": api, "mcp": mcp})
	reply(w, 201, struct {
		accessToken
		Token string `json:"token"`
	}{accessToken{ID: id, Name: in.Name, Prefix: prefix, AllWorkspaces: in.AllWorkspaces, Workspaces: workspaces, Access: in.Access,
		API: api, MCP: mcp, CreatedAt: unixStamp(now.Unix()), ExpiresAt: unixStamp(expires)}, secret})
}

type tokenInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type mcpCallKey struct{}

// tokenDenied keeps tokens to the surfaces they were enabled for. Account credentials, sign-in
// sessions and the dashboard's live connection stay with a signed-in session.
func tokenDenied(r *http.Request, p []string, t *tokenScope) string {
	viaMCP := r.Context().Value(mcpCallKey{}) != nil
	switch {
	case p[1] == "mcp":
		if !t.mcp {
			return "this token is not enabled for MCP"
		}
	case !t.api && !viaMCP:
		return "this token is only enabled for MCP"
	}
	switch {
	case p[1] == "live":
		return "live updates need a signed-in session"
	case p[1] == "me" && (len(p) == 2 && r.Method != "GET" || len(p) > 2 && (p[2] == "sessions" || p[2] == "tokens")):
		return "managing your account needs a signed-in session"
	}
	return ""
}
