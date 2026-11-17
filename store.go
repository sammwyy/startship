package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"sync"
	"time"
)

type Record struct {
	ID            string            `json:"id"`
	Kind          string            `json:"-"`
	Owner         string            `json:"-"`
	Parent        string            `json:"-"`
	Name          string            `json:"name,omitempty"`
	WorkspaceID   string            `json:"workspace_id,omitempty"`
	ProjectID     string            `json:"project_id,omitempty"`
	AppID         string            `json:"app_id,omitempty"`
	GitURL        string            `json:"git_url,omitempty"`
	GitToken      string            `json:"git_token,omitempty"`
	Branch        string            `json:"branch,omitempty"`
	RootDir       string            `json:"root_dir,omitempty"`
	BuildType     string            `json:"build_type,omitempty"`
	Port          int               `json:"port,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	TestCommand   string            `json:"test_command,omitempty"`
	AutoDeploy    bool              `json:"auto_deploy"`
	WebhookSecret string            `json:"webhook_secret,omitempty"`
	Active        string            `json:"active_deployment,omitempty"`
	Host          string            `json:"host,omitempty"`
	PathPrefix    string            `json:"path_prefix,omitempty"`
	StripPrefix   bool              `json:"strip_prefix"`
	Status        string            `json:"status,omitempty"`
	Commit        string            `json:"commit,omitempty"`
	Log           string            `json:"log,omitempty"`
	Image         string            `json:"image,omitempty"`
	Container     string            `json:"container,omitempty"`
	IP            string            `json:"ip,omitempty"`
	StaticDir     string            `json:"static_dir,omitempty"`
	Created       time.Time         `json:"created,omitempty"`
	Finished      *time.Time        `json:"finished,omitempty"`
}
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

func randomID() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func openStore(path string) (*Store, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS records(id TEXT PRIMARY KEY, kind TEXT NOT NULL, owner TEXT NOT NULL, parent TEXT NOT NULL, body TEXT NOT NULL); CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY, username TEXT UNIQUE NOT NULL, password TEXT NOT NULL); CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires INTEGER NOT NULL);")
	return &Store{db: db}, e
}
func (s *Store) save(r Record) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO records VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body, parent=excluded.parent", r.ID, r.Kind, r.Owner, r.Parent, string(b))
	return e
}
func (s *Store) get(id string) (Record, error) {
	var r Record
	var b string
	e := s.db.QueryRow("SELECT kind,owner,parent,body FROM records WHERE id=?", id).Scan(&r.Kind, &r.Owner, &r.Parent, &b)
	if e != nil {
		return r, e
	}
	e = json.Unmarshal([]byte(b), &r)
	return r, e
}
func (s *Store) list(kind, owner string) []Record {
	rows, e := s.db.Query("SELECT kind,owner,parent,body FROM records WHERE kind=? AND (?='' OR owner=?) ORDER BY rowid", kind, owner, owner)
	if e != nil {
		return nil
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		var b string
		if rows.Scan(&r.Kind, &r.Owner, &r.Parent, &b) == nil && json.Unmarshal([]byte(b), &r) == nil {
			out = append(out, r)
		}
	}
	return out
}
func (s *Store) update(id string, f func(*Record)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, e := s.get(id)
	if e != nil {
		return e
	}
	f(&r)
	return s.save(r)
}

var errInvalid = errors.New("invalid request")
