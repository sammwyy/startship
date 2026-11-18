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
			schemes := map[string]string{"postgresql": "postgres", "mysql": "mysql", "mariadb": "mysql", "mongodb": "mongodb", "redis": "redis", "valkey": "redis", "memcached": "memcached", "rabbitmq": "amqp", "kafka": "kafka", "nats": "nats", "clickhouse": "http", "minio": "http", "opensearch": "http"}
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
			_ = e.Command()
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
	for _, id := range []string{"redis", "valkey", "memcached", "nats", "kafka", "opensearch"} {
		e, _ := Lookup(id)
		v, err := Validate(e, e.Versions[0], nil)
		if err != nil || len(v) != 0 && id != "kafka" {
			t.Fatal(id, v, err)
		}
		if e.ID == "kafka" && e.Env(v)["KAFKA_PROCESS_ROLES"] != "broker,controller" {
			t.Fatal("missing KRaft configuration")
		}
		if e.ID == "opensearch" && e.Env(v)["DISABLE_SECURITY_PLUGIN"] != "true" {
			t.Fatal("security not disabled")
		}
	}
}
