package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/sammwyy/startship/internal/config"
	"github.com/sammwyy/startship/internal/firewall"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func listen(name, addr, variable, fallback string) (net.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, listenError(name, addr, variable, fallback, err)
	}
	return l, nil
}
func listenError(name, addr, variable, fallback string, err error) error {
	switch {
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("%s: cannot listen on %s (%w); set %s=%s or run with CAP_NET_BIND_SERVICE for privileged ports", name, addr, err, variable, fallback)
	case errors.Is(err, syscall.EADDRINUSE):
		return fmt.Errorf("%s: cannot listen on %s (%w); pick a free port or stop the process using it", name, addr, err)
	default:
		return fmt.Errorf("%s: cannot listen on %s (%w)", name, addr, err)
	}
}
func serve(servers []*http.Server, groups ...int) error {
	listeners := make([]*boundServer, 0, len(servers))
	for i, srv := range servers {
		group := i
		if len(groups) > 0 {
			group = groups[i]
		}
		l, err := bindServer(srv, group)
		if err != nil {
			for _, opened := range listeners {
				opened.listener.Close()
			}
			return err
		}
		listeners = append(listeners, l)
	}
	for _, l := range listeners {
		l.start()
	}
	return nil
}
func Run(settings config.Daemon) {
	data, e := filepath.Abs(config.DataDirectory())
	if e != nil {
		log.Fatal(e)
	}
	if e = os.MkdirAll(data, 0700); e != nil {
		log.Fatal(e)
	}
	s, e := openStore(filepath.Join(data, "startship.db"))
	if e != nil {
		log.Fatal(e)
	}
	defer s.DB.Close()
	a := &Server{store: s, data: data, queues: make(map[string]chan string), dashboardDomain: os.Getenv("STARTSHIP_DASHBOARD_DOMAIN")}
	a.routerOptions.TrustedProxies, e = firewall.ParseTrustedProxies(settings.TrustedProxies)
	if e != nil {
		log.Fatal(e)
	}
	geo, e := firewall.OpenMMDB(settings.GeoIPDatabase)
	if e != nil {
		log.Fatal(e)
	}
	defer geo.Close()
	if settings.GeoIPDatabase != "" {
		a.routerOptions.GeoIP = geo
	}
	a.routerListeners.settings = settings
	a.setRouterPorts(settings)
	proxyHandler := a.configureRouterProxy(settings)
	a.routerListeners.httpHandler = proxyHandler
	a.initLive()
	a.initCatalogs()
	if e = a.bootstrapAdmin(os.Getenv("STARTSHIP_ADMIN_USER"), os.Getenv("STARTSHIP_ADMIN_PASSWORD")); e != nil {
		log.Fatal(e)
	}
	a.restore()
	a.reconcileServices()
	metricsContext, stopMetrics := context.WithCancel(context.Background())
	defer stopMetrics()
	a.initNetwork(settings)
	go a.watchNetwork(metricsContext)
	go a.sampleTrafficResources(metricsContext)
	go a.watchRecipeApps(metricsContext)
	go a.watchUpdates(metricsContext)
	go a.watchCertificates(metricsContext)
	go a.scheduleBackups(metricsContext)
	go a.watchIdle(metricsContext)
	go a.watchUptime(metricsContext)
	a.routerListeners.dashboardHandler = a.api()
	nextRouter, e := a.prepareRouterListeners(settings)
	if e != nil {
		log.Fatal(e)
	}
	a.commitRouterListeners(settings, nextRouter.next)
	if settings.RouterDisabled {
		log.Print("Startship public router is disabled")
	}
	nextDashboard, e := a.prepareDashboardListeners(settings)
	if e != nil {
		log.Fatal(e)
	}
	a.commitDashboardListeners(settings, nextDashboard.next)
	socket, e := a.serveSocket(socketPath(data))
	if e != nil {
		log.Fatal(e)
	}
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.stopRouterListeners()
	socket.Shutdown(ctx)
}

func (a *Server) configureRouterProxy(settings config.Daemon) http.Handler {
	if email := os.Getenv("STARTSHIP_ACME_EMAIL"); email != "" {
		a.https = true
		m := &autocert.Manager{Client: &acme.Client{DirectoryURL: settings.ACMEDirectory}, Prompt: autocert.AcceptTOS, Email: email, Cache: autocert.DirCache(filepath.Join(a.data, "certs")), HostPolicy: func(ctx context.Context, host string) error {
			if a.certAllowed(host) {
				return nil
			}
			return errInvalid
		}}
		legacyHTTP := m.HTTPHandler(a)
		a.legacyCertificate = m.GetCertificate
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := a.challenges.Load(r.URL.Path); ok {
				a.serveChallenge(w, r)
				return
			}
			legacyHTTP.ServeHTTP(w, r)
		})
	}
	return a
}
