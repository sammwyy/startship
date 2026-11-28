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
	mu               sync.Mutex
	settings         config.Daemon
	dashboardHandler http.Handler
	httpHandler      http.Handler
	active           map[listenerKey]*boundServer
	stopped          bool
}

func listenersOverlap(a, b string) bool {
	hostA, portA, errA := net.SplitHostPort(a)
	hostB, portB, errB := net.SplitHostPort(b)
	if errA != nil || errB != nil || portA != portB || portA == "" || portA == "0" {
		return false
	}
	if hostA == "" || hostB == "" {
		return true
	}
	ipA := net.ParseIP(hostA)
	ipB := net.ParseIP(hostB)
	if ipA == nil || ipB == nil {
		return hostA == hostB
	}
	return ipA.IsUnspecified() || ipB.IsUnspecified() || ipA.Equal(ipB)
}

func (a *Server) rollbackReleasedListeners(released map[listenerKey]*boundServer) {
	for key, old := range released {
		srv := &http.Server{
			Addr:              old.server.Addr,
			Handler:           old.server.Handler,
			ReadHeaderTimeout: old.server.ReadHeaderTimeout,
		}
		if key.group == 2 {
			srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: a.tlsCertificate}
		}
		b, err := bindServer(srv, key.group)
		if err != nil {
			log.Printf("rollback listener %s: %v", key.addr, err)
			delete(a.routerListeners.active, key)
			continue
		}
		b.start()
		a.routerListeners.active[key] = b
		go old.drain()
	}
}

type preparedListeners struct {
	next     map[listenerKey]*boundServer
	opened   []*boundServer
	released map[listenerKey]*boundServer
}

func (a *Server) rollbackPrepared(p preparedListeners) {
	closeBoundServers(p.opened)
	a.rollbackReleasedListeners(p.released)
}

func (a *Server) prepareListenerSpecs(specs []listenerKey, sameGroup func(int) bool, makeServer func(listenerKey) *http.Server) (preparedListeners, error) {
	next := map[listenerKey]*boundServer{}
	desired := map[listenerKey]bool{}
	for _, key := range specs {
		desired[key] = true
	}
	removable := map[listenerKey]*boundServer{}
	for key, b := range a.routerListeners.active {
		if sameGroup(key.group) && !desired[key] {
			removable[key] = b
		}
	}

	var nonOverlapping, overlapping []listenerKey
	for _, key := range specs {
		if old := a.routerListeners.active[key]; old != nil {
			next[key] = old
			continue
		}
		overlaps := false
		for remKey := range removable {
			if listenersOverlap(remKey.addr, key.addr) {
				overlaps = true
				break
			}
		}
		if overlaps {
			overlapping = append(overlapping, key)
		} else {
			nonOverlapping = append(nonOverlapping, key)
		}
	}

	var opened []*boundServer
	released := map[listenerKey]*boundServer{}

	// Bind non-overlapping listeners first so failures leave existing listeners untouched.
	for _, key := range nonOverlapping {
		b, err := bindServer(makeServer(key), key.group)
		if err != nil {
			closeBoundServers(opened)
			return preparedListeners{}, err
		}
		next[key] = b
		opened = append(opened, b)
	}

	// Release only the old listeners whose port/address overlaps a new listener, then bind.
	for _, key := range overlapping {
		for remKey, rem := range removable {
			if released[remKey] == nil && listenersOverlap(remKey.addr, key.addr) {
				rem.listener.Close()
				released[remKey] = rem
			}
		}
		b, err := bindServer(makeServer(key), key.group)
		if err != nil {
			closeBoundServers(opened)
			a.rollbackReleasedListeners(released)
			return preparedListeners{}, err
		}
		next[key] = b
		opened = append(opened, b)
	}

	return preparedListeners{next: next, opened: opened, released: released}, nil
}

// prepareDashboardListeners retains unchanged dashboard endpoints and binds all others,
// releasing overlapping old dashboard listeners first when switching interfaces on the same port.
func (a *Server) prepareDashboardListeners(settings config.Daemon) (preparedListeners, error) {
	addresses, err := config.Resolve(settings.DashboardListen)
	if err != nil {
		return preparedListeners{}, err
	}
	specs := make([]listenerKey, 0, len(addresses))
	for _, addr := range addresses {
		specs = append(specs, listenerKey{0, addr})
	}
	return a.prepareListenerSpecs(
		specs,
		func(group int) bool { return group == 0 },
		func(key listenerKey) *http.Server {
			return &http.Server{Addr: key.addr, Handler: a.routerListeners.dashboardHandler, ReadHeaderTimeout: 10 * time.Second}
		},
	)
}

// prepareRouterListeners retains unchanged endpoints and binds all others,
// releasing overlapping old router listeners first when switching interfaces on the same port.
func (a *Server) prepareRouterListeners(settings config.Daemon) (preparedListeners, error) {
	if settings.RouterDisabled {
		return preparedListeners{next: map[listenerKey]*boundServer{}}, nil
	}
	var specs []listenerKey
	for i, entries := range [][]string{settings.HTTPListen, settings.HTTPSListen} {
		group := i + 1
		addresses, err := config.Resolve(entries)
		if err != nil {
			return preparedListeners{}, err
		}
		for _, addr := range addresses {
			specs = append(specs, listenerKey{group, addr})
		}
	}
	return a.prepareListenerSpecs(
		specs,
		func(group int) bool { return group != 0 },
		func(key listenerKey) *http.Server {
			srv := &http.Server{Addr: key.addr, Handler: a.routerListeners.httpHandler, ReadHeaderTimeout: 10 * time.Second}
			if key.group == 2 {
				srv.Handler = a
				srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: a.tlsCertificate}
			}
			return srv
		},
	)
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
	merged := map[listenerKey]*boundServer{}
	for key, b := range old {
		if key.group == 0 {
			merged[key] = b
		}
	}
	for key, b := range next {
		merged[key] = b
	}
	rt.settings, rt.active = settings, merged
	a.setRouterPorts(settings)
	for key, b := range next {
		if old[key] != b {
			b.start()
		}
	}
	for key, b := range old {
		if key.group != 0 && merged[key] != b {
			b.listener.Close()
			go b.drain()
		}
	}
}

func (a *Server) commitDashboardListeners(settings config.Daemon, next map[listenerKey]*boundServer) []*boundServer {
	rt := &a.routerListeners
	old := rt.active
	merged := map[listenerKey]*boundServer{}
	for key, b := range old {
		if key.group != 0 {
			merged[key] = b
		}
	}
	for key, b := range next {
		merged[key] = b
	}
	rt.settings, rt.active = settings, merged
	a.setRouterPorts(settings)
	for key, b := range next {
		if old[key] != b {
			b.start()
		}
	}
	var removed []*boundServer
	for key, b := range old {
		if key.group == 0 && merged[key] != b {
			removed = append(removed, b)
		}
	}
	return removed
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
	if settings.RouterDisabledOverridden {
		overridden = append(overridden, "router_disabled")
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
	if next.RouterDisabled != current.RouterDisabled && current.RouterDisabledOverridden {
		fail(w, http.StatusConflict, "router_disabled is overridden by STARTSHIP_ROUTER_DISABLED or --router-disabled; change the daemon startup configuration")
		return
	}
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()
	if enabled || changes["http_listen"] != nil || changes["https_listen"] != nil {
		if err := a.checkRouterPortConflicts(next); err != nil {
			fail(w, http.StatusConflict, err.Error())
			return
		}
	}
	prepared, err := a.prepareRouterListeners(next)
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
		a.commitRouterListeners(next, prepared.next)
		return nil
	})
	if err != nil {
		a.rollbackPrepared(prepared)
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

type dashboardConfig struct {
	DashboardListen []string `json:"dashboard_listen"`
	ConfigPath      string   `json:"config_path"`
}

func (a *Server) dashboardConfig() dashboardConfig {
	rt := &a.routerListeners
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return dashboardConfigFrom(rt.settings)
}

func dashboardConfigFrom(settings config.Daemon) dashboardConfig {
	return dashboardConfig{
		DashboardListen: append([]string{}, settings.DashboardListen...),
		ConfigPath:      settings.ConfigPath,
	}
}

func (a *Server) dashboardSettingsAPI(w http.ResponseWriter, r *http.Request, uid string, x *access) {
	switch r.Method {
	case http.MethodGet:
		reply(w, http.StatusOK, a.dashboardConfig())
	case http.MethodPut:
		if !x.admin {
			fail(w, http.StatusForbidden, "only Platform Admins can configure the dashboard")
			return
		}
		var input struct {
			DashboardListen []string `json:"dashboard_listen"`
		}
		if err := decode(w, r, &input); err != nil || input.DashboardListen == nil {
			fail(w, http.StatusBadRequest, "dashboard_listen is required")
			return
		}
		a.updateDashboardSettings(w, r, uid, input.DashboardListen)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *Server) updateDashboardSettings(w http.ResponseWriter, r *http.Request, uid string, dashboardListen []string) {
	rt := &a.routerListeners
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.stopped {
		fail(w, http.StatusServiceUnavailable, "daemon is shutting down")
		return
	}
	current := rt.settings
	next := current
	next.DashboardListen = dashboardListen
	next.HTTPListen = slices.Clone(current.HTTPListen)
	next.HTTPSListen = slices.Clone(current.HTTPSListen)
	next.TrustedProxies = slices.Clone(current.TrustedProxies)
	if err := next.Validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if slices.Equal(next.DashboardListen, current.DashboardListen) {
		reply(w, http.StatusOK, dashboardConfigFrom(current))
		return
	}
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()
	if err := a.checkDashboardPortConflicts(next); err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	prepared, err := a.prepareDashboardListeners(next)
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	changes := map[string][]string{"dashboard_listen": next.DashboardListen}
	if err := config.EditSettings(current.ConfigPath, changes); err != nil {
		a.rollbackPrepared(prepared)
		log.Printf("apply dashboard configuration: %v", err)
		fail(w, http.StatusInternalServerError, fmt.Errorf("cannot save dashboard configuration: %w", err).Error())
		return
	}
	if err := next.ExportDashboard(); err != nil {
		log.Printf("export dashboard configuration: %v", err)
	}
	removed := a.commitDashboardListeners(next, prepared.next)
	a.audit(r.Context(), uid, "system.dashboard", "dashboard", scopePlatform, "", map[string]string{
		"dashboard_listen": strings.Join(next.DashboardListen, ","),
	})
	a.live.publish(liveEvent{id: "system_dashboard", changeKey: "system_dashboard"})
	reply(w, http.StatusOK, dashboardConfigFrom(next))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	for _, b := range removed {
		b.listener.Close()
		go b.drain()
	}
}

func (a *Server) checkDashboardPortConflicts(settings config.Daemon) error {
	dashboardPorts := listenPorts(settings.DashboardListen)
	if !settings.RouterDisabled {
		routerPorts := listenPorts(settings.HTTPListen, settings.HTTPSListen)
		for port := range dashboardPorts {
			if routerPorts[port] {
				return fmt.Errorf("dashboard port %d is reserved by the public router", port)
			}
		}
	}
	for _, service := range listResources[Service](a, "services", "") {
		if dashboardPorts[service.ExternalPort] {
			return fmt.Errorf("dashboard port %d is used by a service external_port", service.ExternalPort)
		}
	}
	for _, app := range listResources[recipeApp](a, "recipe_apps", "") {
		for _, port := range app.Ports {
			if dashboardPorts[port] {
				return fmt.Errorf("dashboard port %d is used by an app", port)
			}
		}
	}
	return nil
}

func (a *Server) hostInterfaces() ([]config.NetworkInterface, error) {
	if a.listInterfaces != nil {
		return a.listInterfaces()
	}
	return config.NetworkInterfaces()
}

func (a *Server) interfacesAPI(w http.ResponseWriter, r *http.Request, _ string, _ *access) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ifaces, err := a.hostInterfaces()
	if err != nil {
		fail(w, http.StatusInternalServerError, fmt.Sprintf("cannot list network interfaces: %v", err))
		return
	}
	reply(w, http.StatusOK, map[string]any{"interfaces": ifaces})
}
