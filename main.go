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
	a := &Server{store: s, data: data, queues: make(map[string]chan string)}
	if u, p := os.Getenv("STARTSHIP_ADMIN_USER"), os.Getenv("STARTSHIP_ADMIN_PASSWORD"); u != "" && p != "" {
		var n int
		s.db.QueryRow("SELECT count(*) FROM users").Scan(&n)
		if n == 0 {
			if _, e = a.createUser(u, p); e != nil {
				log.Fatal(e)
			}
		}
	}
	a.restore()
	dash := &http.Server{Addr: env("STARTSHIP_DASHBOARD_ADDR", ":8080"), Handler: a.api(), ReadHeaderTimeout: 10 * time.Second}
	proxy := &http.Server{Addr: env("STARTSHIP_HTTP_ADDR", ":80"), Handler: a, ReadHeaderTimeout: 10 * time.Second}
	servers := []*http.Server{dash, proxy}
	if email := os.Getenv("STARTSHIP_ACME_EMAIL"); email != "" {
		m := &autocert.Manager{Prompt: autocert.AcceptTOS, Email: email, Cache: autocert.DirCache(filepath.Join(data, "certs")), HostPolicy: func(ctx context.Context, host string) error {
			for _, r := range s.list("routes", "") {
				if normalizeHost(r.Host) == normalizeHost(host) {
					return nil
				}
			}
			return errInvalid
		}}
		proxy.Handler = m.HTTPHandler(a)
		https := &http.Server{Addr: env("STARTSHIP_HTTPS_ADDR", ":443"), Handler: a, TLSConfig: m.TLSConfig(), ReadHeaderTimeout: 10 * time.Second}
		https.TLSConfig.MinVersion = tls.VersionTLS12
		servers = append(servers, https)
	}
	listeners, e := bindServers(servers)
	if e != nil {
		log.Fatal(e)
	}
	for i, srv := range servers {
		ln := listeners[i]
		go func() {
			var err error
			if srv.TLSConfig != nil {
				err = srv.ServeTLS(ln, "", "")
			} else {
				err = srv.Serve(ln)
			}
			if err != nil && err != http.ErrServerClosed {
				log.Fatal(err)
			}
		}()
	}
	log.Printf("Startship dashboard listening on %s", dash.Addr)
	log.Printf("Startship proxy listening on %s", proxy.Addr)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		srv.Shutdown(ctx)
	}
}

func bindServers(servers []*http.Server) ([]net.Listener, error) {
	var listeners []net.Listener
	for _, srv := range servers {
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			for _, opened := range listeners {
				opened.Close()
			}
			return nil, fmt.Errorf("listen on %s: %w; ports below 1024 require root or cap_net_bind_service (sudo setcap cap_net_bind_service=+ep ./startshipd); for a local trial set STARTSHIP_HTTP_ADDR=:8000", srv.Addr, err)
		}
		listeners = append(listeners, ln)
	}
	return listeners, nil
}
