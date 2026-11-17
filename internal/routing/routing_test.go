package routing

import (
	"github.com/sammwyy/startship/internal/store"
	"testing"
)

func TestRoutes(t *testing.T) {
	ts := []Target{{Route: store.Record{Host: "example.com", PathPrefix: "/"}}, {Route: store.Record{Host: "example.com", PathPrefix: "/api/"}}}
	if got := MatchRoute(ts, "EXAMPLE.COM:80", "/api/a"); got == nil || got.Route.PathPrefix != "/api/" {
		t.Fatal(got)
	}
	if MatchRoute(ts, "other.com", "/") != nil {
		t.Fatal("host mismatch")
	}
	if MatchRoute(nil, "", "") != nil {
		t.Fatal("empty")
	}
	if Stripped("/api/hello", "/api/") != "/hello" {
		t.Fatal("strip")
	}
}
func TestRoutePrefixes(t *testing.T) {
	for _, v := range []struct{ input, want string }{{"", "/"}, {"*", "/"}, {"api", "/api"}, {"api/*", "/api/"}, {"/api/*", "/api/"}, {"/api*", "/api"}, {"/api/", "/api/"}, {" /api ", "/api"}} {
		if got := NormalizePrefix(v.input); got != v.want {
			t.Errorf("NormalizePrefix(%q) = %q, want %q", v.input, got, v.want)
		}
	}
	for _, v := range []struct {
		prefix, path string
		want         bool
	}{{"/", "/apiary", true}, {"/api", "/api", true}, {"/api", "/api/", true}, {"/api", "/api/health", true}, {"/api", "/apiary", false}, {"/api", "/apiculture", false}, {"/api/", "/api", false}, {"/api/", "/api/health", true}} {
		if got := matchesPrefix(v.path, v.prefix); got != v.want {
			t.Errorf("matchesPrefix(%q, %q) = %v", v.path, v.prefix, got)
		}
	}
	ts := []Target{{Route: store.Record{Host: "example.com", PathPrefix: "/"}}, {Route: store.Record{Host: "example.com", PathPrefix: "/api"}}}
	for _, p := range []string{"/api", "/api/health", "/apiary"} {
		want := "/api"
		if p == "/apiary" {
			want = "/"
		}
		if got := MatchRoute(ts, "example.com", p); got == nil || got.Route.PathPrefix != want {
			t.Fatalf("route for %s: %+v", p, got)
		}
	}
	for _, v := range []struct{ path, prefix, want string }{{"/api", "/api", "/"}, {"/api/", "/api", "/"}, {"/api/health", "/api", "/health"}, {"/api/health", "/api/", "/health"}} {
		if got := Stripped(v.path, v.prefix); got != v.want {
			t.Errorf("Stripped(%q, %q) = %q", v.path, v.prefix, got)
		}
	}
}
