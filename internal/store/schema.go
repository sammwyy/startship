package store

// schema is the complete database layout. Startship has no released installs
// to carry forward, so tables are created in their final shape.
const schema = `
CREATE TABLE IF NOT EXISTS records(id TEXT PRIMARY KEY, kind TEXT NOT NULL, owner TEXT NOT NULL, parent TEXT NOT NULL, body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY, username TEXT UNIQUE NOT NULL, password TEXT NOT NULL, must_change_password INTEGER NOT NULL DEFAULT 0, display_name TEXT NOT NULL DEFAULT '', email TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires INTEGER NOT NULL, refresh_digest TEXT, logged_at INTEGER NOT NULL DEFAULT 0, last_activity_at INTEGER NOT NULL DEFAULT 0, ip TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '');
CREATE UNIQUE INDEX IF NOT EXISTS session_refresh ON sessions(refresh_digest);
CREATE INDEX IF NOT EXISTS sessions_user ON sessions(user_id,expires);
CREATE TABLE IF NOT EXISTS session_access(digest TEXT PRIMARY KEY, session_id TEXT NOT NULL, expires INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS session_access_session ON session_access(session_id);
CREATE INDEX IF NOT EXISTS session_access_expiry ON session_access(expires);
CREATE TRIGGER IF NOT EXISTS session_access_cleanup AFTER DELETE ON sessions BEGIN DELETE FROM session_access WHERE session_id=OLD.id; END;
CREATE TABLE IF NOT EXISTS access_tokens(id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL, digest TEXT NOT NULL UNIQUE, prefix TEXT NOT NULL, all_workspaces INTEGER NOT NULL DEFAULT 0, workspaces TEXT NOT NULL DEFAULT '[]', access TEXT NOT NULL DEFAULT 'operate', api INTEGER NOT NULL DEFAULT 1, mcp INTEGER NOT NULL DEFAULT 1, created INTEGER NOT NULL, expires INTEGER NOT NULL DEFAULT 0, last_used_at INTEGER NOT NULL DEFAULT 0, last_used_ip TEXT NOT NULL DEFAULT '', last_user_agent TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS access_tokens_user ON access_tokens(user_id,created DESC);
CREATE TRIGGER IF NOT EXISTS access_tokens_owner AFTER DELETE ON users BEGIN DELETE FROM access_tokens WHERE user_id=OLD.id; END;
CREATE TABLE IF NOT EXISTS roles(id TEXT PRIMARY KEY, name TEXT NOT NULL, color TEXT NOT NULL, icon TEXT NOT NULL, permissions TEXT NOT NULL, created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS role_assignments(id TEXT PRIMARY KEY, user_id TEXT NOT NULL, role_id TEXT NOT NULL, scope TEXT NOT NULL, scope_id TEXT NOT NULL, created TEXT NOT NULL, created_by TEXT NOT NULL, UNIQUE(user_id, role_id, scope, scope_id));
CREATE TABLE IF NOT EXISTS permission_overrides(scope TEXT NOT NULL, scope_id TEXT NOT NULL, subject_type TEXT NOT NULL, subject_id TEXT NOT NULL, allow TEXT NOT NULL, deny TEXT NOT NULL, updated TEXT NOT NULL, updated_by TEXT NOT NULL, PRIMARY KEY(scope, scope_id, subject_type, subject_id));
CREATE TABLE IF NOT EXISTS audit_log(id INTEGER PRIMARY KEY AUTOINCREMENT, at TEXT NOT NULL, actor_id TEXT NOT NULL, actor TEXT NOT NULL, action TEXT NOT NULL, target TEXT NOT NULL, scope TEXT NOT NULL, scope_id TEXT NOT NULL, detail TEXT NOT NULL, workspace_id TEXT NOT NULL DEFAULT '', session_id TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '', token_id TEXT NOT NULL DEFAULT '', token_name TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS audit_activity_actor ON audit_log(actor_id,id DESC);
CREATE INDEX IF NOT EXISTS audit_activity_workspace ON audit_log(workspace_id,id DESC);
CREATE INDEX IF NOT EXISTS audit_activity_scope ON audit_log(scope,scope_id,id DESC);
CREATE TABLE IF NOT EXISTS workspace_domains(id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, name TEXT NOT NULL UNIQUE, apex INTEGER NOT NULL, subdomains INTEGER NOT NULL, created TEXT NOT NULL, created_by TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS certificates(id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, body TEXT NOT NULL, private_key TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS acme_accounts(directory TEXT PRIMARY KEY, private_key TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS services(id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS volumes(id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS backups(id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS firewall_policies(workspace_id TEXT PRIMARY KEY, body TEXT NOT NULL, updated TEXT NOT NULL, updated_by TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS firewall_lists(id TEXT NOT NULL, workspace_id TEXT NOT NULL, body TEXT NOT NULL, PRIMARY KEY(workspace_id,id));
CREATE TABLE IF NOT EXISTS network_rules(id TEXT PRIMARY KEY, body TEXT NOT NULL);
`
