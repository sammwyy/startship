package router

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sammwyy/startship/internal/firewall"
)

type restoredBody struct {
	io.Reader
	io.Closer
}
type limitedBody struct {
	io.ReadCloser
	exceeded atomic.Bool
}

func (b *limitedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	var max *http.MaxBytesError
	if errors.As(err, &max) {
		b.exceeded.Store(true)
	}
	return n, err
}

func blocked(w http.ResponseWriter, status, retry int) {
	var id [12]byte
	_, _ = rand.Read(id[:])
	ray := hex.EncodeToString(id[:])
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Startship-Ray", ray)
	if retry > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
	}
	w.WriteHeader(status)
	fmt.Fprintf(w, "Blocked by Startship firewall\nRequest ID: %s\n", ray)
}
func (a *Router) emit(req *firewall.Request, d firewall.Decision) {
	if a.hooks.Event != nil {
		for _, e := range d.Events {
			a.hooks.Event(e)
		}
	}
	if a.hooks.Observe != nil {
		a.hooks.Observe(req.WorkspaceID, req, d)
	}
}
func rejected(req *firewall.Request, id, name string, status int) firewall.Decision {
	class, _ := firewall.ClassifyUA(req.UA)
	return firewall.Decision{Action: firewall.Block, RuleID: id, RuleName: name, StatusCode: status, Events: []firewall.Event{{Time: time.Now().UTC(), WorkspaceID: req.WorkspaceID, ProjectID: req.ProjectID, RouteHost: req.RouteHost, IP: req.IP, Country: req.Country, Method: req.Method, Host: req.Host, Path: req.Path, UA: req.UA, UAClass: class, RuleID: id, RuleName: name, Action: firewall.Block, Status: status}}}
}
func (a *Router) enforce(w http.ResponseWriter, r *http.Request, target Target, workspace string, engine *firewall.Engine) (func(), bool) {
	ip, country := firewall.ResolveClient(r.RemoteAddr, r.Header, a.options.TrustedProxies, a.options.GeoIP)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	req := &firewall.Request{IP: ip, Country: country, Method: r.Method, Host: r.Host, Path: r.URL.Path, RawQuery: r.URL.RawQuery, Headers: r.Header, UA: r.UserAgent(), Scheme: scheme, ContentLength: r.ContentLength, URLBytes: len(r.URL.RequestURI()), WorkspaceID: workspace, ProjectID: target.Route.ProjectID, RouteHost: target.Route.Host}
	if !engine.Acquire(ip) {
		d := rejected(req, "__concurrency", "Concurrent request limit", 429)
		d.RetryAfter = 1
		a.emit(req, d)
		blocked(w, 429, 1)
		return nil, false
	}
	release := func() { engine.Release(ip) }
	fail := func(d firewall.Decision) (func(), bool) {
		release()
		a.emit(req, d)
		blocked(w, d.StatusCode, d.RetryAfter)
		return nil, false
	}
	limits := engine.Limits()
	if engine.Mode() != "off" && r.Body != nil {
		ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		scannable := ct == "application/json" || ct == "application/x-www-form-urlencoded" || strings.HasSuffix(ct, "+json")
		if scannable || engine.Mode() == "enforce" && limits.MaxBodyBytes > 0 && limits.MaxBodyBytes <= 65536 {
			// An extra byte distinguishes a complete <=64KB body from a prefix.
			sample, err := io.ReadAll(io.LimitReader(r.Body, 65537))
			if err != nil {
				release()
				http.Error(w, "Startship: could not read request body.", 400)
				return nil, false
			}
			r.Body = &restoredBody{io.MultiReader(bytes.NewReader(sample), r.Body), r.Body}
			if scannable && len(sample) <= 65536 {
				req.BodySample = sample
			}
			if limits.MaxBodyBytes > 0 && int64(len(sample)) > limits.MaxBodyBytes {
				req.ContentLength = int64(len(sample))
			}
		}
	}
	d := engine.Evaluate(req)
	if d.StatusCode != 0 {
		if d.Action == firewall.Ban && a.hooks.Ban != nil {
			if err := a.hooks.Ban(Ban{workspace, ip, d.BanSeconds, d.RuleID, d.RuleName}); err != nil {
				release()
				a.emit(req, d)
				http.Error(w, "Startship: unable to persist firewall ban.", 503)
				return nil, false
			}
		}
		return fail(d)
	}
	if a.hooks.Event != nil {
		for _, event := range d.Events {
			a.hooks.Event(event)
		}
	}
	// Explicit allow matches bypass sizes and rates, but never concurrency.
	var limited *limitedBody
	if engine.Mode() == "enforce" && limits.MaxBodyBytes > 0 && d.RuleID == "" && r.Body != nil {
		limited = &limitedBody{ReadCloser: http.MaxBytesReader(w, r.Body, limits.MaxBodyBytes)}
		r.Body = limited
	}
	return func() {
		release()
		if limited != nil && limited.exceeded.Load() {
			a.emit(req, rejected(req, "__body", "Body size limit", 413))
			return
		}
		if a.hooks.Observe != nil {
			a.hooks.Observe(workspace, req, d)
		}
	}, true
}
