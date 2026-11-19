// Package router owns the public HTTP data plane. The daemon publishes snapshots
// and receives observations through hooks; dashboard/API state stays in server.
package router

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sammwyy/startship/internal/firewall"
	"github.com/sammwyy/startship/internal/routing"
)

type Target = routing.Target
type Snapshot struct {
	Asleep         map[string]bool
	Targets        []Target
	RouteWorkspace map[string]string
	Policies       map[string]*firewall.Engine
}
type Traffic struct {
	ProjectID, Method, Route string
	Status                   int
	Duration                 time.Duration
	Time                     time.Time
}
type Ban struct {
	WorkspaceID, IP  string
	Seconds          int
	RuleID, RuleName string
}
type Hooks struct {
	Wake      func(context.Context, string) error
	Activity  func(string) func()
	Challenge func(http.ResponseWriter, *http.Request) bool
	Dashboard func(http.ResponseWriter, *http.Request) bool
	Traffic   func(Traffic)
	Event     func(firewall.Event)
	Observe   func(workspace string, request *firewall.Request, decision firewall.Decision)
	Ban       func(Ban) error
}
type Options struct {
	TrustedProxies []netip.Prefix
	GeoIP          firewall.GeoIP
	HTTPSPort      string
}
type Router struct {
	snapshot atomic.Pointer[Snapshot]
	hooks    Hooks
	options  Options
}

func New(hooks Hooks, options Options) *Router {
	options.TrustedProxies = append([]netip.Prefix(nil), options.TrustedProxies...)
	r := &Router{hooks: hooks, options: options}
	r.Update(Snapshot{})
	return r
}

// Update takes ownership of copies of the routing slices and maps. Engines are immutable.
func (r *Router) Update(s Snapshot) {
	copy := Snapshot{Targets: append([]Target(nil), s.Targets...), RouteWorkspace: map[string]string{}, Policies: map[string]*firewall.Engine{}, Asleep: map[string]bool{}}
	for id, asleep := range s.Asleep {
		copy.Asleep[id] = asleep
	}
	for id, ws := range s.RouteWorkspace {
		copy.RouteWorkspace[id] = ws
	}
	for ws, e := range s.Policies {
		copy.Policies[ws] = e
	}
	r.snapshot.Store(&copy)
}
func (a *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a.hooks.Challenge != nil && a.hooks.Challenge(w, r) {
		return
	}
	if r.TLS != nil {
		r.Header.Set("X-Forwarded-Proto", "https")
	}
	if a.hooks.Dashboard != nil && a.hooks.Dashboard(w, r) {
		return
	}
	snapshot := a.snapshot.Load()
	if snapshot == nil {
		http.Error(w, "Startship: no route configured for this address.", 404)
		return
	}
	t := routing.MatchRoute(snapshot.Targets, r.Host, r.URL.Path)
	if t == nil {
		http.Error(w, "Startship: no route configured for this address.", 404)
		return
	}
	target := *t
	if target.Route.ForceHTTPS && r.TLS == nil {
		host := strings.Trim(routing.NormalizeHost(target.Route.Host), "[]")
		port := a.options.HTTPSPort
		if port != "" && port != "443" {
			host = net.JoinHostPort(host, port)
		} else if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		u := url.URL{Scheme: "https", Host: host, Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery}
		http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
		return
	}
	started := time.Now()
	observed := &responseWriter{ResponseWriter: w}
	w = observed
	defer func() {
		if a.hooks.Traffic != nil {
			a.hooks.Traffic(Traffic{target.Route.ProjectID, r.Method, target.Route.Host + target.Route.PathPrefix, observed.statusCode(), time.Since(started), time.Now()})
		}
	}()
	workspace := snapshot.RouteWorkspace[target.Route.ID]
	if engine := snapshot.Policies[workspace]; engine != nil {
		release, ok := a.enforce(w, r, target, workspace, engine)
		if !ok {
			return
		}
		defer release()
	}
	if a.hooks.Activity != nil {
		if done := a.hooks.Activity(target.Route.ProjectID); done != nil {
			defer done()
		}
	}
	snapshot = a.snapshot.Load()
	if snapshot.Asleep[target.Route.ProjectID] {
		start := time.Now()
		if a.hooks.Wake == nil || a.hooks.Wake(r.Context(), target.Route.ProjectID) != nil {
			http.Error(w, "Startship: project failed to wake.", 503)
			return
		}
		snapshot = a.snapshot.Load()
		t = routing.MatchRoute(snapshot.Targets, r.Host, r.URL.Path)
		if t == nil {
			http.Error(w, "Startship: no route configured for this address.", 404)
			return
		}
		target = *t
		w.Header().Set("X-Startship-Woke", fmt.Sprint(time.Since(start).Milliseconds()))
	}
	d := target.Deployment
	if d.Status != "running" {
		http.Error(w, "Startship: this container has no healthy deployment yet.", 502)
		return
	}
	if target.Route.StripPrefix {
		r = r.Clone(r.Context())
		u := *r.URL
		r.URL = &u
		r.URL.Path = routing.Stripped(r.URL.Path, target.Route.PathPrefix)
		r.URL.RawPath = ""
	}
	if d.StaticDir != "" {
		clean := path.Clean("/" + r.URL.Path)
		file := filepath.Join(d.StaticDir, filepath.FromSlash(clean))
		resolved, err := filepath.EvalSymlinks(file)
		if err == nil {
			rel, e := filepath.Rel(d.StaticDir, resolved)
			if e != nil || !safeRoot(rel) {
				http.Error(w, "Forbidden", 403)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		etag := sha256.Sum256([]byte(d.ID + ":" + clean))
		w.Header().Set("ETag", fmt.Sprintf(`"%x"`, etag))
		if _, err = os.Stat(file); os.IsNotExist(err) {
			http.ServeFile(w, r, filepath.Join(d.StaticDir, "index.html"))
			return
		}
		http.FileServer(http.Dir(d.StaticDir)).ServeHTTP(w, r)
		return
	}
	if d.IP == "" {
		http.Error(w, "Startship: app unavailable.", 502)
		return
	}
	upstream, _ := url.Parse("http://" + net.JoinHostPort(d.IP, fmt.Sprint(target.Port)))
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Rewrite = func(pr *httputil.ProxyRequest) { pr.SetURL(upstream); pr.Out.Host = pr.In.Host; pr.SetXForwarded() }
	proxy.Director = nil
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			blocked(w, 413, 0)
			return
		}
		http.Error(w, "Startship: upstream unavailable.", 502)
	}
	proxy.ServeHTTP(w, r)
}
func safeRoot(root string) bool {
	return !filepath.IsAbs(root) && root != ".." && !strings.HasPrefix(filepath.Clean(root), ".."+string(filepath.Separator))
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) statusCode() int {
	if w.status == 0 {
		return 200
	}
	return w.status
}
func (w *responseWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != 101 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *responseWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}
