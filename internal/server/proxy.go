package server

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
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

func previewRouteURL(route Record) string {
	if route.CertificateID != "" {
		return certificateRouteURL(route)
	}
	_, port, _ := net.SplitHostPort(config.Env("STARTSHIP_HTTP_ADDR", ":80"))
	host := route.Host
	if port != "" && port != "80" {
		host = net.JoinHostPort(host, port)
	}
	return (&url.URL{Scheme: "http", Host: host, Path: route.PathPrefix}).String()
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
					host := routing.NormalizeHost(r.Host)
					if port := a.publicRouter().HTTPSPort(); port != "" && port != "443" {
						host = net.JoinHostPort(strings.Trim(host, "[]"), port)
					}
					http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusPermanentRedirect)
				} else {
					a.api().ServeHTTP(w, r)
				}
				return true
			},
			Wake:     a.wakeProject,
			Activity: a.projectRequest,
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
	addresses := map[string][]string{}
	for _, p := range a.store.List("projects", "") {
		asleep[p.ID] = p.Sleeping
	}
	for _, p := range a.store.List("previews", "") {
		asleep[p.ID] = p.Sleeping
		addresses[p.ID] = nil
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
			for _, preview := range a.previews(projectID) {
				release, e := a.store.Get(preview.Active)
				if e != nil || release.Status != "running" {
					continue
				}
				pr := r
				pr.ID, pr.ProjectID, pr.Host = r.ID+":"+preview.ID, preview.ID, previewHost(preview.HostSlug, r.Host)
				pr.CertificateID, pr.ForceHTTPS = "", false
				if !a.routeAllowed(ws, pr.Host) {
					continue
				}
				if c := a.storedCertificate(pr.Host); c != nil {
					pr.CertificateID, pr.ForceHTTPS = c.ID, r.ForceHTTPS
				} else if a.acmeSettings().Email != "" && (r.CertificateID != "" || r.ForceHTTPS || a.https) {
					go a.ensurePreviewCertificate(preview.ID, ws, pr.Host)
				}
				for _, id := range release.ContainerReleases {
					pd, e := a.store.Get(id)
					if e == nil && pd.AppID == app.ID && pd.Status == "running" {
						targets = append(targets, Target{Route: pr, Deployment: pd, Port: port})
						routeWorkspace[pr.ID] = ws
						address := previewRouteURL(pr)
						if !slices.Contains(addresses[preview.ID], address) {
							addresses[preview.ID] = append(addresses[preview.ID], address)
						}
					}
				}
			}
		}
	}
	for _, t := range a.recipeAppTargets() {
		targets = append(targets, t)
		routeWorkspace[t.Route.ID] = t.Route.WorkspaceID
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
	for id, values := range addresses {
		p, err := a.store.Get(id)
		if err != nil || slices.Equal(p.Addresses, values) {
			continue
		}
		body, _ := json.Marshal(values)
		if _, err := a.store.DB.Exec("UPDATE records SET body=json_set(body,'$.addresses',json(?)) WHERE id=?", string(body), id); err != nil {
			return err
		}
		if p, err = a.store.Get(id); err == nil {
			a.store.Notify(p)
		}
	}
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
	for _, r := range a.publicRouteRecords() {
		if routing.NormalizeHost(r.Host) == host {
			return true
		}
	}
	return false
}
func (a *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.publicRouter().ServeHTTP(w, r) }
