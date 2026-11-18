package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	accessCookie  = "startship_session"
	refreshCookie = "startship_refresh"
	accessTTL     = 5 * time.Minute
	sessionTTL    = 7 * 24 * time.Hour
)

func migrateAccount(s *Store) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var migrated int
	if err = tx.QueryRow("SELECT count(*) FROM settings WHERE key='account_sessions_v2'").Scan(&migrated); err != nil || migrated != 0 {
		return err
	}
	// Old session IDs were credentials, so they cannot become public IDs.
	_, err = tx.Exec(`
		ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
		ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';
		DELETE FROM sessions;
		ALTER TABLE sessions ADD COLUMN refresh_digest TEXT;
		ALTER TABLE sessions ADD COLUMN logged_at INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE sessions ADD COLUMN last_activity_at INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE sessions ADD COLUMN ip TEXT NOT NULL DEFAULT '';
		ALTER TABLE sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
		CREATE UNIQUE INDEX session_refresh ON sessions(refresh_digest);
		CREATE INDEX sessions_user ON sessions(user_id,expires);
		CREATE TABLE session_access(digest TEXT PRIMARY KEY, session_id TEXT NOT NULL, expires INTEGER NOT NULL);
		CREATE INDEX session_access_session ON session_access(session_id);
		CREATE INDEX session_access_expiry ON session_access(expires);
		CREATE TRIGGER session_access_cleanup AFTER DELETE ON sessions BEGIN
			DELETE FROM session_access WHERE session_id=OLD.id;
		END;
		INSERT INTO settings VALUES('account_sessions_v2', '1');
	`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type sessionIdentity struct {
	userID, sessionID, digest string
	expires                   int64
	local                     bool
}

type sessionKey struct{}

func requestSession(ctx context.Context) sessionIdentity {
	v, _ := ctx.Value(sessionKey{}).(sessionIdentity)
	return v
}

func digestToken(token string) string {
	d := sha256.Sum256([]byte(token))
	return hex.EncodeToString(d[:])
}

func (a *Server) authenticate(r *http.Request) (s sessionIdentity) {
	if localRequest(r) {
		s.local = true
		a.store.DB.QueryRow("SELECT a.user_id FROM role_assignments a JOIN users u ON u.id=a.user_id WHERE a.role_id=? AND a.scope=? ORDER BY a.created,a.rowid LIMIT 1", platformAdminRole, scopePlatform).Scan(&s.userID)
		return s
	}
	c, err := r.Cookie(accessCookie)
	if err != nil || c.Value == "" {
		return s
	}
	s.digest = digestToken(c.Value)
	now := time.Now().Unix()
	a.store.DB.QueryRow(`SELECT s.user_id,s.id,t.expires FROM session_access t JOIN sessions s ON s.id=t.session_id
		WHERE t.digest=? AND t.expires>? AND s.expires>? AND s.refresh_digest IS NOT NULL`, s.digest, now, now).Scan(&s.userID, &s.sessionID, &s.expires)
	return s
}

func setAuthCookie(w http.ResponseWriter, r *http.Request, name, token string, expires int64) {
	path := "/"
	if name == refreshCookie {
		path = "/api"
	}
	maxAge := -1
	if token != "" {
		maxAge = max(1, int(expires-time.Now().Unix()))
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: token, Path: path, HttpOnly: true,
		Secure: secureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func issueAccess(tx *sql.Tx, sid string, now, sessionExpiry int64) (string, int64, error) {
	token := randomID()
	expiry := min(now+int64(accessTTL/time.Second), sessionExpiry)
	if _, err := tx.Exec("DELETE FROM session_access WHERE expires<=?", now); err != nil {
		return "", 0, err
	}
	_, err := tx.Exec("INSERT INTO session_access VALUES(?,?,?)", digestToken(token), sid, expiry)
	return token, expiry, err
}

func trimMetadata(s string, size int) string {
	return strings.ToValidUTF8(s[:min(size, len(s))], "")
}

func (a *Server) login(w http.ResponseWriter, r *http.Request) {
	var v struct{ Username, Password string }
	if decode(w, r, &v) != nil {
		fail(w, 400, "invalid JSON")
		return
	}
	var uid, hash string
	err := a.store.DB.QueryRow("SELECT id,password FROM users WHERE username=?", strings.TrimSpace(v.Username)).Scan(&uid, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(v.Password)) != nil {
		fail(w, 401, "invalid username or password")
		return
	}
	a.store.Lock()
	defer a.store.Unlock()
	tx, err := a.store.DB.Begin()
	if err != nil {
		fail(w, 500, "session failed")
		return
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	expires := now + int64(sessionTTL/time.Second)
	sid, refresh := randomID(), randomID()
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if net.ParseIP(ip) == nil {
		ip = ""
	}
	// The peer address is trusted; user-controlled forwarding headers are not.
	res, err := tx.Exec(`INSERT INTO sessions(id,user_id,expires,refresh_digest,logged_at,last_activity_at,ip,user_agent)
		SELECT ?,id,?,?,?,?,?,? FROM users WHERE id=? AND password=?`, sid, expires, digestToken(refresh), now, now, ip, trimMetadata(r.UserAgent(), 512), uid, hash)
	if err != nil {
		fail(w, 500, "session failed")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		fail(w, 401, "credentials changed; sign in again")
		return
	}
	token, tokenExpiry, err := issueAccess(tx, sid, now, expires)
	if err == nil {
		_, err = tx.Exec("DELETE FROM sessions WHERE expires<=?", now)
	}
	if err != nil || tx.Commit() != nil {
		fail(w, 500, "session failed")
		return
	}
	ctx := context.WithValue(r.Context(), sessionKey{}, sessionIdentity{userID: uid, sessionID: sid})
	a.audit(ctx, uid, "session.login", "Signed in", scopeAccount, uid, nil)
	a.publishAccount(uid)
	setAuthCookie(w, r, accessCookie, token, tokenExpiry)
	setAuthCookie(w, r, refreshCookie, refresh, expires)
	reply(w, 200, map[string]string{"id": uid})
}

func (a *Server) refreshSession(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(refreshCookie)
	if err != nil || c.Value == "" {
		fail(w, 401, "sign in again")
		return
	}
	tx, err := a.store.DB.Begin()
	if err != nil {
		fail(w, 500, "session unavailable")
		return
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	var sid string
	var expires int64
	err = tx.QueryRow("SELECT id,expires FROM sessions WHERE refresh_digest=? AND expires>?", digestToken(c.Value), now).Scan(&sid, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 401, "sign in again")
		return
	}
	if err != nil {
		fail(w, 500, "session unavailable")
		return
	}
	token, tokenExpiry, err := issueAccess(tx, sid, now, expires)
	if err != nil || tx.Commit() != nil {
		fail(w, 500, "session unavailable")
		return
	}
	setAuthCookie(w, r, accessCookie, token, tokenExpiry)
	reply(w, 200, map[string]bool{"ok": true})
}

func (a *Server) touchSession(s sessionIdentity) {
	if s.sessionID == "" {
		return
	}
	now := time.Now().Unix()
	if res, err := a.store.DB.Exec("UPDATE sessions SET last_activity_at=? WHERE id=? AND last_activity_at<=?", now, s.sessionID, now-60); err == nil {
		if n, _ := res.RowsAffected(); n != 0 {
			a.publishAccount(s.userID)
		}
	}
}

func (a *Server) liveSessionClose(s sessionIdentity) int {
	var valid bool
	a.store.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sessions WHERE id=? AND expires>?)", s.sessionID, time.Now().Unix()).Scan(&valid)
	if valid {
		return 4408
	}
	return 4401
}

type accountSession struct {
	ID             string    `json:"id"`
	IP             string    `json:"ip"`
	UserAgent      string    `json:"user_agent"`
	LoggedAt       time.Time `json:"logged_at"`
	LastActivityAt time.Time `json:"last_activity_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

func (a *Server) accountSessions(uid string) ([]accountSession, error) {
	rows, err := a.store.DB.Query("SELECT id,ip,user_agent,logged_at,last_activity_at,expires FROM sessions WHERE user_id=? AND expires>? ORDER BY last_activity_at DESC, logged_at DESC,rowid DESC", uid, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []accountSession{}
	for rows.Next() {
		var v accountSession
		var logged, active, expiry int64
		if err := rows.Scan(&v.ID, &v.IP, &v.UserAgent, &logged, &active, &expiry); err != nil {
			return nil, err
		}
		v.LoggedAt, v.LastActivityAt, v.ExpiresAt = time.Unix(logged, 0).UTC(), time.Unix(active, 0).UTC(), time.Unix(expiry, 0).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

func (a *Server) endSession(w http.ResponseWriter, r *http.Request, s sessionIdentity, sid, action string) {
	result, err := a.store.DB.Exec("DELETE FROM sessions WHERE user_id=? AND id=?", s.userID, sid)
	if err != nil {
		fail(w, 500, "could not end session")
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		fail(w, 404, "session not found")
		return
	}
	target := "Session revoked"
	if action == "session.logout" {
		target = "Signed out"
	}
	a.audit(r.Context(), s.userID, action, target, scopeAccount, s.userID, map[string]string{"session_id": sid})
	a.publishAccount(s.userID)
	if sid == s.sessionID {
		setAuthCookie(w, r, accessCookie, "", 0)
		setAuthCookie(w, r, refreshCookie, "", 0)
	}
	reply(w, 200, map[string]bool{"ok": true})
}

func (a *Server) logout(w http.ResponseWriter, r *http.Request) {
	s := a.authenticate(r)
	if s.userID == "" {
		if c, err := r.Cookie(refreshCookie); err == nil && c.Value != "" {
			a.store.DB.QueryRow("SELECT user_id,id FROM sessions WHERE refresh_digest=? AND expires>?", digestToken(c.Value), time.Now().Unix()).Scan(&s.userID, &s.sessionID)
		}
	}
	if s.sessionID != "" {
		r = r.WithContext(context.WithValue(r.Context(), sessionKey{}, s))
		a.endSession(w, r, s, s.sessionID, "session.logout")
		return
	}
	setAuthCookie(w, r, accessCookie, "", 0)
	setAuthCookie(w, r, refreshCookie, "", 0)
	reply(w, 200, map[string]bool{"ok": true})
}

type accountProfile struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

func (a *Server) updateAccount(w http.ResponseWriter, r *http.Request, s sessionIdentity) {
	var v struct {
		Username        *string `json:"username"`
		DisplayName     *string `json:"display_name"`
		Email           *string `json:"email"`
		CurrentPassword string  `json:"current_password"`
		NewPassword     *string `json:"new_password"`
	}
	if decode(w, r, &v) != nil {
		fail(w, 400, "invalid JSON")
		return
	}
	if v.Username == nil && v.DisplayName == nil && v.Email == nil && v.NewPassword == nil {
		fail(w, 400, "no account changes provided")
		return
	}
	if v.CurrentPassword == "" {
		fail(w, 400, "current password required")
		return
	}
	if v.Username != nil {
		*v.Username = strings.TrimSpace(*v.Username)
		if len(*v.Username) < 1 || len(*v.Username) > 100 {
			fail(w, 400, "username must be between 1 and 100 bytes")
			return
		}
	}
	if v.DisplayName != nil {
		*v.DisplayName = strings.TrimSpace(*v.DisplayName)
		if len(*v.DisplayName) > 100 {
			fail(w, 400, "display name must be at most 100 bytes")
			return
		}
	}
	if v.Email != nil {
		*v.Email = strings.TrimSpace(*v.Email)
		addr, err := mail.ParseAddress(*v.Email)
		if *v.Email != "" && (len(*v.Email) > 254 || err != nil || addr.Address != *v.Email) {
			fail(w, 400, "invalid email address")
			return
		}
	}
	if v.NewPassword != nil && (len(*v.NewPassword) < 8 || len(*v.NewPassword) > 72) {
		fail(w, 400, "new password must be between 8 and 72 bytes")
		return
	}
	tx, err := a.store.DB.Begin()
	if err != nil {
		fail(w, 500, "account update failed")
		return
	}
	defer tx.Rollback()
	profile := accountProfile{ID: s.userID}
	var hash string
	var pending bool
	var expiry int64
	now := time.Now().Unix()
	err = tx.QueryRow(`SELECT u.username,u.display_name,u.email,u.password,u.must_change_password,s.expires
		FROM users u JOIN sessions s ON s.user_id=u.id JOIN session_access t ON t.session_id=s.id
		WHERE u.id=? AND s.id=? AND s.expires>? AND t.digest=? AND t.expires>?`, s.userID, s.sessionID, now, s.digest, now).
		Scan(&profile.Username, &profile.DisplayName, &profile.Email, &hash, &pending, &expiry)
	if err != nil {
		fail(w, 401, "session expired; sign in again")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(v.CurrentPassword)) != nil {
		fail(w, 403, "current password is incorrect")
		return
	}
	changed := []string{}
	rotate := false
	if v.Username != nil && *v.Username != profile.Username {
		var count int
		if err = tx.QueryRow("SELECT count(*) FROM users WHERE username=? AND id<>?", *v.Username, s.userID).Scan(&count); err != nil {
			fail(w, 500, "account update failed")
			return
		}
		if count != 0 {
			fail(w, 409, "username is already in use")
			return
		}
		profile.Username, rotate = *v.Username, true
		changed = append(changed, "username")
	}
	if v.DisplayName != nil && *v.DisplayName != profile.DisplayName {
		profile.DisplayName = *v.DisplayName
		changed = append(changed, "display_name")
	}
	if v.Email != nil && *v.Email != profile.Email {
		profile.Email = *v.Email
		changed = append(changed, "email")
	}
	if v.NewPassword != nil {
		if *v.NewPassword == v.CurrentPassword {
			fail(w, 400, "choose a different password")
			return
		}
		generated, err := bcrypt.GenerateFromPassword([]byte(*v.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			fail(w, 500, "account update failed")
			return
		}
		hash, rotate = string(generated), true
		changed = append(changed, "password")
	}
	completed := pending && v.NewPassword != nil
	if completed {
		pending = false
		if _, err = tx.Exec("INSERT OR IGNORE INTO settings VALUES('setup_completed',?)", nowStamp()); err != nil {
			fail(w, 500, "account update failed")
			return
		}
	}
	_, err = tx.Exec("UPDATE users SET username=?,display_name=?,email=?,password=?,must_change_password=? WHERE id=?", profile.Username, profile.DisplayName, profile.Email, hash, pending, s.userID)
	var token, refresh string
	var accessExpiry int64
	if err == nil && rotate {
		refresh = randomID()
		_, err = tx.Exec("DELETE FROM sessions WHERE user_id=? AND id<>?", s.userID, s.sessionID)
		if err == nil {
			_, err = tx.Exec("DELETE FROM session_access WHERE session_id=?", s.sessionID)
		}
		if err == nil {
			_, err = tx.Exec("UPDATE sessions SET refresh_digest=? WHERE id=?", digestToken(refresh), s.sessionID)
		}
		if err == nil {
			token, accessExpiry, err = issueAccess(tx, s.sessionID, time.Now().Unix(), expiry)
		}
	}
	if err != nil || tx.Commit() != nil {
		fail(w, 500, "account update failed")
		return
	}
	if len(changed) != 0 {
		a.audit(r.Context(), s.userID, "account.update", "Account settings", scopeAccount, s.userID, map[string]any{"changed": changed})
	}
	if rotate {
		setAuthCookie(w, r, accessCookie, token, accessExpiry)
		setAuthCookie(w, r, refreshCookie, refresh, expiry)
		a.publishAccount(s.userID)
	}
	if completed {
		log.Print("Setup complete: administrator password changed")
	}
	reply(w, 200, profile)
}
