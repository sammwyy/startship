package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDaemonConfigCLI(t *testing.T) {
	t.Setenv("HOME", "")
	path := filepath.Join(t.TempDir(), "startship.toml")
	t.Setenv("STARTSHIP_CONFIG", path)
	var out bytes.Buffer
	c := &cli{out: &out}
	for _, args := range [][]string{{"config", "path"}, {"config", "show"}, {"config", "set", "dashboard_listen", "127.0.0.1:8080", "lo:8081"}, {"config", "get", "dashboard_listen"}} {
		if err := c.run(args); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(out.String(), path) || !strings.Contains(out.String(), "sudo systemctl restart startshipd") || !strings.Contains(out.String(), `["127.0.0.1:8080","lo:8081"]`) {
		t.Fatal(out.String())
	}
	if err := c.run([]string{"config", "unset", "dashboard_listen"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "get", "dashboard_listen"}, {"config", "set", "unknown", "x"}, {"config", "set", "http_listen", "wrong"}, {"config", "set", "data_dir"}, {"config", "unset"}, {"config", "--config"}, {"config", "show", "extra"}} {
		if err := c.run(args); err == nil {
			t.Fatal(args)
		}
	}
	other := filepath.Join(t.TempDir(), "other.toml")
	if err := c.run([]string{"config", "--config", other, "set", "data_dir", "/srv/startship"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(other)
	if !strings.Contains(string(b), "/srv/startship") {
		t.Fatal(string(b))
	}
}
