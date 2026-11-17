package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type socketContextKey struct{}

func localRequest(r *http.Request) bool {
	local, _ := r.Context().Value(socketContextKey{}).(bool)
	return local
}

func socketPath(data string) string {
	return env("STARTSHIP_SOCKET", filepath.Join(env("RUNTIME_DIRECTORY", data), "startship.sock"))
}

func (a *Server) serveSocket(path string) (*http.Server, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to remove non-socket %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0660); err != nil {
		l.Close()
		return nil, err
	}
	srv := &http.Server{Handler: a.api(), ReadHeaderTimeout: 10 * time.Second, ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
		return context.WithValue(ctx, socketContextKey{}, true)
	}}
	go srv.Serve(l)
	return srv, nil
}
