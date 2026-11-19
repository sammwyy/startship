package server

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/sammwyy/startship/internal/config"
	"github.com/sammwyy/startship/internal/firewall"
	"github.com/sammwyy/startship/internal/router"
	"github.com/sammwyy/startship/internal/routing"
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
func (a *Server) publicRouter() *router.Router {
	a.routerOnce.Do(func() {
		options := a.routerOptions
		if options.HTTPSPort == "" {
			_, options.HTTPSPort, _ = net.SplitHostPort(config.Env("STARTSHIP_HTTPS_ADDR", ":443"))
		}
		a.router = router.New(router.Hooks{
			Challenge: a.serveChallenge,
			Dashboard: func(w http.ResponseWriter, r *http.Request) bool {
				if a.dashboardDomain == "" || routing.NormalizeHost(r.Host) != routing.NormalizeHost(a.dashboardDomain) {
					return false
				}
				if a.https && r.TLS == nil {
					http.Redirect(w, r, "https://"+routing.NormalizeHost(r.Host)+r.URL.RequestURI(), http.StatusPermanentRedirect)
				} else {
					a.api().ServeHTTP(w, r)
				}
				return true
			},
			Wake:     a.wakeProject,
			Activity: a.projectActivity,
			Traffic: func(t router.Traffic) {
				a.projectActivity(t.ProjectID)
				a.traffic.record(t.ProjectID, t.Method, t.Route, t.Status, t.Duration, t.Time)
			},
			Event:   a.recordFirewallEvent,
			Observe: a.observeFirewall,
			Ban:     a.recordFirewallBan,
		}, options)
	})
	return a.router
}
func (a *Server) refresh() {
	a.routerRefresh.Lock()
	defer a.routerRefresh.Unlock()
	if err := a.refreshLocked(); err != nil {
		log.Printf("firewall: refresh: %v", err)
	}
}
func (a *Server) refreshLocked() error {
	targets := []Target{}
	routeWorkspace := map[string]string{}
	asleep := map[string]bool{}
	for _, p := range a.store.List("projects", "") {
		asleep[p.ID] = p.Sleeping
	}
	for _, r := range a.store.List("routes", "") {
		app, err := a.store.Get(r.AppID)
		if err != nil {
			continue
		}
		d, _ := a.store.Get(app.Active)
		port := r.Port
		if port == 0 {
			port = app.Port
		}
		targets = append(targets, Target{Route: r, Deployment: d, Port: port})
		projectID := r.ProjectID
		if projectID == "" {
			projectID = app.ProjectID
		}
		if projectID == "" {
			projectID = app.Parent
		}
		project, err := a.store.Get(projectID)
		if err == nil {
			ws := project.WorkspaceID
			if ws == "" {
				ws = project.Parent
			}
			routeWorkspace[r.ID] = ws
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Route.ID < targets[j].Route.ID })
	policies := map[string]*firewall.Engine{}
	for _, ws := range routeWorkspace {
		if _, ok := policies[ws]; ok {
			continue
		}
		p, lists, err := a.loadFirewall(ws)
		if err != nil {
			return err
		}
		a.firewall.mu.Lock()
		if a.firewall.states == nil {
			a.firewall.states = map[string]*firewall.State{}
		}
		if a.firewall.states[ws] == nil {
			a.firewall.states[ws] = firewall.NewState()
		}
		state := a.firewall.states[ws]
		a.firewall.mu.Unlock()
		engine, err := firewall.Compile(p, lists, state)
		if err != nil {
			return err
		}
		policies[ws] = engine
	}
	a.publicRouter().Update(router.Snapshot{Targets: targets, RouteWorkspace: routeWorkspace, Policies: policies, Asleep: asleep})
	a.firewall.mu.Lock()
	for ws := range policies {
		a.firewall.states[ws].ClearBans()
	}
	a.firewall.mu.Unlock()
	return nil
}
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
func (a *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.publicRouter().ServeHTTP(w, r) }
