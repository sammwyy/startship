package main

import (
	"context"
	"crypto/tls"
	"golang.org/x/crypto/acme/autocert"
	"log"
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
func main() {
	data, e := filepath.Abs(env("HARBOR_DATA", "./data"))
	if e != nil {
		log.Fatal(e)
	}
	if e = os.MkdirAll(data, 0700); e != nil {
		log.Fatal(e)
	}
	s, e := openStore(filepath.Join(data, "harbor.db"))
	if e != nil {
		log.Fatal(e)
	}
	defer s.db.Close()
	a := &Server{store: s, data: data, queues: make(map[string]chan string)}
	if u, p := os.Getenv("HARBOR_ADMIN_USER"), os.Getenv("HARBOR_ADMIN_PASSWORD"); u != "" && p != "" {
		var n int
		s.db.QueryRow("SELECT count(*) FROM users").Scan(&n)
		if n == 0 {
			if _, e = a.createUser(u, p); e != nil {
				log.Fatal(e)
			}
		}
	}
	a.restore()
	dash := &http.Server{Addr: env("HARBOR_DASHBOARD_ADDR", ":8080"), Handler: a.api(), ReadHeaderTimeout: 10 * time.Second}
	proxy := &http.Server{Addr: env("HARBOR_HTTP_ADDR", ":80"), Handler: a, ReadHeaderTimeout: 10 * time.Second}
	servers := []*http.Server{dash, proxy}
	if email := os.Getenv("HARBOR_ACME_EMAIL"); email != "" {
		m := &autocert.Manager{Prompt: autocert.AcceptTOS, Email: email, Cache: autocert.DirCache(filepath.Join(data, "certs")), HostPolicy: func(ctx context.Context, host string) error {
			for _, r := range s.list("routes", "") {
				if normalizeHost(r.Host) == normalizeHost(host) {
					return nil
				}
			}
			return errInvalid
		}}
		proxy.Handler = m.HTTPHandler(a)
		https := &http.Server{Addr: env("HARBOR_HTTPS_ADDR", ":443"), Handler: a, TLSConfig: m.TLSConfig(), ReadHeaderTimeout: 10 * time.Second}
		https.TLSConfig.MinVersion = tls.VersionTLS12
		servers = append(servers, https)
		go func() {
			if e := https.ListenAndServeTLS("", ""); e != nil && e != http.ErrServerClosed {
				log.Fatal(e)
			}
		}()
	}
	go func() {
		if e := dash.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Fatal(e)
		}
	}()
	go func() {
		if e := proxy.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Fatal(e)
		}
	}()
	log.Printf("Harbor dashboard listening on %s", dash.Addr)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		srv.Shutdown(ctx)
	}
}
