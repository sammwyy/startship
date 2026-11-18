package server

import (
	"crypto/sha256"
	"fmt"
	"github.com/sammwyy/startship/internal/config"
	"github.com/sammwyy/startship/internal/routing"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Target = routing.Target

func certificateRouteURL(route Record) string {
	host := strings.Trim(routing.NormalizeHost(route.Host), "[]")
	_, port, _ := net.SplitHostPort(config.Env("STARTSHIP_HTTPS_ADDR", ":443"))
	if port != "" && port != "443" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return (&url.URL{Scheme: "https", Host: host, Path: route.PathPrefix}).String()
}

func routeURL(route Record) string {
	scheme, addr, defaultPort := "http", config.Env("STARTSHIP_HTTP_ADDR", ":80"), "80"
	if os.Getenv("STARTSHIP_ACME_EMAIL") != "" {
		scheme, addr, defaultPort = "https", config.Env("STARTSHIP_HTTPS_ADDR", ":443"), "443"
	}
	_, port, _ := net.SplitHostPort(addr)
	host := strings.Trim(routing.NormalizeHost(route.Host), "[]")
	if port != "" && port != defaultPort {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return (&url.URL{Scheme: scheme, Host: host, Path: route.PathPrefix}).String()
}

func (a *Server) refresh() {
	targets := []Target{}
	for _, r := range a.store.List("routes", "") {
		app, e := a.store.Get(r.AppID)
		if e != nil {
			continue
		}
		d, _ := a.store.Get(app.Active)
		port := r.Port
		if port == 0 {
			port = app.Port
		}
		targets = append(targets, Target{Route: r, Deployment: d, Port: port})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Route.ID < targets[j].Route.ID })
	a.routeMu.Lock()
	a.targets = targets
	a.routeMu.Unlock()
}

// certAllowed reports whether ACME may issue a certificate for host: the
// dashboard domain or any routed host.
func (a *Server) certAllowed(host string) bool {
	host = routing.NormalizeHost(host)
	if host == "" {
		return false
	}
	if a.dashboardDomain != "" && host == routing.NormalizeHost(a.dashboardDomain) {
		return true
	}
	for _, r := range a.store.List("routes", "") {
		if routing.NormalizeHost(r.Host) == host {
			return true
		}
	}
	return false
}
func (a *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a.serveChallenge(w, r) {
		return
	}
	if r.TLS != nil {
		r.Header.Set("X-Forwarded-Proto", "https")
	}
	if a.dashboardDomain != "" && routing.NormalizeHost(r.Host) == routing.NormalizeHost(a.dashboardDomain) {
		if a.https && r.TLS == nil {
			http.Redirect(w, r, "https://"+routing.NormalizeHost(r.Host)+r.URL.RequestURI(), http.StatusPermanentRedirect)
			return
		}
		a.api().ServeHTTP(w, r)
		return
	}
	a.routeMu.RLock()
	t := routing.MatchRoute(a.targets, r.Host, r.URL.Path)
	var target Target
	if t != nil {
		target = *t
	}
	a.routeMu.RUnlock()
	if t == nil {
		http.Error(w, "Startship: no route configured for this address.", 404)
		return
	}
	if target.Route.ForceHTTPS && r.TLS == nil {
		u, _ := url.Parse(certificateRouteURL(target.Route))
		u.Path = r.URL.Path
		u.RawPath = r.URL.RawPath
		u.RawQuery = r.URL.RawQuery
		http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
		return
	}
	started := time.Now()
	observed := &trafficWriter{ResponseWriter: w}
	w = observed
	defer func() {
		a.traffic.record(target.Route.ProjectID, r.Method, target.Route.Host+target.Route.PathPrefix, observed.statusCode(), time.Since(started), time.Now())
	}()
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
		resolved, e := filepath.EvalSymlinks(file)
		if e == nil {
			rel, e := filepath.Rel(d.StaticDir, resolved)
			if e != nil || !safeRoot(rel) {
				http.Error(w, "Forbidden", 403)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		etag := sha256.Sum256([]byte(d.ID + ":" + clean))
		w.Header().Set("ETag", fmt.Sprintf(`"%x"`, etag))
		if _, e = os.Stat(file); os.IsNotExist(e) {
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
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		http.Error(w, "Startship: upstream unavailable.", 502)
	}
	proxy.ServeHTTP(w, r)
}
