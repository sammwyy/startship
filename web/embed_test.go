package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductionDashboard(t *testing.T) {
	handler := Handler()
	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK || !strings.Contains(index.Body.String(), "type=\"module\"") || strings.Contains(index.Body.String(), "/src/main.tsx") {
		t.Fatalf("dashboard must serve the Vite production entrypoint: %d %s", index.Code, index.Body.String())
	}
	if index.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("dashboard entrypoint must revalidate after upgrades")
	}
	assets, err := fs.Glob(files, "dist/assets/*.js")
	if err != nil || len(assets) == 0 {
		t.Fatalf("missing production scripts: %v", err)
	}
	for _, name := range assets {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, strings.TrimPrefix(name, "dist"), nil))
		if r.Code != http.StatusOK || r.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("production script unavailable or not immutable: %s", name)
		}
	}
	for _, path := range []string{"/src/main.tsx", "/package.json", "/app.js"} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		if r.Code != http.StatusNotFound {
			t.Fatalf("development source exposed at %s", path)
		}
	}
}
