package server

import (
	"context"
	"crypto/tls"
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
		return nil, fmt.Errorf("%s: cannot listen on %s (%w); set %s=%s or run with CAP_NET_BIND_SERVICE for privileged ports", name, addr, err, variable, fallback)
	}
	return l, nil
}
func serve(servers []*http.Server, groups ...int) error {
	names := []string{"dashboard", "proxy", "HTTPS proxy"}
	variables := []string{"STARTSHIP_DASHBOARD_ADDR", "STARTSHIP_HTTP_ADDR", "STARTSHIP_HTTPS_ADDR"}
	fallbacks := []string{":18080", ":8081", ":8443"}
	listeners := make([]net.Listener, 0, len(servers))
	for i, srv := range servers {
		group := i
		if len(groups) > 0 {
			group = groups[i]
		}
		l, err := listen(names[group], srv.Addr, variables[group], fallbacks[group])
		if err != nil {
			for _, opened := range listeners {
				opened.Close()
			}
			return err
		}
		listeners = append(listeners, l)
	}
	for i, srv := range servers {
		group := i
		if len(groups) > 0 {
			group = groups[i]
		}
		l := listeners[i]
		go func() {
			var err error
			if srv.TLSConfig != nil {
				err = srv.ServeTLS(l, "", "")
			} else {
				err = srv.Serve(l)
			}
			if err != nil && err != http.ErrServerClosed {
				log.Fatal(err)
			}
		}()
		log.Printf("Startship %s listening on %s", names[group], l.Addr())
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
	if len(settings.HTTPSListen) > 0 {
		_, a.routerOptions.HTTPSPort, _ = net.SplitHostPort(settings.HTTPSListen[0])
	}
	a.reserveServicePorts(settings.DashboardListen, settings.HTTPListen, settings.HTTPSListen)
	a.initLive()
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
	go a.watchUpdates(metricsContext)
	go a.watchCertificates(metricsContext)
	var proxyHandler http.Handler = a
	if email := os.Getenv("STARTSHIP_ACME_EMAIL"); email != "" {
		a.https = true
		m := &autocert.Manager{Client: &acme.Client{DirectoryURL: settings.ACMEDirectory}, Prompt: autocert.AcceptTOS, Email: email, Cache: autocert.DirCache(filepath.Join(data, "certs")), HostPolicy: func(ctx context.Context, host string) error {
			if a.certAllowed(host) {
				return nil
			}
			return errInvalid
		}}
		legacyHTTP := m.HTTPHandler(a)
		proxyHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := a.challenges.Load(r.URL.Path); ok {
				a.serveChallenge(w, r)
				return
			}
			legacyHTTP.ServeHTTP(w, r)
		})
		a.legacyCertificate = m.GetCertificate
	}
	var servers []*http.Server
	var groups []int
	for group, entries := range [][]string{settings.DashboardListen, settings.HTTPListen, settings.HTTPSListen} {
		addresses, err := config.Resolve(entries)
		if err != nil {
			log.Fatal(err)
		}
		for _, addr := range addresses {
			handler := a.api()
			if group == 1 {
				handler = proxyHandler
			}
			if group == 2 {
				handler = a
			}
			srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
			if group == 2 {
				srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: a.tlsCertificate}
			}
			servers = append(servers, srv)
			groups = append(groups, group)
		}
	}
	if e := serve(servers, groups...); e != nil {
		log.Fatal(e)
	}
	socket, e := a.serveSocket(socketPath(data))
	if e != nil {
		log.Fatal(e)
	}
	servers = append(servers, socket)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		srv.Shutdown(ctx)
	}
}
