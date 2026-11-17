package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sammwyy/startship/internal/update"
	"github.com/sammwyy/startship/internal/version"
)

func updateFixture(t *testing.T) (*cli, *bytes.Buffer, string, *int) {
	t.Helper()
	assets := map[string]string{"startship_linux_amd64": "cli v1.5.0", "startshipd_linux_amd64": "daemon v1.5.0"}
	sums := ""
	for name, body := range assets {
		h := sha256.Sum256([]byte(body))
		sums += hex.EncodeToString(h[:]) + "  " + name + "\n"
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			w.Write([]byte(`{"tag_name":"v1.5.0","html_url":"https://example.test/v1.5.0"}`))
		case "/download/checksums.txt":
			w.Write([]byte(sums))
		case "/api/system/update":
			if r.Method == "POST" {
				w.WriteHeader(202)
				w.Write([]byte(`{"version":"v1.5.0","by":"admin"}`))
				return
			}
			w.Write([]byte(`{"current":"v1.4.0","latest":"v1.5.0","update":{"state":"failed","version":"v1.5.0","error":"boom"}}`))
		default:
			body, ok := assets[strings.TrimPrefix(r.URL.Path, "/download/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(body))
		}
	}))
	t.Cleanup(s.Close)
	dir := t.TempDir()
	for _, n := range update.Binaries {
		os.WriteFile(filepath.Join(dir, n), []byte("old"), 0o755)
	}
	restarts := 0
	var out bytes.Buffer
	c := &cli{config: config{Server: s.URL}, path: filepath.Join(t.TempDir(), "config.json"), in: bufio.NewReader(strings.NewReader("")), out: &out, client: s.Client(),
		updates: &update.Client{HTTP: s.Client(), ReleasesURL: s.URL + "/releases", DownloadURL: s.URL + "/download", GOOS: "linux", GOARCH: "amd64"},
		binDir:  dir, restart: func() (bool, error) { restarts++; return true, nil }}
	old := version.Version
	version.Version = "v1.4.0"
	t.Cleanup(func() { version.Version = old })
	return c, &out, dir, &restarts
}

func TestUpdateCheckAndDaemonRequest(t *testing.T) {
	c, out, _, _ := updateFixture(t)
	if err := c.run([]string{"update", "check"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Latest release: v1.5.0", "CLI:            v1.4.0 (update available", "Daemon:         v1.4.0", "Last update:    failed v1.5.0: boom"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	out.Reset()
	if err := c.run([]string{"update", "daemon"}); err != nil || !strings.Contains(out.String(), "queued") {
		t.Fatal(err, out)
	}
	if err := c.run([]string{"update", "bogus"}); err == nil {
		t.Fatal("accepted unknown subcommand")
	}
}

func TestUpdateLocalInstallsBothAndRestarts(t *testing.T) {
	c, out, dir, restarts := updateFixture(t)
	if err := c.run([]string{"update"}); err != nil {
		t.Fatal(err)
	}
	for n, want := range map[string]string{"startship": "cli v1.5.0", "startshipd": "daemon v1.5.0"} {
		if b, _ := os.ReadFile(filepath.Join(dir, n)); string(b) != want {
			t.Errorf("%s = %q", n, b)
		}
	}
	if *restarts != 1 || !strings.Contains(out.String(), "Restarted startshipd") {
		t.Fatal(*restarts, out)
	}
	version.Version = "v1.5.0"
	out.Reset()
	if err := c.run([]string{"update"}); err != nil || !strings.Contains(out.String(), "Already up to date") {
		t.Fatal(err, out)
	}
}

func TestApplyRequest(t *testing.T) {
	c, _, dir, restarts := updateFixture(t)
	state := t.TempDir()
	req, status := filepath.Join(state, "request.json"), filepath.Join(state, "status.json")
	t.Setenv("STARTSHIP_UPDATE_REQUEST", req)
	t.Setenv("STARTSHIP_UPDATE_STATUS", status)
	if err := c.run([]string{"update", "apply-request"}); err != nil {
		t.Fatal("missing request must be a no-op:", err)
	}
	update.WriteJSON(req, update.Request{Version: "v1.5.0", Requested: time.Now(), By: "admin"}, 0o640)
	if err := c.run([]string{"update", "apply-request"}); err != nil {
		t.Fatal(err)
	}
	var s update.Status
	if err := update.ReadJSON(status, &s); err != nil || s.State != "succeeded" || s.Version != "v1.5.0" {
		t.Fatal(s, err)
	}
	if _, err := os.Stat(req); !os.IsNotExist(err) {
		t.Fatal("request not consumed")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "startshipd")); string(b) != "daemon v1.5.0" || *restarts != 1 {
		t.Fatal(string(b), *restarts)
	}
	os.WriteFile(req, []byte(fmt.Sprintf(`{"version":%q}`, "latest")), 0o640)
	if err := c.run([]string{"update", "apply-request"}); err == nil {
		t.Fatal("accepted invalid version")
	}
	if err := update.ReadJSON(status, &s); err != nil || s.State != "failed" || s.Error == "" {
		t.Fatal(s, err)
	}
}
