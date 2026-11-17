package main

import (
	"crypto/rand"
	"database/sql"
	"log"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

func (a *Server) setupComplete() bool {
	var n int
	return a.store.db.QueryRow("SELECT count(*) FROM settings WHERE key='setup_completed'").Scan(&n) == nil && n > 0
}

func (a *Server) markSetupComplete() error {
	_, err := a.store.db.Exec("INSERT OR IGNORE INTO settings VALUES('setup_completed',strftime('%Y-%m-%dT%H:%M:%SZ','now'))")
	return err
}

func (a *Server) bootstrapAdmin(username, password string) error {
	var n int
	if err := a.store.db.QueryRow("SELECT count(*) FROM users").Scan(&n); err != nil {
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
		if err := a.store.db.QueryRow("SELECT id,username FROM users WHERE must_change_password=1 LIMIT 1").Scan(&id, &username); err != nil {
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
	tx, err := a.store.db.Begin()
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
