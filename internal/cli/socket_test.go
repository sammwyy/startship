package cli

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketTransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"username":"admin","local_admin":true}`))
	})}
	go srv.Serve(l)
	defer srv.Close()
	t.Setenv("STARTSHIP_SOCKET", path)
	for _, server := range []string{"", "unix://" + path} {
		var out bytes.Buffer
		c := &cli{config: config{Server: server}, out: &out, client: http.DefaultClient}
		if err := c.run([]string{"whoami"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "local_admin: true") {
			t.Fatal(out.String())
		}
	}
	c := &cli{config: config{Server: "https://remote.example"}, client: http.DefaultClient}
	server, _, err := c.transport()
	if err != nil || server != c.Server {
		t.Fatal(server, err)
	}
	t.Setenv("STARTSHIP_SOCKET", filepath.Join(t.TempDir(), "absent"))
	c.Server = ""
	server, _, err = c.transport()
	if err != nil || server != "http://localhost:9000" {
		t.Fatal(server, err)
	}
	c.Server = "unix://host/not-allowed"
	if _, _, err = c.transport(); err == nil {
		t.Fatal("accepted invalid URL")
	}
	c.Server = "unix:///does/not/exist"
	if _, err := c.request("GET", "me", nil); err == nil {
		t.Fatal("missing socket accepted")
	}
}

func TestLocalLogout(t *testing.T) {
	c := &cli{config: config{Token: "old"}, path: filepath.Join(t.TempDir(), "config.json")}
	if err := c.run([]string{"logout"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(c.path)
	if err != nil || strings.Contains(string(b), "old") {
		t.Fatal(string(b), err)
	}
}
