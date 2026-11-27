package catalog

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/sammwyy/startship/registry"
)

type Field struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Default  string `json:"default"`
	Generate bool   `json:"generate"`
	Required bool   `json:"required"`
	Help     string `json:"help"`
}
type Engine struct {
	ID          string   `json:"id"`
	Icon        string   `json:"icon"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Image       string   `json:"image"`
	Versions    []string `json:"versions"`
	Port        int      `json:"port"`
	DataPath    string   `json:"data_path"`
	Fields      []Field  `json:"fields"`
}

type Registry []Engine

var engines = builtins()

func builtins() Registry {
	out, err := Parse(registry.Services)
	if err != nil {
		panic(fmt.Errorf("built-in services: %w", err))
	}
	return out
}

var engineID = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var fieldKey = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var imageName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*$`)
var imageTag = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127}$`)

func Parse(body []byte) (Registry, error) {
	var index struct {
		Engines Registry `json:"engines"`
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&index); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF || index.Engines == nil {
		return nil, fmt.Errorf("registry must contain one object with an engines array")
	}
	seen := map[string]bool{}
	for i, e := range index.Engines {
		if !engineID.MatchString(e.ID) || seen[e.ID] || strings.TrimSpace(e.Name) == "" ||
			!slices.Contains([]string{"database", "cache", "queue", "search", "storage"}, e.Category) ||
			!imageName.MatchString(e.Image) || strings.Contains(path.Base(e.Image), ":") ||
			e.Port < 1 || e.Port > 65535 || len(e.Versions) == 0 ||
			e.DataPath != "" && (!path.IsAbs(e.DataPath) || e.DataPath == "/" || path.Clean(e.DataPath) != e.DataPath || strings.ContainsAny(e.DataPath, ",\x00")) {
			return nil, fmt.Errorf("invalid or duplicate service engine %q", e.ID)
		}
		seen[e.ID] = true
		versions := map[string]bool{}
		for _, v := range e.Versions {
			if !imageTag.MatchString(v) || versions[v] {
				return nil, fmt.Errorf("%s: invalid or duplicate version %q", e.ID, v)
			}
			versions[v] = true
		}
		fields := map[string]bool{}
		for _, f := range e.Fields {
			if !fieldKey.MatchString(f.Key) || fields[f.Key] || f.Label == "" ||
				!slices.Contains([]string{"text", "password"}, f.Type) ||
				((f.Key == "password" || f.Key == "secret_key") && f.Type != "password") {
				return nil, fmt.Errorf("%s: invalid or duplicate field %q", e.ID, f.Key)
			}
			fields[f.Key] = true
		}
		if e.Icon == "" {
			e.Icon = e.ID
		}
		if e.Description == "" {
			e.Description = e.Name + " " + e.Category + " service."
		}
		index.Engines[i] = e
	}
	return index.Engines, nil
}

func All() []Engine { return engines.All() }

func Lookup(id string) (Engine, bool) { return engines.Lookup(id) }

func (r Registry) All() []Engine {
	out := make([]Engine, len(r))
	for i, e := range r {
		out[i] = clone(e)
	}
	return out
}

func (r Registry) Lookup(id string) (Engine, bool) {
	for _, e := range r {
		if e.ID == id {
			return clone(e), true
		}
	}
	return Engine{}, false
}

func clone(e Engine) Engine {
	e.Versions = slices.Clone(e.Versions)
	e.Fields = slices.Clone(e.Fields)
	return e
}
func Validate(e Engine, version string, values map[string]string) (map[string]string, error) {
	if !slices.Contains(e.Versions, version) {
		return nil, fmt.Errorf("unsupported version")
	}
	out := map[string]string{}
	for k := range values {
		if !slices.ContainsFunc(e.Fields, func(f Field) bool { return f.Key == k }) {
			return nil, fmt.Errorf("unknown config field %s", k)
		}
	}
	for _, f := range e.Fields {
		v, ok := values[f.Key]
		if !ok {
			v = f.Default
			if f.Generate {
				b := make([]byte, 24)
				if _, err := rand.Read(b); err != nil {
					return nil, err
				}
				v = hex.EncodeToString(b)
				if e.ID == "opensearch" {
					v = "Ss1!" + v
				}
			}
		}
		if f.Required && v == "" {
			return nil, fmt.Errorf("%s required", f.Key)
		}
		if f.Key == "external_host" && (strings.ContainsAny(v, " /:@\n\x00") || strings.TrimSpace(v) != v) {
			return nil, fmt.Errorf("external_host must be a hostname or IPv4 address without a port")
		}
		out[f.Key] = v
	}
	if e.ID == "opensearch" {
		if out["user"] != "admin" {
			return nil, fmt.Errorf("OpenSearch uses the built-in admin user")
		}
		p := out["password"]
		if len(p) < 8 || !regexp.MustCompile(`[A-Z]`).MatchString(p) || !regexp.MustCompile(`[a-z]`).MatchString(p) || !regexp.MustCompile(`[0-9]`).MatchString(p) || !regexp.MustCompile(`[^A-Za-z0-9]`).MatchString(p) {
			return nil, fmt.Errorf("OpenSearch password needs at least 8 characters with uppercase, lowercase, number and symbol")
		}
	}
	return out, nil
}
func (e Engine) Env(v map[string]string) map[string]string {
	out := map[string]string{}
	set := func(prefix string) {
		for k, val := range v {
			out[prefix+strings.ToUpper(k)] = val
		}
	}
	switch e.ID {
	case "postgresql":
		out["POSTGRES_USER"] = v["user"]
		out["POSTGRES_PASSWORD"] = v["password"]
		out["POSTGRES_DB"] = v["database"]
	case "mysql", "mariadb":
		set(strings.ToUpper(e.ID) + "_")
		out[strings.ToUpper(e.ID)+"_ROOT_PASSWORD"] = v["password"]
	case "mongodb":
		out["MONGO_INITDB_ROOT_USERNAME"] = v["user"]
		out["MONGO_INITDB_ROOT_PASSWORD"] = v["password"]
		out["MONGO_INITDB_DATABASE"] = v["database"]
	case "rabbitmq":
		out["RABBITMQ_DEFAULT_USER"] = v["user"]
		out["RABBITMQ_DEFAULT_PASS"] = v["password"]
	case "clickhouse":
		set("CLICKHOUSE_")
		out["CLICKHOUSE_DB"] = v["database"]
		out["CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT"] = "1"
	case "seaweedfs":
		b, _ := json.Marshal(map[string]any{"identities": []any{map[string]any{"name": "startship", "credentials": []map[string]string{{"accessKey": v["access_key"], "secretKey": v["secret_key"]}}, "actions": []string{"Admin", "Read", "Write", "List", "Tagging"}}}})
		out["STARTSHIP_S3_CONFIG"] = string(b)
	case "opensearch":
		out["discovery.type"] = "single-node"
		out["OPENSEARCH_INITIAL_ADMIN_PASSWORD"] = v["password"]
		out["node.store.allow_mmap"] = "false"
		out["OPENSEARCH_JAVA_OPTS"] = "-Xms512m -Xmx512m"
	case "kafka":
		out = map[string]string{"KAFKA_NODE_ID": "1", "KAFKA_PROCESS_ROLES": "broker,controller", "KAFKA_LISTENERS": "PLAINTEXT://:9092,CONTROLLER://:9093", "KAFKA_ADVERTISED_LISTENERS": "PLAINTEXT://localhost:9092", "KAFKA_CONTROLLER_LISTENER_NAMES": "CONTROLLER", "KAFKA_LISTENER_SECURITY_PROTOCOL_MAP": "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT", "KAFKA_CONTROLLER_QUORUM_VOTERS": "1@localhost:9093", "KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR": "1", "KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1", "KAFKA_TRANSACTION_STATE_LOG_MIN_ISR": "1", "KAFKA_LOG_DIRS": e.DataPath}
	}
	return out
}
func (e Engine) Command(v map[string]string) []string {
	switch e.ID {
	case "redis", "valkey":
		args := []string{"--appendonly", "yes"}
		if v["password"] != "" {
			args = append(args, "--requirepass", v["password"])
		}
		return args
	case "nats":
		return []string{"-js", "-sd", "/data"}
	case "seaweedfs":
		return []string{"-c", `set -e; umask 077; printf '%s' "$STARTSHIP_S3_CONFIG" > /tmp/startship-s3.json; cd /data; exec weed server -dir=/data -ip=127.0.0.1 -ip.bind=0.0.0.0 -s3 -s3.port=8333 -s3.config=/tmp/startship-s3.json`}
	}
	return nil
}

var separator = regexp.MustCompile(`[^A-Z0-9]`)

func (e Engine) Connection(alias string, port int, v map[string]string) map[string]string {
	host := alias + ".local"
	prefix := separator.ReplaceAllString(strings.ToUpper(alias), "_") + "_"
	scheme := map[string]string{"postgresql": "postgres", "mariadb": "mysql", "rabbitmq": "amqp", "valkey": "redis", "clickhouse": "http", "seaweedfs": "http", "opensearch": "https"}[e.ID]
	if scheme == "" {
		scheme = e.ID
	}
	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port))}
	if v["user"] != "" || v["password"] != "" {
		u.User = url.UserPassword(v["user"], v["password"])
	}
	if v["database"] != "" {
		u.Path = "/" + v["database"]
	}
	if e.ID == "mongodb" {
		u.RawQuery = "authSource=admin"
	}
	out := map[string]string{prefix + "HOST": host, prefix + "PORT": strconv.Itoa(port), prefix + "URL": u.String()}
	for _, k := range []string{"user", "password", "database", "access_key", "secret_key"} {
		if v[k] != "" {
			out[prefix+strings.ToUpper(k)] = v[k]
		}
	}
	if e.ID == "seaweedfs" {
		out[prefix+"ENDPOINT"] = u.String()
	}
	return out
}
