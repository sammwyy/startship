package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"github.com/sammwyy/startship/internal/pipeline"
	_ "modernc.org/sqlite"
	"sync"
	"time"
)

type Record struct {
	Previews          bool               `json:"previews"`
	PreviewID         string             `json:"preview_id,omitempty"`
	Addresses         []string           `json:"addresses,omitempty"`
	ID                string             `json:"id"`
	Kind              string             `json:"-"`
	Owner             string             `json:"-"`
	Parent            string             `json:"-"`
	Name              string             `json:"name,omitempty"`
	WorkspaceID       string             `json:"workspace_id,omitempty"`
	ProjectID         string             `json:"project_id,omitempty"`
	AppID             string             `json:"app_id,omitempty"`
	ContainerID       string             `json:"container_id,omitempty"`
	ReleaseID         string             `json:"release_id,omitempty"`
	ContainerReleases []string           `json:"container_releases,omitempty"`
	GitURL            string             `json:"git_url,omitempty"`
	GitToken          string             `json:"git_token,omitempty"`
	Branch            string             `json:"branch,omitempty"`
	RootDir           string             `json:"root_dir,omitempty"`
	BuildType         string             `json:"build_type,omitempty"`
	Pipeline          *pipeline.Pipeline `json:"pipeline,omitempty"`
	Port              int                `json:"port,omitempty"`
	Env               map[string]string  `json:"env,omitempty"`
	TestCommand       string             `json:"test_command,omitempty"`
	AutoDeploy        bool               `json:"auto_deploy"`
	WebhookSecret     string             `json:"webhook_secret,omitempty"`
	Active            string             `json:"active_deployment,omitempty"`
	SleepAfter        int                `json:"sleep_after,omitempty"`
	Sleeping          bool               `json:"sleeping,omitempty"`
	ResourcesUpdated  *time.Time         `json:"resources_updated,omitempty"`
	Host              string             `json:"host,omitempty"`
	PathPrefix        string             `json:"path_prefix,omitempty"`
	StripPrefix       bool               `json:"strip_prefix"`
	CertificateID     string             `json:"certificate_id,omitempty"`
	ForceHTTPS        bool               `json:"force_https"`
	Status            string             `json:"status,omitempty"`
	Commit            string             `json:"commit,omitempty"`
	Log               string             `json:"log,omitempty"`
	Image             string             `json:"image,omitempty"`
	Container         string             `json:"container,omitempty"`
	Network           string             `json:"network,omitempty"`
	NoCache           bool               `json:"no_cache,omitempty"`
	RedeployOf        string             `json:"redeploy_of,omitempty"`
	IP                string             `json:"ip,omitempty"`
	StaticDir         string             `json:"static_dir,omitempty"`
	Created           time.Time          `json:"created,omitempty"`
	Finished          *time.Time         `json:"finished,omitempty"`
}
type Store struct {
	DB       *sql.DB
	OnChange func(Record, bool)
	sync.Mutex
}

func RandomID() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func Open(path string) (*Store, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;" + schema); e != nil {
		db.Close()
		return nil, e
	}
	return &Store{DB: db}, nil
}
func (s *Store) Save(r Record) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO records VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body, parent=excluded.parent", r.ID, r.Kind, r.Owner, r.Parent, string(b))
	if e == nil {
		s.Notify(r)
	}
	return e
}
func (s *Store) Notify(records ...Record) {
	if s.OnChange != nil {
		for _, r := range records {
			s.OnChange(r, false)
		}
	}
}
func (s *Store) Delete(r Record) error {
	_, err := s.DB.Exec("DELETE FROM records WHERE id=?", r.ID)
	if err == nil && s.OnChange != nil {
		s.OnChange(r, true)
	}
	return err
}
func (s *Store) Get(id string) (Record, error) {
	var r Record
	var b string
	e := s.DB.QueryRow("SELECT kind,owner,parent,body FROM records WHERE id=?", id).Scan(&r.Kind, &r.Owner, &r.Parent, &b)
	if e != nil {
		return r, e
	}
	e = json.Unmarshal([]byte(b), &r)
	return r, e
}
func (s *Store) List(kind, owner string) []Record {
	rows, e := s.DB.Query("SELECT kind,owner,parent,body FROM records WHERE kind=? AND (?='' OR owner=?) ORDER BY rowid", kind, owner, owner)
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
func (s *Store) Update(id string, f func(*Record)) error {
	s.Lock()
	defer s.Unlock()
	r, e := s.Get(id)
	if e != nil {
		return e
	}
	f(&r)
	return s.Save(r)
}
