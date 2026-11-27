package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sammwyy/startship/internal/config"
)

type listenerKey struct {
	group int
	addr  string
}

type boundServer struct {
	server   *http.Server
	listener net.Listener
	name     string
}

func bindServer(srv *http.Server, group int) (*boundServer, error) {
	names := [...]string{"dashboard", "proxy", "HTTPS proxy"}
	variables := [...]string{"STARTSHIP_DASHBOARD_ADDR", "STARTSHIP_HTTP_ADDR", "STARTSHIP_HTTPS_ADDR"}
	fallbacks := [...]string{":18080", ":8081", ":8443"}
	l, err := listen(names[group], srv.Addr, variables[group], fallbacks[group])
	if err != nil {
		return nil, err
	}
	return &boundServer{server: srv, listener: l, name: names[group]}, nil
}

func (b *boundServer) start() {
	go func() {
		var err error
		if b.server.TLSConfig != nil {
			err = b.server.ServeTLS(b.listener, "", "")
		} else {
			err = b.server.Serve(b.listener)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			log.Fatal(err)
		}
	}()
	log.Printf("Startship %s listening on %s", b.name, b.listener.Addr())
}

func (b *boundServer) drain() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.server.Shutdown(ctx); err != nil {
		b.server.Close()
	}
}

type routerListeners struct {
	mu          sync.Mutex
	settings    config.Daemon
	httpHandler http.Handler
	active      map[listenerKey]*boundServer
	stopped     bool
}

// prepareRouterListeners retains unchanged endpoints and binds all others before any old listener is closed.
func (a *Server) prepareRouterListeners(settings config.Daemon) (map[listenerKey]*boundServer, []*boundServer, error) {
	next := map[listenerKey]*boundServer{}
	if settings.RouterDisabled {
		return next, nil, nil
	}
	var opened []*boundServer
	for i, entries := range [][]string{settings.HTTPListen, settings.HTTPSListen} {
		group := i + 1
		addresses, err := config.Resolve(entries)
		if err != nil {
			closeBoundServers(opened)
			return nil, nil, err
		}
		for _, addr := range addresses {
			key := listenerKey{group, addr}
			if old := a.routerListeners.active[key]; old != nil {
				next[key] = old
				continue
			}
			srv := &http.Server{Addr: addr, Handler: a.routerListeners.httpHandler, ReadHeaderTimeout: 10 * time.Second}
			if group == 2 {
				srv.Handler = a
				srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: a.tlsCertificate}
			}
			b, err := bindServer(srv, group)
			if err != nil {
				closeBoundServers(opened)
				return nil, nil, err
			}
			next[key] = b
			opened = append(opened, b)
		}
	}
	return next, opened, nil
}

func closeBoundServers(servers []*boundServer) {
	for _, b := range servers {
		b.listener.Close()
	}
}

func (a *Server) setRouterPorts(settings config.Daemon) {
	port := ""
	if len(settings.HTTPSListen) > 0 {
		_, port, _ = net.SplitHostPort(settings.HTTPSListen[0])
	}
	r := a.publicRouter()
	a.routerOptions.HTTPSPort = port
	r.SetHTTPSPort(port)
	if settings.RouterDisabled {
		a.reserveServicePorts(settings.DashboardListen)
	} else {
		a.reserveServicePorts(settings.DashboardListen, settings.HTTPListen, settings.HTTPSListen)
	}
}

func (a *Server) commitRouterListeners(settings config.Daemon, next map[listenerKey]*boundServer) {
	rt := &a.routerListeners
	old := rt.active
	rt.settings, rt.active = settings, next
	a.setRouterPorts(settings)
	for key, b := range next {
		if old[key] != b {
			b.start()
		}
	}
	for key, b := range old {
		if next[key] != b {
			b.listener.Close()
			go b.drain()
		}
	}
}

func (a *Server) stopRouterListeners() {
	rt := &a.routerListeners
	rt.mu.Lock()
	rt.stopped = true
	active := rt.active
	rt.active = nil
	rt.mu.Unlock()
	var wg sync.WaitGroup
	for _, b := range active {
		b.listener.Close()
		wg.Go(b.drain)
	}
	wg.Wait()
}

var routerSettingKeys = [...]string{"http_listen", "https_listen", "router_disabled"}

type routerConfig struct {
	Enabled     bool     `json:"enabled"`
	HTTPListen  []string `json:"http_listen"`
	HTTPSListen []string `json:"https_listen"`
	ConfigPath  string   `json:"config_path"`
	Overridden  []string `json:"overridden"`
}

func (a *Server) routerConfig() routerConfig {
	rt := &a.routerListeners
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return routerConfigFrom(rt.settings)
}

func routerConfigFrom(settings config.Daemon) routerConfig {
	overridden := []string{}
	for _, key := range routerSettingKeys {
		if settings.Overridden[key] {
			overridden = append(overridden, key)
		}
	}
	return routerConfig{
		Enabled: !settings.RouterDisabled, HTTPListen: append([]string{}, settings.HTTPListen...),
		HTTPSListen: append([]string{}, settings.HTTPSListen...), ConfigPath: settings.ConfigPath, Overridden: overridden,
	}
}

func (a *Server) routerSettingsAPI(w http.ResponseWriter, r *http.Request, uid string, x *access) {
	switch r.Method {
	case http.MethodGet:
		reply(w, http.StatusOK, a.routerConfig())
	case http.MethodPut:
		if !x.admin {
			fail(w, http.StatusForbidden, "only Platform Admins can configure the router")
			return
		}
		var input struct {
			Enabled     *bool    `json:"enabled"`
			HTTPListen  []string `json:"http_listen"`
			HTTPSListen []string `json:"https_listen"`
		}
		if err := decode(w, r, &input); err != nil || input.Enabled == nil || input.HTTPListen == nil || input.HTTPSListen == nil {
			fail(w, http.StatusBadRequest, "enabled, http_listen and https_listen are required")
			return
		}
		a.updateRouterSettings(w, r, uid, *input.Enabled, input.HTTPListen, input.HTTPSListen)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *Server) updateRouterSettings(w http.ResponseWriter, r *http.Request, uid string, enabled bool, httpListen, httpsListen []string) {
	rt := &a.routerListeners
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.stopped {
		fail(w, http.StatusServiceUnavailable, "daemon is shutting down")
		return
	}
	current := rt.settings
	next := current
	next.RouterDisabled, next.HTTPListen, next.HTTPSListen = !enabled, httpListen, httpsListen
	next.DashboardListen = slices.Clone(current.DashboardListen)
	next.TrustedProxies = slices.Clone(current.TrustedProxies)
	if err := next.Validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	changes := map[string][]string{}
	if !slices.Equal(next.HTTPListen, current.HTTPListen) {
		changes["http_listen"] = next.HTTPListen
	}
	if !slices.Equal(next.HTTPSListen, current.HTTPSListen) {
		changes["https_listen"] = next.HTTPSListen
	}
	if next.RouterDisabled != current.RouterDisabled {
		changes["router_disabled"] = []string{strconv.FormatBool(next.RouterDisabled)}
	}
	if len(changes) == 0 {
		reply(w, http.StatusOK, routerConfigFrom(current))
		return
	}
	for _, key := range routerSettingKeys {
		if _, changed := changes[key]; changed && current.Overridden[key] {
			fail(w, http.StatusConflict, fmt.Sprintf("%s is overridden by %s or --%s; change the daemon startup configuration", key, config.EnvironmentKey(key), strings.ReplaceAll(key, "_", "-")))
			return
		}
	}
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()
	if enabled || changes["http_listen"] != nil || changes["https_listen"] != nil {
		if err := a.checkRouterPortConflicts(next); err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
	}
	listeners, opened, err := a.prepareRouterListeners(next)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	err = a.withNetworkRouterPorts(next, func() error {
		// Persist after all binds succeed, but before replacing the old listener set.
		if err := config.EditSettings(current.ConfigPath, changes); err != nil {
			return fmt.Errorf("cannot save router configuration: %w", err)
		}
		if err := next.ExportRouter(); err != nil {
			log.Printf("export router configuration: %v", err)
		}
		a.commitRouterListeners(next, listeners)
		return nil
	})
	if err != nil {
		closeBoundServers(opened)
		log.Printf("apply router configuration: %v", err)
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.audit(r.Context(), uid, "system.router", "router", scopePlatform, "", map[string]string{
		"enabled": strconv.FormatBool(enabled), "http_listen": strings.Join(httpListen, ","), "https_listen": strings.Join(httpsListen, ","),
	})
	a.live.publish(liveEvent{id: "system_router", changeKey: "system_router"})
	reply(w, http.StatusOK, routerConfigFrom(next))
}

func (a *Server) checkRouterPortConflicts(settings config.Daemon) error {
	dashboardPorts := listenPorts(settings.DashboardListen)
	routerPorts := listenPorts(settings.HTTPListen, settings.HTTPSListen)
	for port := range routerPorts {
		if dashboardPorts[port] {
			return fmt.Errorf("router port %d is reserved by dashboard_listen", port)
		}
	}
	for _, service := range listResources[Service](a, "services", "") {
		if routerPorts[service.ExternalPort] {
			return fmt.Errorf("router port %d is used by a service external_port", service.ExternalPort)
		}
	}
	for _, app := range listResources[recipeApp](a, "recipe_apps", "") {
		for _, port := range app.Ports {
			if routerPorts[port] {
				return fmt.Errorf("router port %d is used by an app", port)
			}
		}
	}
	return nil
}
