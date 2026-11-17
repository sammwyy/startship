package main

import (
	"context"
	"crypto/tls"
	"fmt"
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

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func dataDirectory() string {
	if configured := os.Getenv("STARTSHIP_DATA"); configured != "" {
		return configured
	}
	if _, err := os.Stat("./data"); !os.IsNotExist(err) {
		log.Print("Using existing ./data; consider renaming it to ./_data so Go tooling skips it")
		return "./data"
	}
	return "./_data"
}

func listen(name, addr, variable, fallback string) (net.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%s: cannot listen on %s (%w); set %s=%s or run with CAP_NET_BIND_SERVICE for privileged ports", name, addr, err, variable, fallback)
	}
	return l, nil
}
func serve(servers []*http.Server) error {
	names := []string{"dashboard", "proxy", "HTTPS proxy"}
	variables := []string{"STARTSHIP_DASHBOARD_ADDR", "STARTSHIP_HTTP_ADDR", "STARTSHIP_HTTPS_ADDR"}
	fallbacks := []string{":18080", ":8081", ":8443"}
	listeners := make([]net.Listener, 0, len(servers))
	for i, srv := range servers {
		l, err := listen(names[i], srv.Addr, variables[i], fallbacks[i])
		if err != nil {
			for _, opened := range listeners {
				opened.Close()
			}
			return err
		}
		listeners = append(listeners, l)
	}
	for i, srv := range servers {
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
		log.Printf("Startship %s listening on %s", names[i], l.Addr())
	}
	return nil
}
func main() {
	data, e := filepath.Abs(dataDirectory())
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
	defer s.db.Close()
	a := &Server{store: s, data: data, queues: make(map[string]chan string), dashboardDomain: os.Getenv("STARTSHIP_DASHBOARD_DOMAIN")}
	if e = a.bootstrapAdmin(os.Getenv("STARTSHIP_ADMIN_USER"), os.Getenv("STARTSHIP_ADMIN_PASSWORD")); e != nil {
		log.Fatal(e)
	}
	a.restore()
	metricsContext, stopMetrics := context.WithCancel(context.Background())
	defer stopMetrics()
	go a.sampleTrafficResources(metricsContext)
	dash := &http.Server{Addr: env("STARTSHIP_DASHBOARD_ADDR", ":8080"), Handler: a.api(), ReadHeaderTimeout: 10 * time.Second}
	proxy := &http.Server{Addr: env("STARTSHIP_HTTP_ADDR", ":80"), Handler: a, ReadHeaderTimeout: 10 * time.Second}
	servers := []*http.Server{dash, proxy}
	if email := os.Getenv("STARTSHIP_ACME_EMAIL"); email != "" {
		a.https = true
		m := &autocert.Manager{Prompt: autocert.AcceptTOS, Email: email, Cache: autocert.DirCache(filepath.Join(data, "certs")), HostPolicy: func(ctx context.Context, host string) error {
			if a.certAllowed(host) {
				return nil
			}
			return errInvalid
		}}
		proxy.Handler = m.HTTPHandler(a)
		https := &http.Server{Addr: env("STARTSHIP_HTTPS_ADDR", ":443"), Handler: a, TLSConfig: m.TLSConfig(), ReadHeaderTimeout: 10 * time.Second}
		https.TLSConfig.MinVersion = tls.VersionTLS12
		servers = append(servers, https)
	}
	if e := serve(servers); e != nil {
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
