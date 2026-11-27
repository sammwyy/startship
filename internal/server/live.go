package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/sammwyy/startship/internal/version"
)

type liveEvent struct {
	changeKey         string
	firewallWorkspace string
	firewallConfig    bool
	record            Record
	deleted           bool
	id, text          string
	subscription      *liveSub
	active            string
	end               int
}
type liveHub struct {
	mu      sync.Mutex
	logMu   sync.Mutex
	clients map[*liveClient]bool
}
type liveClient struct {
	events  chan liveEvent
	pending map[string]liveEvent
	cancel  context.CancelFunc
}

func (a *Server) initLive() {
	a.live = &liveHub{clients: make(map[*liveClient]bool)}
	a.store.OnChange = func(r Record, deleted bool) { a.live.publish(liveEvent{record: r, deleted: deleted}) }
}
func (h *liveHub) publish(e liveEvent) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if e.changeKey != "" {
			c.pending[e.changeKey] = e
			continue
		}
		if e.firewallWorkspace != "" {
			key := "firewall:" + e.firewallWorkspace
			merged := e
			merged.firewallConfig = merged.firewallConfig || c.pending[key].firewallConfig
			c.pending[key] = merged
			continue
		}
		if e.record.ID != "" {
			c.pending[e.record.ID] = e
			continue
		}
		select {
		case c.events <- e:
		default:
			c.cancel()
		}
	}
}

type liveSub struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	Topic         string `json:"topic"`
	ProjectID     string `json:"project_id"`
	WorkspaceID   string `json:"workspace_id"`
	ReleaseID     string `json:"release_id"`
	ContainerID   string `json:"container_id"`
	Window        int    `json:"window"`
	last          map[string]json.RawMessage
	kinds         map[string]string
	update        string
	active        string
	cancel        context.CancelFunc
	next          time.Time
	logEnd        int
	firewallDirty bool
}

func (s *liveSub) relevant(r Record) bool {
	switch s.Topic {
	case "uptime":
		return r.ID == s.ProjectID
	case "status_page", "notifications":
		return r.ID == s.WorkspaceID
	case "firewall":
		return r.ID == s.WorkspaceID
	case "project":
		return r.ID == s.ProjectID || r.ProjectID == s.ProjectID
	case "workspace":
		return r.ID == s.WorkspaceID || r.Kind == "projects" && r.WorkspaceID == s.WorkspaceID
	case "workspaces":
		return r.Kind == "workspaces"
	case "certificates":
		return r.Kind == "certificates" && r.WorkspaceID == s.WorkspaceID
	case "services", "volumes", "backups", "recipe_apps":
		return r.Kind == s.Topic && r.WorkspaceID == s.WorkspaceID
	case "release_log":
		return r.ID == s.ReleaseID
	case "container_log":
		return r.ID == s.ContainerID
	}
	return false
}

func (s *liveSub) message(t string) map[string]any {
	return map[string]any{"type": t, "id": s.ID, "topic": s.Topic}
}
func liveKind(r Record) string {
	switch r.Kind {
	case "apps":
		return "container"
	case "deployments", "releases":
		return "release"
	case "projects":
		return "project"
	case "previews":
		return "preview"
	case "routes":
		return "route"
	}
	return ""
}
func (a *Server) liveAccess(uid string, s *liveSub) (*access, Record, string) {
	x := a.access(uid)
	id, kind, perm := "", "", ""
	switch s.Topic {
	case "project", "traffic", "project_activity", "uptime":
		id, kind = s.ProjectID, "projects"
		if s.Topic == "project_activity" {
			perm = "activity.view"
		}
		if s.Topic == "traffic" {
			perm = "traffic.view"
			if s.Window == 0 {
				s.Window = 300
			}
			if s.Window != 300 && s.Window != 900 && s.Window != 3600 {
				return x, Record{}, "invalid subscription"
			}
		}
	case "workspace", "certificates", "services", "volumes", "firewall", "backups", "workspace_activity", "status_page", "notifications", "recipe_apps":
		id, kind = s.WorkspaceID, "workspaces"
		if s.Topic == "status_page" || s.Topic == "notifications" {
			perm = "workspace.manage"
		}
		if s.Topic == "workspace_activity" {
			perm = "activity.view"
		}
		if s.Topic == "firewall" {
			perm = "firewall.view"
		}
		if s.Topic == "certificates" {
			perm = "workspace.domains"
		}
	case "release_log":
		id, perm = s.ReleaseID, "logs.view"
	case "container_log":
		id, kind, perm = s.ContainerID, "apps", "logs.view"
	case "audit_activity":
		if !x.platform("audit.view") {
			return x, Record{}, "forbidden"
		}
		return x, Record{}, ""
	case "system_migrations":
		if !x.admin {
			return x, Record{}, "forbidden"
		}
		return x, Record{}, ""
	case "system_router", "system_update", "account_activity", "account_sessions", "account_tokens", "workspaces", "recipes_catalog", "services_catalog":
		return x, Record{}, ""
	default:
		return x, Record{}, "invalid subscription"
	}
	if id == "" {
		return x, Record{}, "invalid subscription"
	}
	r, err := a.store.Get(id)
	if err != nil || kind != "" && r.Kind != kind || s.Topic == "release_log" && r.Kind != "releases" && r.Kind != "deployments" {
		return x, r, "not found"
	}
	if !x.visible(r) || perm != "" && !x.can(r, perm) {
		return x, r, "forbidden"
	}
	return x, r, ""
}
func (a *Server) liveData(ctx context.Context, uid string, s *liveSub) (any, map[string]apiRecord, Record, string) {
	x, r, why := a.liveAccess(uid, s)
	if why != "" {
		return nil, nil, r, why
	}
	if s.Topic == "system_update" {
		return a.updater().info(ctx, false), nil, r, ""
	}
	records := map[string]apiRecord{}
	convert := func(v Record) apiRecord {
		v.Log = ""
		out := x.publicRecord(v)
		if v.Kind == "apps" {
			out = a.containerRecord(v)
		}
		records[v.ID] = out
		return out
	}
	switch s.Topic {
	case "recipes_catalog", "services_catalog":
		a.ensureCatalog(s.Topic)
		entries, meta := a.catalogData(s.Topic)
		return map[string]any{"entries": entries, "metadata": meta}, nil, r, ""
	case "system_router":
		return a.routerConfig(), nil, r, ""
	case "system_migrations":
		return map[string]any{"jobs": a.listMigrationJobs()}, nil, r, ""
	case "workspaces":
		out := []apiRecord{}
		for _, v := range a.store.List("workspaces", "") {
			if x.visible(v) {
				out = append(out, x.publicRecord(v))
			}
		}
		return out, nil, r, ""
	case "account_sessions", "account_activity", "account_tokens", "workspace_activity", "project_activity", "audit_activity":
		data, err := a.activityLive(uid, s)
		if err != nil {
			return nil, nil, r, "activity unavailable"
		}
		return data, nil, r, ""
	case "firewall":
		policy, lists, err := a.firewallConfig(r.ID)
		if err != nil {
			return nil, nil, r, "firewall storage unavailable"
		}
		events, stats := a.firewallData(r.ID, time.Now())
		if len(events) > 100 {
			events = events[:100]
		}
		return map[string]any{"policy": policy, "lists": lists, "events": events, "stats": stats, "can_manage": x.can(r, "firewall.manage")}, nil, r, ""
	case "services":
		return map[string]any{"services": a.publicServices(s.WorkspaceID, x)}, nil, r, ""
	case "recipe_apps":
		return a.recipeApps(s.WorkspaceID, x.at(s.WorkspaceID, "")["apps.manage"]), nil, r, ""
	case "volumes":
		a.refreshVolumeSizes(s.WorkspaceID)
		return map[string]any{"volumes": listResources[Volume](a, "volumes", s.WorkspaceID)}, nil, r, ""
	case "backups":
		a.refreshVolumeSizes(s.WorkspaceID)
		return a.backupsData(s.WorkspaceID, ""), nil, r, ""
	case "certificates":
		return map[string]any{"certificates": a.certificates(s.WorkspaceID)}, nil, r, ""
	case "uptime":
		data, err := a.summarizeUptime(r, time.Now(), 30, true)
		if err != nil {
			return nil, nil, r, "uptime unavailable"
		}
		return data, nil, r, ""
	case "status_page":
		data, err := a.statusConfig(r.ID)
		if err != nil {
			return nil, nil, r, "status page unavailable"
		}
		return data, nil, r, ""
	case "notifications":
		data, err := a.notificationsData(r.ID)
		if err != nil {
			return nil, nil, r, "notifications unavailable"
		}
		return data, nil, r, ""
	case "traffic":
		return a.trafficData(r, s.Window), nil, r, ""
	case "container_log":
		if s.last != nil && s.active == r.Active {
			return nil, nil, r, ""
		}
		text, e := a.containerLogText(ctx, r)
		if e != nil {
			return nil, nil, r, "not found"
		}
		return map[string]string{"log": text}, nil, r, ""
	case "release_log":
		full := x.publicRecord(r)
		if r.Status == "running" || r.Status == "superseded" {
			if r.Kind == "releases" && r.PreviewID == "" {
				full.Redeployable = a.projectReleaseAvailable(r) == nil
			} else if r.Kind != "releases" && r.PreviewID == "" {
				full.Redeployable = releaseAvailable(r) == nil
			}
		}
		convert(r)
		upsert := full
		upsert.Log = ""
		records[r.ID] = upsert
		return full, records, r, ""
	case "workspace":
		projects := []apiRecord{}
		for _, v := range a.store.List("projects", "") {
			if v.WorkspaceID == s.WorkspaceID && x.visible(v) {
				projects = append(projects, convert(v))
			}
		}
		return map[string]any{"projects": projects}, records, r, ""
	default:
		data := map[string]any{"project": convert(r)}
		for _, k := range []string{"apps", "releases", "routes", "previews"} {
			out := []apiRecord{}
			for _, v := range a.store.List(k, "") {
				if v.ProjectID == s.ProjectID && x.visible(v) {
					out = append(out, convert(v))
				}
			}
			key := k
			if k == "apps" {
				key = "containers"
			}
			data[key] = out
		}
		return data, records, r, ""
	}
}
func (a *Server) liveHandler(w http.ResponseWriter, r *http.Request, uid string) {
	origin := r.Header.Get("Origin")
	if origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
		fail(w, 403, "forbidden")
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	if uid == "" {
		// Browsers can see a WebSocket close code but not a handshake's HTTP 401.
		conn.Close(websocket.StatusCode(4401), "reauthentication required")
		return
	}
	conn.SetReadLimit(8192)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := &liveClient{events: make(chan liveEvent, 1024), pending: map[string]liveEvent{}, cancel: cancel}
	a.live.mu.Lock()
	a.live.clients[c] = true
	a.live.mu.Unlock()
	defer func() { a.live.mu.Lock(); delete(a.live.clients, c); a.live.mu.Unlock() }()
	send := func(v any) bool {
		b, e := json.Marshal(v)
		if e != nil {
			return false
		}
		writeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		return conn.Write(writeCtx, websocket.MessageText, b) == nil
	}
	if !send(map[string]any{"type": "ready", "user_id": uid, "version": version.Version, "commit": version.Commit, "unavailable_engines": unavailableEngines()}) {
		return
	}
	input := make(chan liveSub, 16)
	go func() {
		defer cancel()
		for {
			_, b, e := conn.Read(ctx)
			if e != nil {
				return
			}
			var s liveSub
			if json.Unmarshal(b, &s) != nil {
				s.Type = "invalid"
			}
			select {
			case input <- s:
			case <-ctx.Done():
				return
			}
		}
	}()
	subs := map[string]*liveSub{}
	defer func() {
		for _, s := range subs {
			if s.cancel != nil {
				s.cancel()
			}
		}
	}()
	drop := func(s *liveSub, why string) {
		m := s.message("error")
		m["error"] = why
		send(m)
		if s.cancel != nil {
			s.cancel()
		}
		delete(subs, s.ID)
	}
	remember := func(s *liveSub, records map[string]apiRecord) {
		s.last = map[string]json.RawMessage{}
		s.kinds = map[string]string{}
		for id, v := range records {
			s.last[id], _ = json.Marshal(v)
			s.kinds[id] = liveKind(v.Record)
		}
	}
	startStream := func(s *liveSub, r Record) {
		if s.cancel != nil {
			s.cancel()
		}
		streamCtx, stop := context.WithCancel(ctx)
		s.cancel = stop
		s.active = r.Active
		d, e := a.store.Get(r.Active)
		if e != nil || d.Container == "" || d.StaticDir != "" {
			return
		}
		go func() {
			cmd := exec.CommandContext(streamCtx, "docker", "logs", "--tail", "0", "-f", d.Container)
			cmd.Stdout = liveLogOutput{func(b []byte) {
				select {
				case c.events <- liveEvent{subscription: s, active: r.Active, text: string(b)}:
				case <-streamCtx.Done():
				default:
					cancel()
				}
			}}
			cmd.Stderr = cmd.Stdout
			_ = cmd.Run()
		}()
	}
	flush := time.NewTicker(150 * time.Millisecond)
	defer flush.Stop()
	interval := a.liveSessionInterval
	if interval == 0 {
		interval = 30 * time.Second
	}
	session := time.NewTicker(interval)
	defer session.Stop()
	identity := requestSession(r.Context())
	var expires <-chan time.Time
	if !identity.local {
		timer := time.NewTimer(time.Until(time.Unix(identity.expires, 0)))
		defer timer.Stop()
		expires = timer.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-expires:
			conn.Close(websocket.StatusCode(4408), "access token expired")
			return
		case <-session.C:
			if a.user(r) != uid {
				conn.Close(websocket.StatusCode(a.liveSessionClose(identity)), "reauthentication required")
				return
			}
			for _, s := range subs {
				if _, _, why := a.liveAccess(uid, s); why != "" {
					drop(s, why)
				}
			}
			go func() {
				pingCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				defer stop()
				if conn.Ping(pingCtx) != nil {
					cancel()
				}
			}()
		case req := <-input:
			a.touchSession(identity)
			if old := subs[req.ID]; old != nil {
				if old.cancel != nil {
					old.cancel()
				}
				delete(subs, req.ID)
			}
			if req.Type == "unsubscribe" {
				continue
			}
			if req.Type != "subscribe" || req.ID == "" || len(subs) >= 64 {
				drop(&req, "invalid subscription")
				continue
			}
			data, records, record, why := a.liveData(ctx, uid, &req)
			if why != "" {
				drop(&req, why)
				continue
			}
			s := &req
			s.logEnd = len(record.Log)
			subs[s.ID] = s
			remember(s, records)
			m := s.message("snapshot")
			m["data"] = data
			if !send(m) {
				return
			}
			delay := 5 * time.Second
			if s.Topic == "firewall" {
				delay = time.Second
			}
			if s.Topic == "system_update" {
				delay = 2 * time.Second
			}
			s.next = time.Now().Add(delay)
			b, _ := json.Marshal(data)
			s.update = string(b)
			if s.Topic == "container_log" {
				startStream(s, record)
			}
		case event := <-c.events:
			if event.id == "system_update" {
				for _, s := range subs {
					if s.Topic == "system_update" {
						s.next = time.Time{}
					}
				}
				continue
			}
			for _, s := range subs {
				if (s.Topic == "release_log" && s.ReleaseID == event.id) || (s.Topic == "container_log" && s == event.subscription && s.active == event.active) {
					if s.Topic == "release_log" && event.end <= s.logEnd {
						continue
					}
					text := event.text
					if s.Topic == "release_log" {
						if overlap := s.logEnd - (event.end - len(text)); overlap > 0 {
							text = text[overlap:]
						}
						s.logEnd = event.end
					}
					m := s.message("log")
					m["data"], m["reset"] = text, false
					if !send(m) {
						return
					}
				}
			}
		case now := <-flush.C:
			a.live.mu.Lock()
			pending := c.pending
			c.pending = map[string]liveEvent{}
			a.live.mu.Unlock()
			if _, changed := pending["account_sessions:"+uid]; changed && a.user(r) != uid {
				conn.Close(websocket.StatusCode(a.liveSessionClose(identity)), "reauthentication required")
				return
			}
			for _, event := range pending {
				if event.deleted {
					for _, s := range subs {
						if s.Topic == "project" && s.ProjectID == event.record.ID {
							m := s.message("delete")
							m["kind"], m["record_id"] = "project", event.record.ID
							if !send(m) {
								return
							}
							delete(subs, s.ID)
						}
					}
				}
			}
			for _, s := range subs {
				if s.Topic == "firewall" {
					e, has := pending["firewall:"+s.WorkspaceID]
					s.firewallDirty = s.firewallDirty || has
					if has && e.firewallConfig {
						s.next = time.Time{}
					}
				}
				periodic := s.Topic == "system_update" && now.After(s.next) || s.Topic == "traffic" && now.After(s.next)
				periodic = periodic || s.Topic == "firewall" && s.firewallDirty && !now.Before(s.next)
				key := s.activityKey(uid)
				switch s.Topic {
				case "system_router", "system_migrations", "recipes_catalog", "services_catalog":
					key = s.Topic
				case "uptime":
					key = "uptime:" + s.ProjectID
				case "status_page", "notifications":
					key = s.Topic + ":" + s.WorkspaceID
				}
				_, relevant := pending[key]
				for _, e := range pending {
					if s.relevant(e.record) || s.Topic == "status_page" && e.deleted && e.record.Kind == "projects" && e.record.WorkspaceID == s.WorkspaceID {
						relevant = true
						break
					}
				}
				if !relevant && !periodic {
					continue
				}
				if (s.Topic == "traffic" || s.Topic == "system_update") && !periodic {
					continue
				}
				data, records, r, why := a.liveData(ctx, uid, s)
				if why != "" {
					drop(s, why)
					continue
				}
				if key != "" || periodic || s.Topic == "certificates" || s.Topic == "services" || s.Topic == "volumes" || s.Topic == "firewall" || s.Topic == "backups" || s.Topic == "recipe_apps" {
					b, _ := json.Marshal(data)
					if s.Topic == "traffic" || string(b) != s.update {
						m := s.message("snapshot")
						m["data"] = data
						if !send(m) {
							return
						}
						s.update = string(b)
					}
					delay := 5 * time.Second
					if s.Topic == "firewall" {
						delay = time.Second
						s.firewallDirty = false
					}
					if s.Topic == "system_update" {
						delay = 2 * time.Second
					}
					s.next = now.Add(delay)
					if s.Topic == "firewall" {
						s.next = time.Now().Add(delay)
					}
					continue
				}
				if s.Topic == "container_log" {
					if r.Active != s.active {
						startStream(s, r)
						m := s.message("log")
						m["reset"] = true
						m["data"] = data.(map[string]string)["log"]
						if !send(m) {
							return
						}
					}
					continue
				}
				for id, v := range records {
					b, _ := json.Marshal(v)
					if string(b) != string(s.last[id]) {
						m := s.message("upsert")
						m["kind"], m["record"] = liveKind(v.Record), v
						if !send(m) {
							return
						}
					}
				}
				for id := range s.last {
					if _, ok := records[id]; !ok {
						m := s.message("delete")
						m["kind"], m["record_id"] = s.kinds[id], id
						if !send(m) {
							return
						}
					}
				}
				remember(s, records)
			}
		}
	}
}

type liveLogOutput struct{ write func([]byte) }

func (w liveLogOutput) Write(b []byte) (int, error) { w.write(b); return len(b), nil }
