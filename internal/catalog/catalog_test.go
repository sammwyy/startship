package catalog

import (
	"net/url"
	"testing"
)

func TestCatalog(t *testing.T) {
	for _, e := range All() {
		t.Run(e.ID, func(t *testing.T) {
			v, err := Validate(e, e.Versions[0], nil)
			if err != nil {
				t.Fatal(err)
			}
			c := e.Connection("my_db", e.Port, v)
			schemes := map[string]string{"postgresql": "postgres", "mysql": "mysql", "mariadb": "mysql", "mongodb": "mongodb", "redis": "redis", "valkey": "redis", "memcached": "memcached", "rabbitmq": "amqp", "kafka": "kafka", "nats": "nats", "clickhouse": "http", "minio": "http", "opensearch": "https"}
			u, err := url.Parse(c["MY_DB_URL"])
			if err != nil || u.Hostname() != "my_db.local" || c["MY_DB_HOST"] != "my_db.local" || u.Scheme != schemes[e.ID] {
				t.Fatal(c, err)
			}
			if _, err = Validate(e, "latest", nil); err == nil {
				t.Fatal("version accepted")
			}
			if _, err = Validate(e, e.Versions[0], map[string]string{"bogus": "x"}); err == nil {
				t.Fatal("unknown field")
			}
			for _, f := range e.Fields {
				if f.Required {
					if _, err = Validate(e, e.Versions[0], map[string]string{f.Key: ""}); err == nil {
						t.Fatal("empty field accepted")
					}
				}
			}
			_ = e.Env(v)
			_ = e.Command(v)
		})
	}
	if _, ok := Lookup("missing"); ok {
		t.Fatal("missing engine")
	}
}

func TestEngineConfiguration(t *testing.T) {
	keys := map[string]string{"postgresql": "POSTGRES_DB", "mysql": "MYSQL_DATABASE", "mariadb": "MARIADB_DATABASE", "mongodb": "MONGO_INITDB_DATABASE", "rabbitmq": "RABBITMQ_DEFAULT_USER", "clickhouse": "CLICKHOUSE_DB", "minio": "MINIO_ROOT_USER"}
	for id, key := range keys {
		e, _ := Lookup(id)
		v, err := Validate(e, e.Versions[0], nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(v["password"]) != 48 {
			t.Fatal("secret not generated", id)
		}
		env := e.Env(v)
		if env[key] != "startship" {
			t.Fatal(id, env)
		}
		v["password"] = "p@ss:/?#"
		c := e.Connection("db", e.Port, v)
		u, err := url.Parse(c["DB_URL"])
		if err != nil {
			t.Fatal(err)
		}
		password, _ := u.User.Password()
		if password != v["password"] {
			t.Fatal(c)
		}
	}
	for _, id := range []string{"memcached", "nats", "kafka"} {
		e, _ := Lookup(id)
		v, err := Validate(e, e.Versions[0], nil)
		if err != nil || len(v) != 0 && id != "kafka" {
			t.Fatal(id, v, err)
		}
		if e.ID == "kafka" && e.Env(v)["KAFKA_PROCESS_ROLES"] != "broker,controller" {
			t.Fatal("missing KRaft configuration")
		}
	}
}

func TestAuthenticatedDefaults(t *testing.T) {
	for _, id := range []string{"redis", "valkey", "opensearch"} {
		e, _ := Lookup(id)
		v, err := Validate(e, e.Versions[0], nil)
		if err != nil || len(v["password"]) < 48 {
			t.Fatal(id, "missing generated password", err)
		}
		u, err := url.Parse(e.Connection("cache", e.Port, v)["CACHE_URL"])
		if err != nil {
			t.Fatal(err)
		}
		password, _ := u.User.Password()
		if password != v["password"] {
			t.Fatal(id, "URL lost password")
		}
		if id != "opensearch" {
			args := e.Command(v)
			if len(args) != 4 || args[2] != "--requirepass" || args[3] != v["password"] {
				t.Fatal(id, "missing AUTH configuration")
			}
			if len(e.Command(nil)) != 2 {
				t.Fatal("legacy unauthenticated service command changed")
			}
			continue
		}
		env := e.Env(v)
		if v["user"] != "admin" || u.Scheme != "https" || env["OPENSEARCH_INITIAL_ADMIN_PASSWORD"] != v["password"] || env["DISABLE_SECURITY_PLUGIN"] != "" || env["node.store.allow_mmap"] != "false" {
			t.Fatal("incorrect OpenSearch security defaults")
		}
		for _, config := range []map[string]string{{"user": "other"}, {"password": "weak"}} {
			if _, err := Validate(e, e.Versions[0], config); err == nil {
				t.Fatal("invalid OpenSearch credentials accepted")
			}
		}
	}
}
