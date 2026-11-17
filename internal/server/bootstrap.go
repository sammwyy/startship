package server

import (
	"crypto/rand"
	"database/sql"
	"log"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

func (a *Server) setupComplete() bool {
	var n int
	return a.store.DB.QueryRow("SELECT count(*) FROM settings WHERE key='setup_completed'").Scan(&n) == nil && n > 0
}

func (a *Server) markSetupComplete() error {
	_, err := a.store.DB.Exec("INSERT OR IGNORE INTO settings VALUES('setup_completed',strftime('%Y-%m-%dT%H:%M:%SZ','now'))")
	return err
}

func (a *Server) bootstrapAdmin(username, password string) error {
	var n int
	if err := a.store.DB.QueryRow("SELECT count(*) FROM users").Scan(&n); err != nil {
		return err
	}
	if n == 0 && username != "" && password != "" {
		if _, err := a.createUser(username, password); err != nil {
			return err
		}
		return a.markSetupComplete()
	}
	var id string
	if n > 0 {
		if a.setupComplete() {
			return nil
		}
		if err := a.store.DB.QueryRow("SELECT u.id,u.username FROM users u JOIN role_assignments a ON a.user_id=u.id AND a.role_id=? AND a.scope=? WHERE u.must_change_password=1 ORDER BY u.rowid LIMIT 1", platformAdminRole, scopePlatform).Scan(&id, &username); err != nil {
			if err != sql.ErrNoRows {
				return err
			}
			return a.markSetupComplete()
		}
	} else {
		username = "admin"
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#%+-"
	b := make([]byte, 20)
	for i := range b {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return err
		}
		b[i] = alphabet[v.Int64()]
	}
	password = string(b)
	if id == "" {
		var err error
		id, err = a.createUserPending(username, password, true)
		if err != nil {
			return err
		}
	}
	hash, err := bcrypt.GenerateFromPassword(b, bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := a.store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE users SET password=?,must_change_password=1 WHERE id=?", string(hash), id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE user_id=?", id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	log.Printf("\n============================================================\nSTARTSHIP FIRST-RUN ADMIN — SETUP PENDING\nDashboard: http://localhost:8080 (or your server address)\nUsername: %s\nPassword: %s\nYou MUST change this password on first sign-in.\nUntil setup is complete, restarting rotates these credentials.\n============================================================", username, password)
	return nil
}
