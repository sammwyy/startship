package catalog

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
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

func field(key, value string) Field {
	return Field{Key: key, Label: key, Type: "text", Default: value, Required: true}
}
func credentials() []Field {
	return []Field{field("user", "startship"), {Key: "password", Label: "Password", Type: "password", Generate: true, Required: true}, field("database", "startship")}
}

var engines = []Engine{
	{ID: "postgresql", Name: "PostgreSQL", Category: "database", Image: "postgres", Versions: []string{"17", "16", "15"}, Port: 5432, DataPath: "/var/lib/postgresql/data", Fields: credentials()},
	{ID: "mysql", Name: "MySQL", Category: "database", Image: "mysql", Versions: []string{"8.4", "8.0"}, Port: 3306, DataPath: "/var/lib/mysql", Fields: credentials()},
	{ID: "mariadb", Name: "MariaDB", Category: "database", Image: "mariadb", Versions: []string{"11.4", "10.11"}, Port: 3306, DataPath: "/var/lib/mysql", Fields: credentials()},
	{ID: "mongodb", Name: "MongoDB", Category: "database", Image: "mongo", Versions: []string{"8.0", "7.0"}, Port: 27017, DataPath: "/data/db", Fields: credentials()},
	{ID: "redis", Name: "Redis", Category: "cache", Image: "redis", Versions: []string{"7.4", "7.2"}, Port: 6379, DataPath: "/data"},
	{ID: "valkey", Name: "Valkey", Category: "cache", Image: "valkey/valkey", Versions: []string{"8.0", "7.2"}, Port: 6379, DataPath: "/data"},
	{ID: "memcached", Name: "Memcached", Category: "cache", Image: "memcached", Versions: []string{"1.6"}, Port: 11211},
	{ID: "rabbitmq", Name: "RabbitMQ", Category: "queue", Image: "rabbitmq", Versions: []string{"4.1", "4.0"}, Port: 5672, DataPath: "/var/lib/rabbitmq", Fields: credentials()[:2]},
	{ID: "kafka", Name: "Kafka", Category: "queue", Image: "apache/kafka", Versions: []string{"3.9.0"}, Port: 9092, DataPath: "/var/lib/kafka/data", Fields: []Field{{Key: "external_host", Label: "External hostname", Type: "text", Default: "localhost", Required: true, Help: "Public hostname or IP advertised to external Kafka clients (without a port)."}}},
	{ID: "nats", Name: "NATS", Category: "queue", Image: "nats", Versions: []string{"2.10"}, Port: 4222, DataPath: "/data"},
	{ID: "clickhouse", Name: "ClickHouse", Category: "database", Image: "clickhouse/clickhouse-server", Versions: []string{"25.3", "24.8"}, Port: 8123, DataPath: "/var/lib/clickhouse", Fields: credentials()},
	{ID: "minio", Name: "MinIO", Category: "storage", Image: "minio/minio", Versions: []string{"RELEASE.2025-04-22T22-12-26Z"}, Port: 9000, DataPath: "/data", Fields: credentials()[:2]},
	{ID: "opensearch", Name: "OpenSearch", Category: "search", Image: "opensearchproject/opensearch", Versions: []string{"2.19.1"}, Port: 9200, DataPath: "/usr/share/opensearch/data"},
}

func All() []Engine {
	out := make([]Engine, len(engines))
	for i, e := range engines {
		e.Icon = e.ID
		e.Description = e.Name + " " + e.Category + " service."
		e.Versions = slices.Clone(e.Versions)
		e.Fields = slices.Clone(e.Fields)
		out[i] = e
	}
	return out
}
func Lookup(id string) (Engine, bool) {
	for _, e := range All() {
		if e.ID == id {
			return e, true
		}
	}
	return Engine{}, false
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
	case "minio":
		out["MINIO_ROOT_USER"] = v["user"]
		out["MINIO_ROOT_PASSWORD"] = v["password"]
	case "opensearch":
		out["discovery.type"] = "single-node"
		out["DISABLE_SECURITY_PLUGIN"] = "true"
		out["DISABLE_INSTALL_DEMO_CONFIG"] = "true"
		out["OPENSEARCH_JAVA_OPTS"] = "-Xms512m -Xmx512m"
	case "kafka":
		out = map[string]string{"KAFKA_NODE_ID": "1", "KAFKA_PROCESS_ROLES": "broker,controller", "KAFKA_LISTENERS": "PLAINTEXT://:9092,CONTROLLER://:9093", "KAFKA_ADVERTISED_LISTENERS": "PLAINTEXT://localhost:9092", "KAFKA_CONTROLLER_LISTENER_NAMES": "CONTROLLER", "KAFKA_LISTENER_SECURITY_PROTOCOL_MAP": "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT", "KAFKA_CONTROLLER_QUORUM_VOTERS": "1@localhost:9093", "KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR": "1", "KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1", "KAFKA_TRANSACTION_STATE_LOG_MIN_ISR": "1", "KAFKA_LOG_DIRS": e.DataPath}
	}
	return out
}
func (e Engine) Command() []string {
	switch e.ID {
	case "redis", "valkey":
		return []string{"--appendonly", "yes"}
	case "nats":
		return []string{"-js", "-sd", "/data"}
	case "minio":
		return []string{"server", "/data"}
	}
	return nil
}

var separator = regexp.MustCompile(`[^A-Z0-9]`)

func (e Engine) Connection(alias string, port int, v map[string]string) map[string]string {
	host := alias + ".local"
	prefix := separator.ReplaceAllString(strings.ToUpper(alias), "_") + "_"
	scheme := map[string]string{"postgresql": "postgres", "mariadb": "mysql", "rabbitmq": "amqp", "valkey": "redis", "clickhouse": "http", "minio": "http", "opensearch": "http"}[e.ID]
	if scheme == "" {
		scheme = e.ID
	}
	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port))}
	if v["user"] != "" {
		u.User = url.UserPassword(v["user"], v["password"])
	}
	if v["database"] != "" {
		u.Path = "/" + v["database"]
	}
	if e.ID == "mongodb" {
		u.RawQuery = "authSource=admin"
	}
	out := map[string]string{prefix + "HOST": host, prefix + "PORT": strconv.Itoa(port), prefix + "URL": u.String()}
	for _, k := range []string{"user", "password", "database"} {
		if v[k] != "" {
			out[prefix+strings.ToUpper(k)] = v[k]
		}
	}
	return out
}
