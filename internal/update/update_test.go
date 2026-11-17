package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.3", 0},
		{"v1.10.0", "v1.9.9", 1},
		{"v2.0.0", "v10.0.0", -1},
		{"v1.0.0-rc.1", "v1.0.0", -1},
		{"v1.0.0-rc.2", "v1.0.0-rc.10", -1},
		{"v1.0.0-alpha", "v1.0.0-1", 1},
		{"v1.0.0+build", "v1.0.0", 0},
		{"dev", "v0.0.1", -1},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	for _, v := range []string{"1.2.3", "v1.2", "v1.2.3/../x", "latest"} {
		if ValidVersion(v) {
			t.Errorf("%q accepted", v)
		}
	}
}

// ReleaseServer serves a fake GitHub release API and assets for version.
func releaseServer(t *testing.T, version string, assets map[string]string, corrupt string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://example.test/%s","body":"notes"}`, version, version)
	})
	sums := ""
	for name, body := range assets {
		h := sha256.Sum256([]byte(body))
		sums += hex.EncodeToString(h[:]) + "  " + name + "\n"
		served := body
		if name == corrupt {
			served += "tampered"
		}
		mux.HandleFunc("/download/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(served)) })
	}
	mux.HandleFunc("/download/checksums.txt", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sums)) })
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func testClient(s *httptest.Server) *Client {
	return &Client{HTTP: s.Client(), ReleasesURL: s.URL + "/releases", DownloadURL: s.URL + "/download", GOOS: "linux", GOARCH: "amd64"}
}

func TestLatestAndInstall(t *testing.T) {
	s := releaseServer(t, "v1.4.0", map[string]string{"startship_linux_amd64": "new cli", "startshipd_linux_amd64": "new daemon"}, "")
	c := testClient(s)
	r, err := c.Latest(context.Background())
	if err != nil || r.Version != "v1.4.0" || r.Notes != "notes" {
		t.Fatal(r, err)
	}
	dir := t.TempDir()
	for _, n := range Binaries {
		os.WriteFile(filepath.Join(dir, n), []byte("old"), 0o755)
	}
	installed, err := c.Install(context.Background(), "v1.4.0", dir, Binaries)
	if err != nil || len(installed) != 2 {
		t.Fatal(installed, err)
	}
	for n, want := range map[string]string{"startship": "new cli", "startshipd": "new daemon"} {
		b, _ := os.ReadFile(filepath.Join(dir, n))
		info, _ := os.Stat(filepath.Join(dir, n))
		if string(b) != want || info.Mode().Perm() != 0o755 {
			t.Errorf("%s = %q %v", n, b, info.Mode())
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("temporary files left: %v", entries)
	}
}

func TestInstallSkipsMissingDaemonButNeedsCLI(t *testing.T) {
	s := releaseServer(t, "v1.4.0", map[string]string{"startship_linux_amd64": "new cli", "startshipd_linux_amd64": "new daemon"}, "")
	dir := t.TempDir()
	if _, err := testClient(s).Install(context.Background(), "v1.4.0", dir, Binaries); err == nil {
		t.Fatal("installed without an existing CLI")
	}
	os.WriteFile(filepath.Join(dir, "startship"), []byte("old"), 0o755)
	installed, err := testClient(s).Install(context.Background(), "v1.4.0", dir, Binaries)
	if err != nil || len(installed) != 1 {
		t.Fatal(installed, err)
	}
}

func TestInstallRejectsChecksumMismatchAtomically(t *testing.T) {
	s := releaseServer(t, "v1.4.0", map[string]string{"startship_linux_amd64": "new cli", "startshipd_linux_amd64": "new daemon"}, "startshipd_linux_amd64")
	dir := t.TempDir()
	for _, n := range Binaries {
		os.WriteFile(filepath.Join(dir, n), []byte("old"), 0o755)
	}
	if _, err := testClient(s).Install(context.Background(), "v1.4.0", dir, Binaries); err == nil {
		t.Fatal("accepted tampered asset")
	}
	for _, n := range Binaries {
		if b, _ := os.ReadFile(filepath.Join(dir, n)); string(b) != "old" {
			t.Errorf("%s replaced despite failed verification", n)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("temporary files left: %v", entries)
	}
	c := testClient(s)
	c.GOOS = "darwin"
	if _, err := c.Install(context.Background(), "v1.4.0", dir, Binaries); err == nil {
		t.Fatal("installed unsupported platform")
	}
	if _, err := testClient(s).Install(context.Background(), "../v1", dir, Binaries); err == nil {
		t.Fatal("accepted invalid version")
	}
}
