package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// One hour of five-second buckets, plus the current bucket. No request URLs,
// query strings, headers, bodies, client addresses, or durable storage.
const trafficSlots = 721
const trafficStep = 5 * time.Second

type trafficBucket struct {
	At           int64    `json:"at"`
	Requests     int64    `json:"requests"`
	Errors       int64    `json:"errors"`
	ClientErrors int64    `json:"client_errors"`
	DurationMS   float64  `json:"-"`
	AverageMS    float64  `json:"average_ms"`
	MaxMS        float64  `json:"max_ms"`
	CPU          *float64 `json:"cpu_percent"`
	Memory       *float64 `json:"memory_bytes"`
}

type trafficRequest struct {
	At       time.Time `json:"at"`
	Method   string    `json:"method"`
	Route    string    `json:"route"`
	Status   int       `json:"status"`
	Duration float64   `json:"duration_ms"`
}

type containerUsage struct {
	Name   string  `json:"name"`
	CPU    float64 `json:"cpu_percent"`
	Memory float64 `json:"memory_bytes"`
	Limit  float64 `json:"limit_bytes"`
}

type projectMetrics struct {
	Buckets        [trafficSlots]trafficBucket
	Recent         []trafficRequest
	Containers     []containerUsage
	ResourceStatus string
	SampledAt      time.Time
}

type trafficMetrics struct {
	mu       sync.Mutex
	projects map[string]*projectMetrics
}

func (m *trafficMetrics) project(id string) *projectMetrics {
	if m.projects == nil {
		m.projects = make(map[string]*projectMetrics)
	}
	if m.projects[id] == nil {
		m.projects[id] = &projectMetrics{}
	}
	return m.projects[id]
}

func (p *projectMetrics) bucket(at time.Time) *trafficBucket {
	stamp := at.Unix() / 5 * 5
	b := &p.Buckets[(stamp/5)%trafficSlots]
	if b.At != stamp {
		*b = trafficBucket{At: stamp}
	}
	return b
}

func (m *trafficMetrics) record(project, method, route string, status int, duration time.Duration, at time.Time) {
	if project == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.project(project)
	b := p.bucket(at)
	b.Requests++
	if status >= 500 {
		b.Errors++
	}
	if status >= 400 && status < 500 {
		b.ClientErrors++
	}
	ms := float64(duration) / float64(time.Millisecond)
	b.DurationMS += ms
	b.AverageMS = b.DurationMS / float64(b.Requests)
	if ms > b.MaxMS {
		b.MaxMS = ms
	}
	p.Recent = append(p.Recent, trafficRequest{at, truncateTraffic(method, 16), truncateTraffic(route, 256), status, ms})
	if len(p.Recent) > 40 {
		p.Recent = append([]trafficRequest(nil), p.Recent[len(p.Recent)-40:]...)
	}
}

func truncateTraffic(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (a *Server) projectTraffic(w http.ResponseWriter, r *http.Request, project Record) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		fail(w, 405, "method not allowed")
		return
	}
	seconds := 300
	if v := r.URL.Query().Get("window"); v != "" {
		var err error
		seconds, err = strconv.Atoi(v)
		if err != nil || (seconds != 300 && seconds != 900 && seconds != 3600) {
			fail(w, 400, "window must be 300, 900, or 3600 seconds")
			return
		}
	}
	now := time.Now().UTC()
	end := now.Unix() / 5 * 5
	start := end - int64(seconds) + 5
	// Always 60 chart intervals; traffic is exact, resource values are the last
	// observed sample per interval. Null means unsampled, never fabricated zero.
	width := int64(seconds / 60)
	buckets := make([]trafficBucket, 60)
	for i := range buckets {
		buckets[i].At = start + int64(i)*width
	}
	a.traffic.mu.Lock()
	p := a.traffic.project(project.ID)
	for _, b := range p.Buckets {
		if b.At < start || b.At > end {
			continue
		}
		i := (b.At - start) / width
		out := &buckets[i]
		out.Requests += b.Requests
		out.Errors += b.Errors
		out.ClientErrors += b.ClientErrors
		out.DurationMS += b.DurationMS
		if b.MaxMS > out.MaxMS {
			out.MaxMS = b.MaxMS
		}
		// Iterate timestamps below for the latest resource sample, independent
		// of ring-buffer position after wraparound.
	}
	for stamp := start; stamp <= end; stamp += 5 {
		b := p.Buckets[(stamp/5)%trafficSlots]
		if b.At == stamp && b.CPU != nil {
			out := &buckets[(stamp-start)/width]
			out.CPU, out.Memory = b.CPU, b.Memory
		}
	}
	var total trafficBucket
	for i := range buckets {
		b := &buckets[i]
		if b.Requests > 0 {
			b.AverageMS = b.DurationMS / float64(b.Requests)
		}
		total.Requests += b.Requests
		total.Errors += b.Errors
		total.ClientErrors += b.ClientErrors
		total.DurationMS += b.DurationMS
		if b.MaxMS > total.MaxMS {
			total.MaxMS = b.MaxMS
		}
	}
	if total.Requests > 0 {
		total.AverageMS = total.DurationMS / float64(total.Requests)
	}
	recent := make([]trafficRequest, 0, len(p.Recent))
	for i := len(p.Recent) - 1; i >= 0; i-- {
		if p.Recent[i].At.Unix() >= start {
			recent = append(recent, p.Recent[i])
		}
	}
	containers := append([]containerUsage{}, p.Containers...)
	status, sampledAt := p.ResourceStatus, p.SampledAt
	a.traffic.mu.Unlock()
	if status == "" {
		status = "waiting"
	}
	if status == "ok" && now.Sub(sampledAt) > 20*time.Second {
		status = "stale"
	}
	w.Header().Set("Cache-Control", "no-store")
	reply(w, 200, map[string]any{"project_id": project.ID, "now": now, "window_seconds": seconds, "interval_seconds": width,
		"buckets": buckets, "total": total, "recent": recent, "containers": containers, "resource_status": status, "sampled_at": sampledAt,
		"retention_seconds": 3600})
}

// trafficWriter preserves streaming and protocol upgrades used by ReverseProxy.
type trafficWriter struct {
	http.ResponseWriter
	status int
}

func (w *trafficWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *trafficWriter) statusCode() int {
	if w.status == 0 {
		return 200
	}
	return w.status
}
func (w *trafficWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != 101 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *trafficWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *trafficWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *trafficWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func memoryBytes(s string) (float64, error) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
		i++
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, err
	}
	units := map[string]float64{"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12, "KiB": 1024, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40}
	unit, ok := units[strings.TrimSpace(s[i:])]
	if !ok {
		return 0, fmt.Errorf("unknown memory unit")
	}
	return n * unit, nil
}

type dockerUsage struct{ Name, CPUPerc, MemUsage string }

func parseContainerUsage(line []byte) (string, containerUsage, error) {
	var d dockerUsage
	if err := json.Unmarshal(line, &d); err != nil {
		return "", containerUsage{}, err
	}
	cpu, err := strconv.ParseFloat(strings.TrimSuffix(d.CPUPerc, "%"), 64)
	if err != nil || cpu < 0 || math.IsNaN(cpu) || math.IsInf(cpu, 0) {
		return "", containerUsage{}, fmt.Errorf("invalid CPU sample")
	}
	parts := strings.Split(d.MemUsage, "/")
	if len(parts) != 2 {
		return "", containerUsage{}, fmt.Errorf("invalid memory sample")
	}
	used, e1 := memoryBytes(parts[0])
	limit, e2 := memoryBytes(parts[1])
	if e1 != nil || e2 != nil {
		return "", containerUsage{}, fmt.Errorf("invalid memory sample")
	}
	return d.Name, containerUsage{CPU: cpu, Memory: used, Limit: limit}, nil
}

func (a *Server) sampleTrafficResources(ctx context.Context) {
	ticker := time.NewTicker(trafficStep)
	defer ticker.Stop()
	for {
		a.collectTrafficResources(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *Server) collectTrafficResources(ctx context.Context) {
	projects := a.store.list("projects", "")
	apps := a.store.list("apps", "")
	type activeContainer struct{ project, name, container string }
	active := []activeContainer{}
	args := []string{"stats", "--no-stream", "--format", "{{json .}}"}
	for _, app := range apps {
		d, err := a.store.get(app.Active)
		if err == nil && d.Status == "running" && d.Container != "" {
			active = append(active, activeContainer{app.ProjectID, app.Name, d.Container})
			args = append(args, d.Container)
		}
	}
	samples := map[string]containerUsage{}
	if len(active) > 0 {
		commandCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		out, _ := exec.CommandContext(commandCtx, "docker", args...).Output()
		cancel()
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if name, usage, err := parseContainerUsage([]byte(line)); err == nil {
				samples[name] = usage
			}
		}
	}
	now := time.Now().UTC()
	a.traffic.mu.Lock()
	defer a.traffic.mu.Unlock()
	live := make(map[string]bool, len(projects))
	for _, project := range projects {
		live[project.ID] = true
		p := a.traffic.project(project.ID)
		p.Containers = []containerUsage{}
		count := 0
		var cpu, memory float64
		for _, c := range active {
			if c.project != project.ID {
				continue
			}
			count++
			if sample, ok := samples[c.container]; ok {
				sample.Name = c.name
				p.Containers = append(p.Containers, sample)
				cpu += sample.CPU
				memory += sample.Memory
			}
		}
		p.ResourceStatus = "ok"
		if count == 0 {
			p.ResourceStatus = "none"
		} else if len(p.Containers) != count {
			p.ResourceStatus = "unavailable"
		}
		p.SampledAt = now
		if p.ResourceStatus == "ok" {
			b := p.bucket(now)
			b.CPU, b.Memory = &cpu, &memory
		}
	}
	for id := range a.traffic.projects {
		if !live[id] {
			delete(a.traffic.projects, id)
		}
	}
}
