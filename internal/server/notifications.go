package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	uptimeDown      = "uptime.down"
	uptimeRecovered = "uptime.recovered"
	deployFailed    = "deploy.failed"
	deploySucceeded = "deploy.succeeded"
)

type notificationChannel struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Host      string   `json:"host"`
	Events    []string `json:"events"`
	Enabled   bool     `json:"enabled"`
	CreatedAt string   `json:"created_at"`
	target    string
}

type notificationPatch struct {
	Name    *string   `json:"name"`
	Kind    *string   `json:"kind"`
	URL     *string   `json:"url"`
	Events  *[]string `json:"events"`
	Enabled *bool     `json:"enabled"`
}

type notificationDelivery struct {
	ID         int64  `json:"id"`
	ChannelID  string `json:"channel_id"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"http_status"`
	Error      string `json:"error"`
	CreatedAt  string `json:"created_at"`
}

type notificationSnapshot struct {
	Channels   []notificationChannel  `json:"channels"`
	Deliveries []notificationDelivery `json:"deliveries"`
}

func notificationURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 4096 {
		return nil, errors.New("enter a valid HTTP or HTTPS webhook URL without credentials or a fragment")
	}
	return u, nil
}

func (c *notificationChannel) apply(p notificationPatch) error {
	if p.Name != nil {
		c.Name = strings.TrimSpace(*p.Name)
	}
	if c.Name == "" || len(c.Name) > 120 {
		return errors.New("channel name must be between 1 and 120 bytes")
	}
	if p.Kind != nil {
		c.Kind = *p.Kind
	}
	if c.Kind != "discord" && c.Kind != "slack" && c.Kind != "webhook" {
		return errors.New("choose discord, slack, or webhook")
	}
	if p.URL != nil {
		c.target = strings.TrimSpace(*p.URL)
	}
	u, err := notificationURL(c.target)
	if err != nil {
		return err
	}
	c.Host = u.Scheme + "://" + u.Host
	if p.Events != nil {
		c.Events = []string{}
		for _, event := range *p.Events {
			switch event {
			case uptimeDown, uptimeRecovered, deployFailed, deploySucceeded:
				if !slices.Contains(c.Events, event) {
					c.Events = append(c.Events, event)
				}
			default:
				return errors.New("unknown notification event")
			}
		}
	}
	if p.Enabled != nil {
		c.Enabled = *p.Enabled
	}
	return nil
}

type notificationScanner interface{ Scan(...any) error }

const notificationChannelColumns = "id, name, kind, url, events, enabled, created_at"

func scanNotificationChannel(row notificationScanner) (notificationChannel, error) {
	c := notificationChannel{Events: []string{}}
	var events string
	if err := row.Scan(&c.ID, &c.Name, &c.Kind, &c.target, &events, &c.Enabled, &c.CreatedAt); err != nil {
		return c, err
	}
	if u, err := notificationURL(c.target); err == nil {
		c.Host = u.Scheme + "://" + u.Host
	}
	return c, json.Unmarshal([]byte(events), &c.Events)
}

func (a *Server) notificationChannels(workspace string) ([]notificationChannel, error) {
	rows, err := a.store.DB.Query("SELECT "+notificationChannelColumns+" FROM notification_channels WHERE workspace_id=? ORDER BY created_at,id", workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notificationChannel{}
	for rows.Next() {
		c, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (a *Server) notificationDeliveries(workspace string) ([]notificationDelivery, error) {
	rows, err := a.store.DB.Query("SELECT id,channel_id,event,status,http_status,error,created_at FROM notification_deliveries WHERE workspace_id=? ORDER BY id DESC LIMIT 50", workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notificationDelivery{}
	for rows.Next() {
		var d notificationDelivery
		if err := rows.Scan(&d.ID, &d.ChannelID, &d.Event, &d.Status, &d.HTTPStatus, &d.Error, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (a *Server) notificationsData(workspace string) (notificationSnapshot, error) {
	var out notificationSnapshot
	var err error
	out.Channels, err = a.notificationChannels(workspace)
	if err != nil {
		return out, err
	}
	out.Deliveries, err = a.notificationDeliveries(workspace)
	return out, err
}

func (a *Server) notificationsChanged(workspace string) {
	a.live.publish(liveEvent{changeKey: "notifications:" + workspace})
}

func (a *Server) workspaceNotifications(w http.ResponseWriter, r *http.Request, ws Record, x *access, path []string) {
	if !x.can(ws, "workspace.manage") {
		forbidden(w)
		return
	}
	if len(path) == 1 && path[0] == "deliveries" {
		if r.Method != http.MethodGet {
			fail(w, 405, "method not allowed")
			return
		}
		data, err := a.notificationDeliveries(ws.ID)
		if err != nil {
			fail(w, 500, "could not load notification deliveries")
			return
		}
		reply(w, 200, data)
		return
	}
	if len(path) == 0 || path[0] != "channels" || len(path) > 3 || (len(path) == 3 && path[2] != "test") {
		fail(w, 404, "not found")
		return
	}
	if len(path) == 1 && r.Method == http.MethodGet {
		data, err := a.notificationChannels(ws.ID)
		if err != nil {
			fail(w, 500, "could not load notification channels")
			return
		}
		reply(w, 200, data)
		return
	}
	create := len(path) == 1 && r.Method == http.MethodPost
	update := len(path) == 2 && r.Method == http.MethodPatch
	remove := len(path) == 2 && r.Method == http.MethodDelete
	test := len(path) == 3 && r.Method == http.MethodPost
	if !create && !update && !remove && !test {
		fail(w, 405, "method not allowed")
		return
	}
	var patch notificationPatch
	if (create || update) && decode(w, r, &patch) != nil {
		fail(w, 400, "invalid notification channel")
		return
	}
	c, status, err := a.mutateNotificationChannel(ws.ID, path, patch, create, remove, test)
	if err != nil {
		fail(w, status, err.Error())
		return
	}
	switch {
	case test:
		go a.deliverNotification(ws.ID, c, notificationPayload{Event: "test", Workspace: ws.Name, Message: "Startship test notification", At: time.Now().UTC()})
		reply(w, 202, map[string]bool{"queued": true})
	case remove:
		reply(w, 200, map[string]bool{"deleted": true})
	default:
		reply(w, status, c)
	}
}

func (a *Server) mutateNotificationChannel(workspace string, path []string, patch notificationPatch, create, remove, test bool) (notificationChannel, int, error) {
	a.store.Lock()
	defer a.store.Unlock()
	c := notificationChannel{ID: randomID(), Events: []string{}, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	var err error
	if !create {
		c, err = scanNotificationChannel(a.store.DB.QueryRow("SELECT "+notificationChannelColumns+" FROM notification_channels WHERE workspace_id=? AND id=?", workspace, path[1]))
		if errors.Is(err, sql.ErrNoRows) {
			return c, 404, errors.New("notification channel not found")
		}
		if err != nil {
			return c, 500, errors.New("could not load notification channel")
		}
	}
	if test {
		return c, 202, nil
	}
	status := 200
	if remove {
		_, err = a.store.DB.Exec("DELETE FROM notification_channels WHERE id=? AND workspace_id=?", c.ID, workspace)
	} else {
		if err = c.apply(patch); err != nil {
			return c, 400, err
		}
		events, _ := json.Marshal(c.Events)
		if create {
			status = 201
			var result sql.Result
			result, err = a.store.DB.Exec(`INSERT INTO notification_channels(id,workspace_id,name,kind,url,events,enabled,created_at)
				SELECT ?,id,?,?,?,?,?,? FROM records WHERE id=? AND kind='workspaces'`, c.ID, c.Name, c.Kind, c.target, string(events), c.Enabled, c.CreatedAt, workspace)
			if err == nil {
				if n, _ := result.RowsAffected(); n == 0 {
					return c, 404, errors.New("workspace not found")
				}
			}
		} else {
			_, err = a.store.DB.Exec("UPDATE notification_channels SET name=?,kind=?,url=?,events=?,enabled=? WHERE id=? AND workspace_id=?", c.Name, c.Kind, c.target, string(events), c.Enabled, c.ID, workspace)
		}
	}
	if err != nil {
		return c, 500, errors.New("could not save notification channel")
	}
	a.notificationsChanged(workspace)
	return c, status, nil
}

type notificationPayload struct {
	Event     string    `json:"event"`
	Workspace string    `json:"workspace"`
	Project   string    `json:"project"`
	Message   string    `json:"message"`
	At        time.Time `json:"at"`
}

func (a *Server) dispatchNotification(project Record, event string, at time.Time) {
	ws, err := a.store.Get(projectWorkspace(project))
	if err != nil || ws.Kind != "workspaces" {
		return
	}
	channels, err := a.notificationChannels(ws.ID)
	if err != nil {
		return
	}
	message := map[string]string{
		uptimeDown: "is down", uptimeRecovered: "has recovered",
		deployFailed: "deployment failed", deploySucceeded: "deployment succeeded",
	}[event]
	payload := notificationPayload{event, ws.Name, project.Name, project.Name + ": " + message, at}
	for _, c := range channels {
		if c.Enabled && slices.Contains(c.Events, event) {
			a.deliverNotification(ws.ID, c, payload)
		}
	}
}

var notificationClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{DialContext: dialNotification, DisableKeepAlives: true},
}

var notificationReservedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
}

func dialNotification(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	local := addr == os.Getenv("STARTSHIP_NOTIFICATION_LOCAL_HOST")
	dialer := net.Dialer{}
	for _, address := range addresses {
		ip := address.Unmap()
		reserved := slices.ContainsFunc(notificationReservedNetworks, func(p netip.Prefix) bool { return p.Contains(ip) })
		if !(local && ip.IsLoopback()) && (!ip.IsGlobalUnicast() || ip.IsPrivate() || reserved) {
			continue
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("webhook destination unavailable")
}

func (a *Server) deliverNotification(workspace string, c notificationChannel, payload notificationPayload) {
	var body any = payload
	switch c.Kind {
	case "discord":
		body = map[string]any{"content": "Startship notification", "embeds": []map[string]string{{"description": payload.Message}}}
	case "slack":
		message := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(payload.Message)
		body = map[string]string{"text": message}
	}
	content, _ := json.Marshal(body)
	status, reason, code := "failed", "could not reach webhook", 0
	if req, err := http.NewRequest(http.MethodPost, c.target, bytes.NewReader(content)); err == nil {
		req.Header.Set("Content-Type", "application/json")
		if resp, err := notificationClient.Do(req); err == nil {
			resp.Body.Close()
			code = resp.StatusCode
			if code >= 200 && code < 300 {
				status, reason = "ok", ""
			} else {
				reason = "webhook returned a non-success status"
			}
		}
	}
	result, err := a.store.DB.Exec(`INSERT INTO notification_deliveries(channel_id,workspace_id,event,status,http_status,error,created_at)
		SELECT id,workspace_id,?,?,?,?,? FROM notification_channels WHERE id=? AND workspace_id=?`,
		payload.Event, status, code, reason, time.Now().UTC().Format(time.RFC3339Nano), c.ID, workspace)
	if err == nil {
		if n, _ := result.RowsAffected(); n > 0 {
			a.notificationsChanged(workspace)
		}
	}
}
