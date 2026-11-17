package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type Target struct {
	Route      Record
	Deployment Record
	Port       int
}

func routeURL(route Record) string {
	scheme, addr, defaultPort := "http", env("STARTSHIP_HTTP_ADDR", ":80"), "80"
	if os.Getenv("STARTSHIP_ACME_EMAIL") != "" {
		scheme, addr, defaultPort = "https", env("STARTSHIP_HTTPS_ADDR", ":443"), "443"
	}
	_, port, _ := net.SplitHostPort(addr)
	host := strings.Trim(normalizeHost(route.Host), "[]")
	if port != "" && port != defaultPort {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return (&url.URL{Scheme: scheme, Host: host, Path: route.PathPrefix}).String()
}

func normalizeHost(h string) string {
	if host, _, e := net.SplitHostPort(h); e == nil {
		h = host
	}
	return strings.TrimSuffix(strings.ToLower(h), ".")
}
func matchRoute(targets []Target, host, p string) *Target {
	host = normalizeHost(host)
	var best *Target
	for i := range targets {
		t := &targets[i]
		if normalizeHost(t.Route.Host) == host && strings.HasPrefix(p, t.Route.PathPrefix) && (best == nil || len(t.Route.PathPrefix) > len(best.Route.PathPrefix)) {
			best = t
		}
	}
	return best
}
func stripped(p, prefix string) string {
	p = strings.TrimPrefix(p, prefix)
	return "/" + strings.TrimPrefix(p, "/")
}
func (a *Server) refresh() {
	targets := []Target{}
	for _, r := range a.store.list("routes", "") {
		app, e := a.store.get(r.AppID)
		if e != nil {
			continue
		}
		d, _ := a.store.get(app.Active)
		targets = append(targets, Target{r, d, app.Port})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Route.ID < targets[j].Route.ID })
	a.routeMu.Lock()
	a.targets = targets
	a.routeMu.Unlock()
}
func (a *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if domain := os.Getenv("STARTSHIP_DASHBOARD_DOMAIN"); domain != "" && normalizeHost(r.Host) == normalizeHost(domain) {
		a.api().ServeHTTP(w, r)
		return
	}
	a.routeMu.RLock()
	t := matchRoute(a.targets, r.Host, r.URL.Path)
	var target Target
	if t != nil {
		target = *t
	}
	a.routeMu.RUnlock()
	if t == nil {
		http.Error(w, "Startship: no route configured for this address.", 404)
		return
	}
	d := target.Deployment
	if d.Status != "running" {
		http.Error(w, "Startship: this app has no healthy deployment yet.", 502)
		return
	}
	if target.Route.StripPrefix {
		r = r.Clone(r.Context())
		u := *r.URL
		r.URL = &u
		r.URL.Path = stripped(r.URL.Path, target.Route.PathPrefix)
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
