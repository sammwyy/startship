package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketIdentity(t *testing.T) {
	a := testServer(t)
	if _, err := a.createUserPending("admin", "password123", true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "s.sock")
	srv, err := a.serveSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0660 {
		t.Fatal(info, err)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}}
	defer client.CloseIdleConnections()
	for _, tc := range []struct {
		method, path, contains string
		status                 int
	}{
		{"GET", "me", `"local_admin":true`, 200},
		{"GET", "workspaces", "admin's Workspace", 200},
		{"POST", "logout", "requires an HTTP login session", 400},
		{"POST", "login", "requires an HTTP login session", 400},
		{"PATCH", "me", "requires an HTTP login session", 400},
	} {
		r, _ := http.NewRequest(tc.method, "http://startship/api/"+tc.path, nil)
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status || !strings.Contains(string(b), tc.contains) {
			t.Fatalf("%s: %d %s", tc.path, resp.StatusCode, b)
		}
	}
	tcp := httptest.NewServer(a.api())
	defer tcp.Close()
	resp, err := http.Get(tcp.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal(resp.Status)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("socket not removed", err)
	}
}

func TestSocketPathAndNonSocket(t *testing.T) {
	t.Setenv("STARTSHIP_SOCKET", "")
	t.Setenv("RUNTIME_DIRECTORY", "")
	if socketPath("/data") != "/data/startship.sock" {
		t.Fatal(socketPath("/data"))
	}
	t.Setenv("RUNTIME_DIRECTORY", "/run/startship")
	if socketPath("/data") != "/run/startship/startship.sock" {
		t.Fatal(socketPath("/data"))
	}
	t.Setenv("STARTSHIP_SOCKET", "/custom.sock")
	if socketPath("/data") != "/custom.sock" {
		t.Fatal(socketPath("/data"))
	}
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Server{}).serveSocket(path); err == nil {
		t.Fatal("accepted regular file")
	}
}
