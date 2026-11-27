package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sammwyy/startship/internal/update"
	"github.com/sammwyy/startship/internal/version"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
)

type config struct {
	Server  string `json:"server"`
	Token   string `json:"token"`
	Refresh string `json:"refresh,omitempty"`
}
type cli struct {
	config
	path                   string
	pathErr                error
	in                     *bufio.Reader
	out                    io.Writer
	client                 *http.Client
	raw                    bool
	socketPermissionDenied bool
	updates                *update.Client
	binDir                 string
	restart                func() (bool, error)
}

func (c *cli) transport() (string, *http.Client, error) {
	server := c.Server
	if server == "" {
		path := os.Getenv("STARTSHIP_SOCKET")
		if path == "" {
			path = "/run/startship/startship.sock"
		}
		if _, err := os.Stat(path); err == nil || errors.Is(err, os.ErrPermission) {
			conn, err := net.DialTimeout("unix", path, time.Second)
			if err == nil {
				conn.Close()
				server = "unix://" + path
			} else if errors.Is(err, syscall.EACCES) {
				c.socketPermissionDenied = true
			}
		}
		if server == "" {
			server = "http://localhost:8080"
		}
	}
	u, err := url.Parse(server)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", nil, errors.New("server must be an http(s) URL or unix:///path without credentials, query or fragment")
	}
	if u.Scheme == "unix" && u.Host == "" && filepath.IsAbs(u.Path) {
		client := *c.client
		client.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", u.Path)
		}}
		return "http://startship", &client, nil
	}
	if u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", nil, errors.New("server must be an http(s) URL or unix:///path")
	}
	return strings.TrimRight(server, "/"), c.client, nil
}

func (c *cli) save() error {
	if c.pathErr != nil {
		return c.pathErr
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(c.config); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), c.path)
}

func (c *cli) request(method, path string, body any) (result any, requestErr error) {
	defer func() {
		if requestErr != nil && c.socketPermissionDenied {
			requestErr = fmt.Errorf("%w; run as root or join the startship group (usermod -aG startship $USER), or startship login", requestErr)
		}
	}()
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.doAPI(method, path, b)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode >= 300 {
		if m, ok := result.(map[string]any); ok {
			if msg, ok := m["error"].(string); ok {
				return nil, fmt.Errorf("%s: %s", resp.Status, msg)
			}
		}
		return nil, fmt.Errorf("API: %s", resp.Status)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid API response: %w", err)
	}
	if err := c.readCredentials(resp); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *cli) doAPI(method, path string, body []byte) (*http.Response, error) {
	server, client, err := c.transport()
	if err != nil {
		return nil, err
	}
	send := func(method, path string, body []byte) (*http.Response, error) {
		r, err := http.NewRequest(method, server+"/api/"+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		r.Header.Set("User-Agent", "Startship-CLI/"+version.Version)
		r.Header.Set("Content-Type", "application/json")
		if strings.HasPrefix(c.Token, "sst_") {
			r.Header.Set("Authorization", "Bearer "+c.Token)
		} else if c.Token != "" && path != "refresh" {
			r.AddCookie(&http.Cookie{Name: "startship_session", Value: c.Token})
		}
		if c.Refresh != "" && (path == "refresh" || path == "logout") {
			r.AddCookie(&http.Cookie{Name: "startship_refresh", Value: c.Refresh})
		}
		return client.Do(r)
	}
	resp, err := send(method, path, body)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || c.Refresh == "" || path == "login" || path == "logout" || path == "refresh" {
		return resp, err
	}
	resp.Body.Close()
	if err = c.renewAccess(); err != nil {
		return nil, err
	}
	return send(method, path, body)
}

func (c *cli) renewAccess() error {
	if c.Refresh == "" {
		return errors.New("session expired; run startship login again")
	}
	_, err := c.request("POST", "refresh", nil)
	if err != nil {
		return fmt.Errorf("renew access (sign in again if the session was ended): %w", err)
	}
	return nil
}

func (c *cli) readCredentials(resp *http.Response) error {
	changed := false
	for _, cookie := range resp.Cookies() {
		value := cookie.Value
		if cookie.MaxAge < 0 {
			value = ""
		}
		switch cookie.Name {
		case "startship_session":
			c.Token, changed = value, true
		case "startship_refresh":
			c.Refresh, changed = value, true
		}
	}
	if changed {
		return c.save()
	}
	return nil
}

func (c *cli) prompt(label string, secret bool) (string, error) {
	fmt.Fprint(c.out, label+": ")
	if secret {
		cmd := exec.Command("stty", "-echo")
		cmd.Stdin = os.Stdin
		if cmd.Run() == nil {
			defer func() { cmd := exec.Command("stty", "echo"); cmd.Stdin = os.Stdin; _ = cmd.Run(); fmt.Fprintln(c.out) }()
		}
	}
	s, err := c.in.ReadString('\n')
	if err == io.EOF && s != "" {
		err = nil
	}
	return strings.TrimRight(s, "\r\n"), err
}

func (c *cli) resolve(kind, value string) (string, error) {
	v, err := c.request("GET", kind, nil)
	if err != nil {
		return "", err
	}
	rows, ok := v.([]any)
	if !ok {
		return "", errors.New("expected API list")
	}
	id := ""
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		if m["id"] == value {
			return value, nil
		}
		nameKey := "name"
		if kind == "users" {
			nameKey = "username"
		}
		if m[nameKey] == value {
			if id != "" {
				return "", fmt.Errorf("ambiguous %s name %q; use an ID", kind, value)
			}
			id, _ = m["id"].(string)
		}
	}
	if id == "" {
		return "", fmt.Errorf("%s %q not found", kind, value)
	}
	return id, nil
}

func (c *cli) print(v any) error {
	if c.raw {
		e := json.NewEncoder(c.out)
		e.SetIndent("", "  ")
		return e.Encode(v)
	}
	if rows, ok := v.([]any); ok {
		w := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tSTATUS\tHOST")
		for _, row := range rows {
			m, ok := row.(map[string]any)
			if !ok {
				continue
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", field(m, "id"), field(m, "name"), field(m, "status"), field(m, "host"))
		}
		return w.Flush()
	}
	if m, ok := v.(map[string]any); ok {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(c.out, "%s: %v\n", k, m[k])
		}
		return nil
	}
	return json.NewEncoder(c.out).Encode(v)
}
func field(m map[string]any, k string) string { s, _ := m[k].(string); return s }

const usage = `startship [--json] COMMAND
  config [--config PATH] path|show|get KEY|set KEY VALUE...|unset KEY
                                 edit daemon TOML, not saved login settings
  users list|create USER|rename USER NAME|reset-password USER|delete USER
  roles list|get ROLE|create NAME|edit ROLE|delete ROLE
    --clone-of ROLE (create only) --name NAME (edit) --color HEX --icon ICON
    --perm p,...                  repeatable; replaces permissions; '' clears
  permissions                    permission catalog and role icons
  grant USER ROLE [--workspace W | --project P]
  revoke USER ROLE [--workspace W | --project P]
  assignments list [--user U] [--workspace W | --project P]
  members WORKSPACE | workspaces members WORKSPACE
  overrides list workspace|project TARGET
  overrides set workspace|project TARGET --role R|--user U [--allow p,...] [--deny p,...]
                                 empty allow/deny clears the override
  effective [USER] [--workspace W | --project P]
  audit [--workspace W]
  login [SERVER]                 prompt for username and password
  logout | whoami | passwd       session and account management
  account update [USERNAME] [--display-name NAME] [--email EMAIL]
                                 prompts for current password
  account sessions | account revoke SESSION
  account activity [--follow]    your recent actions
  account tokens                 personal access tokens for the API, CLI and MCP
  account token create NAME [--workspaces W1,W2 | --all] [--admin]
                    [--expires DAYS] [--no-api] [--no-mcp]
  account token revoke TOKEN_ID
  activity workspace|project TARGET [--follow]
  workspaces list|get ID|create NAME|rename ID NAME|delete ID
  projects list [WORKSPACE] | projects get ID|create NAME WORKSPACE|delete ID
  projects update ID JSON        project settings (same fields as API)
  projects sleep ID | projects wake ID
  projects update ID --previews on|off
  previews PROJECT | previews remove PROJECT PREVIEW_ID
  projects update ID '{"sleep_after":10}'  sleep after idle minutes (0 = off)
  projects source ID JSON        git_url, branch, git_token, build defaults
  projects env get ID | projects env set ID KEY=VALUE...
  deploy PROJECT [--wait] [--no-cache]
  redeploy PROJECT [RELEASE] [--wait]
  deployments list [PROJECT] | deployments get RELEASE
  logs PROJECT | logs --container CONTAINER | logs --deployment RELEASE
  containers list [PROJECT] | containers get ID
  routes list [PROJECT] | routes add PROJECT CONTAINER HOST [PATH] | routes rm ID
  routes update ID JSON          edit an existing route
  catalog [refresh [WORKSPACE]]   service engines, versions and ports
  apps recipes [list|show RECIPE|refresh [WORKSPACE]]
  apps ls WORKSPACE | apps show WORKSPACE APP
  apps install WORKSPACE RECIPE NAME [--config JSON] [--ports JSON]
  apps start|stop|restart|update WORKSPACE APP
  apps delete WORKSPACE APP [--purge]
  apps config get WORKSPACE APP [--file ID]
  apps config set WORKSPACE APP [--config JSON] [--ports JSON] [--file ID --restart]
  apps domains list WORKSPACE APP
  apps domains add WORKSPACE APP ENDPOINT HOST [--certificate ID] [--force-https]
  apps domains rm WORKSPACE APP HOST
  services [list] WORKSPACE
  services create WORKSPACE ENGINE NAME [--version V] [--set KEY=VALUE]... [--port N] [--volume ID]
  services show ID | services patch ID [--version V] [--set KEY=VALUE]... [--port N]
  services start|stop|restart ID | services logs ID [--tail N]
  services delete ID [--delete-volume]  keeps data by default
  services attach ID PROJECT ALIAS | services detach ID PROJECT
  services connections PROJECT   attached services, connection env and available services
  volumes [list] WORKSPACE
  volumes create WORKSPACE NAME [--project P --mount /path]
  volumes attach ID PROJECT /path | volumes detach ID | volumes delete ID
  backups [list] WORKSPACE [--service ID]
  backups create SERVICE   copy the service volume into the workspace backups volume now
  backups schedule SERVICE --every hourly|daily|weekly [--at HH:MM] [--minute M] [--weekday mon] [--keep N] | --off   times in UTC
  backups restore ID [--service ID]   takes a safety backup, then replaces the service data
  backups delete ID | backups download ID [--output FILE]
  domains list WORKSPACE
  domains add WORKSPACE DOMAIN [--subdomains]
  domains rm WORKSPACE DOMAIN     domain ID or name; delete/remove also accepted
  firewall show WS | firewall export WS | firewall apply WS FILE.json
  firewall mode WS off|monitor|enforce
  firewall managed WS RULE on|off [--action block|allow|log|ban]
  firewall limits WS [--rate N/DUR] [--rate-action block|ban] [--ban DUR]
    [--concurrent N] [--max-body SIZE] [--max-url SIZE]
  firewall rules list WS
  firewall rules add WS --name NAME --when 'FIELD OP VALUE' [--when ...]
    [--any] --action block|allow|log|rate_limit|ban [--rate N/DUR] [--ban DUR]
                                 'in' values are comma-separated; rate is per IP
  firewall rules rm|enable|disable WS RULE
  firewall rules move WS RULE POS positions are 1-based
  firewall lists list WS | firewall lists add WS NAME [--kind block|allow]
  firewall lists rm WS LIST
  firewall lists entry add WS LIST VALUE [--for DUR]
  firewall lists entry rm WS LIST VALUE
  network status | network list
  network block VALUE [--for DUR] [--router-only] [--note TEXT]
  network unblock ID|VALUE       IP, CIDR or ISO-2 country; platform permission
  firewall ban WS IP [--for DUR] | firewall unban WS IP
  firewall events WS [--follow]   live snapshots over HTTP(S) or Unix socket
                                 --json --follow prints new events as NDJSON
                                 durations: 30s/15m/1h/7d; sizes: 10MB/512KB
  workspaces domains ...         alias for domains; quote wildcard names
  traffic PROJECT | detect JSON | version
  uptime PROJECT [enable|disable] project health history and incidents
  notifications list|log WORKSPACE
  notifications add WORKSPACE --name NAME --kind discord|slack|webhook --url URL [--events EVENT,...] [--enable]
  notifications update WORKSPACE CHANNEL_ID [--name NAME] [--kind KIND] [--url URL] [--events EVENT,...] [--enable|--disable]
  notifications remove|test WORKSPACE CHANNEL_ID
  router [show]                  show active router settings and startup overrides
  router set [--http ADDR[,ADDR]] [--https ADDR[,ADDR]]
  router disable | router enable  apply and save changes without a restart (Platform Admin)
  status-page show WORKSPACE
  status-page set WORKSPACE [--slug SLUG] [--title TITLE] [--projects ID,NAME,...] [--enable|--disable]
  update check                   CLI, daemon and latest release versions
  update [VERSION]               install the latest (or VERSION) CLI and daemon
                                 beside this binary and restart startshipd (root)
  update daemon [VERSION]        ask the connected daemon to update itself
                                 (Platform Admin; needs the installer's updater)
Names or IDs accepted for workspaces/projects/containers. JSON objects may be
passed as '-' to read stdin. STARTSHIP_SERVER and STARTSHIP_TOKEN override config.
Without a saved server, the local admin socket needs no login (root/startship group).
STARTSHIP_SOCKET overrides /run/startship/startship.sock; unix:///path is also accepted.
If unavailable, uses http://localhost:8080. Use login URL for remote servers.
`

func (c *cli) run(args []string) error {
	if len(args) > 0 && args[0] == "config" {
		return c.runConfig(args[1:])
	}
	var a []string
	wait, noCache := false, false
	for _, s := range args {
		switch s {
		case "--json":
			c.raw = true
		case "--wait":
			wait = true
		case "--no-cache":
			noCache = true
		default:
			a = append(a, s)
		}
	}
	if len(a) == 0 || a[0] == "help" || a[0] == "--help" {
		fmt.Fprint(c.out, usage)
		return nil
	}
	cmd := a[0]
	a = a[1:]
	if cmd == "config" {
		return c.runConfig(a)
	}
	if cmd == "update" {
		return c.runUpdate(a)
	}
	if cmd == "domains" {
		return c.runDomains(a)
	}
	if cmd == "firewall" {
		return c.runFirewall(a)
	}
	if cmd == "network" {
		return c.runNetwork(a)
	}
	if cmd == "router" {
		return c.runRouter(a)
	}
	if cmd == "catalog" {
		return c.runCatalog(a)
	}
	if cmd == "services" {
		return c.runServices(a)
	}
	if cmd == "apps" {
		return c.runApps(a)
	}
	if cmd == "volumes" {
		return c.runVolumes(a)
	}
	if cmd == "backups" {
		return c.runBackups(a)
	}
	if cmd == "workspaces" && len(a) > 0 && a[0] == "domains" {
		return c.runDomains(a[1:])
	}
	if cmd == "account" {
		return c.runAccount(a)
	}
	if cmd == "activity" {
		return c.runActivity(a)
	}
	if handled, err := c.runAccess(cmd, a); handled {
		return err
	}
	method, path := "GET", ""
	var body any
	need := func(n int) error {
		if len(a) != n {
			return fmt.Errorf("invalid arguments; run startship help")
		}
		return nil
	}
	object := func(s string) (map[string]any, error) {
		var m map[string]any
		if s == "-" {
			b, e := io.ReadAll(c.in)
			if e != nil {
				return nil, e
			}
			s = string(b)
		}
		if e := json.Unmarshal([]byte(s), &m); e != nil {
			return nil, e
		}
		if m == nil {
			return nil, errors.New("expected JSON object")
		}
		return m, nil
	}
	switch cmd {
	case "previews":
		remove := len(a) > 0 && (a[0] == "remove" || a[0] == "rm")
		if remove {
			a = a[1:]
		}
		if len(a) != 1 && !remove || remove && len(a) != 2 {
			return errors.New("use previews PROJECT or previews remove PROJECT PREVIEW_ID")
		}
		id, e := c.resolve("projects", a[0])
		if e != nil {
			return e
		}
		path = "projects/" + url.PathEscape(id) + "/previews"
		if remove {
			method = "DELETE"
			path += "/" + url.PathEscape(a[1])
		}
		if !remove {
			v, e := c.request("GET", path, nil)
			if e != nil {
				return e
			}
			if c.raw {
				return c.print(v)
			}
			w := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tBRANCH\tSTATUS\tADDRESS\tCOMMIT")
			rows, _ := v.([]any)
			for _, row := range rows {
				m, _ := row.(map[string]any)
				addresses, _ := m["addresses"].([]any)
				parts := []string{}
				for _, address := range addresses {
					parts = append(parts, fmt.Sprint(address))
				}
				status := field(m, "status")
				if m["sleeping"] == true {
					status = "sleeping"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", field(m, "id"), field(m, "branch"), status, strings.Join(parts, ","), field(m, "commit"))
			}
			return w.Flush()
		}
	case "version":
		fmt.Fprintln(c.out, version.Version)
		return nil
	case "login":
		if len(a) > 1 {
			return need(1)
		}
		if len(a) == 1 {
			c.Server = a[0]
			c.Token, c.Refresh = "", ""
		}
		if c.Server == "" {
			c.Server = "http://localhost:8080"
		}
		u, e := c.prompt("Username", false)
		if e != nil {
			return e
		}
		p, e := c.prompt("Password", true)
		if e != nil {
			return e
		}
		method, path, body = "POST", "login", map[string]string{"username": u, "password": p}
	case "logout":
		if e := need(0); e != nil {
			return e
		}
		if c.Server == "" {
			c.Token, c.Refresh = "", ""
			return c.save()
		}
		method, path = "POST", "logout"
	case "whoami":
		if e := need(0); e != nil {
			return e
		}
		path = "me"
	case "passwd":
		if e := need(0); e != nil {
			return e
		}
		p, e := c.prompt("Current password", true)
		if e != nil {
			return e
		}
		n, e := c.prompt("New password", true)
		if e != nil {
			return e
		}
		method, path, body = "PATCH", "me", map[string]string{"current_password": p, "new_password": n}
	case "uptime":
		return c.runUptime(a)
	case "status-page":
		return c.runStatusPage(a)
	case "notifications":
		return c.runNotifications(a)
	case "deploy", "redeploy", "traffic", "logs":
		if len(a) == 0 {
			return errors.New("project or log target required")
		}
		if cmd == "logs" && len(a) == 2 && (a[0] == "--container" || a[0] == "--deployment") {
			if a[0] == "--container" {
				id, e := c.resolve("containers", a[1])
				if e != nil {
					return e
				}
				path = "containers/" + url.PathEscape(id) + "/logs"
			} else {
				path = "releases/" + url.PathEscape(a[1])
			}
		} else {
			if len(a) > 1 && !(cmd == "redeploy" && len(a) == 2) {
				return errors.New("unexpected arguments")
			}
			id, e := c.resolve("projects", a[0])
			if e != nil {
				return e
			}
			path = "projects/" + url.PathEscape(id)
			switch cmd {
			case "traffic":
				path += "/traffic"
			case "logs":
				v, e := c.request("GET", "releases?project_id="+url.QueryEscape(id)+"&logs=false", nil)
				if e != nil {
					return e
				}
				rows, ok := v.([]any)
				if !ok {
					return errors.New("expected API list")
				}
				if len(rows) == 0 {
					return c.print(map[string]any{"log": "No project deployment yet."})
				}
				latest, _ := rows[0].(map[string]any)
				for _, row := range rows[1:] {
					m, _ := row.(map[string]any)
					if field(m, "created") > field(latest, "created") {
						latest = m
					}
				}
				path = "releases/" + url.PathEscape(field(latest, "id"))
			default:
				method = "POST"
				path += "/" + cmd
				m := map[string]any{"no_cache": noCache}
				if len(a) == 2 {
					m["deployment_id"] = a[1]
				}
				body = m
			}
		}
	case "detect":
		if e := need(1); e != nil {
			return e
		}
		m, e := object(a[0])
		if e != nil {
			return e
		}
		method, path, body = "POST", "detect", m
	case "workspaces", "projects", "containers", "deployments", "routes":
		kind := cmd
		if kind == "deployments" {
			kind = "releases"
		}
		if len(a) == 0 {
			a = []string{"list"}
		}
		op := a[0]
		a = a[1:]
		if kind == "projects" && op == "update" && len(a) == 3 && a[1] == "--previews" {
			if a[2] != "on" && a[2] != "off" {
				return errors.New("--previews expects on or off")
			}
			id, e := c.resolve(kind, a[0])
			if e != nil {
				return e
			}
			path, method, body = kind+"/"+url.PathEscape(id), "PATCH", map[string]any{"previews": a[2] == "on"}
			break
		}
		path = kind
		if op == "list" {
			if len(a) > 1 || (kind == "workspaces" && len(a) > 0) {
				return errors.New("unexpected list arguments")
			}
			if len(a) == 1 {
				parent, query := "projects", "project_id"
				if kind == "projects" {
					parent, query = "workspaces", "workspace_id"
				}
				id, e := c.resolve(parent, a[0])
				if e != nil {
					return e
				}
				path += "?" + query + "=" + url.QueryEscape(id)
			}
			break
		}
		if kind == "projects" && op == "env" {
			if len(a) < 2 {
				return errors.New("use projects env get ID or set ID KEY=VALUE...")
			}
			mode := a[0]
			id, e := c.resolve(kind, a[1])
			if e != nil {
				return e
			}
			path += "/" + url.PathEscape(id)
			if mode == "get" && len(a) == 2 {
				v, e := c.request("GET", path, nil)
				if e != nil {
					return e
				}
				m, _ := v.(map[string]any)
				env := m["env"]
				if env == nil {
					env = map[string]any{}
				}
				return c.print(env)
			}
			if mode != "set" {
				return errors.New("unknown env action")
			}
			env := map[string]string{}
			for _, s := range a[2:] {
				k, v, ok := strings.Cut(s, "=")
				if !ok || k == "" {
					return errors.New("expected KEY=VALUE")
				}
				env[k] = v
			}
			method, body = "PATCH", map[string]any{"env": env}
			break
		}
		if op == "create" && kind == "workspaces" {
			if e := need(1); e != nil {
				return e
			}
			method, body = "POST", map[string]string{"name": a[0]}
			break
		}
		if op == "create" && kind == "projects" {
			if e := need(2); e != nil {
				return e
			}
			id, e := c.resolve("workspaces", a[1])
			if e != nil {
				return e
			}
			method, body = "POST", map[string]string{"name": a[0], "workspace_id": id}
			break
		}
		if op == "add" && kind == "routes" {
			if len(a) < 3 || len(a) > 4 {
				return errors.New("use routes add PROJECT CONTAINER HOST [PATH]")
			}
			p, e := c.resolve("projects", a[0])
			if e != nil {
				return e
			}
			id, e := c.resolve("containers", a[1])
			if e != nil {
				return e
			}
			prefix := "/"
			if len(a) == 4 {
				prefix = a[3]
			}
			method, body = "POST", map[string]string{"project_id": p, "container_id": id, "host": a[2], "path_prefix": prefix}
			break
		}
		if len(a) == 0 {
			return errors.New("ID required")
		}
		id := a[0]
		if kind == "projects" || kind == "workspaces" || kind == "containers" {
			var e error
			id, e = c.resolve(kind, id)
			if e != nil {
				return e
			}
		}
		path += "/" + url.PathEscape(id)
		switch {
		case kind == "projects" && (op == "sleep" || op == "wake"):
			if e := need(1); e != nil {
				return e
			}
			method, path = "POST", path+"/"+op
		case op == "get":
			if e := need(1); e != nil {
				return e
			}
		case (op == "delete" || op == "rm") && kind != "containers" && kind != "releases":
			if e := need(1); e != nil {
				return e
			}
			method = "DELETE"
		case op == "rename" && kind == "workspaces":
			if e := need(2); e != nil {
				return e
			}
			method, body = "PATCH", map[string]string{"name": a[1]}
		case (op == "update" && (kind == "projects" || kind == "routes")) || (op == "source" && kind == "projects"):
			if e := need(2); e != nil {
				return e
			}
			m, e := object(a[1])
			if e != nil {
				return e
			}
			method, body = "PATCH", m
		default:
			return errors.New("unsupported action; run startship help")
		}
	default:
		return fmt.Errorf("unknown command %q; run startship help", cmd)
	}
	v, err := c.request(method, path, body)
	if err != nil {
		return err
	}
	if wait && (cmd == "deploy" || cmd == "redeploy") {
		m, _ := v.(map[string]any)
		id := field(m, "id")
		if id == "" {
			return errors.New("deployment response missing ID")
		}
		for {
			v, err = c.request("GET", "releases/"+url.PathEscape(id), nil)
			if err != nil {
				return err
			}
			m, _ = v.(map[string]any)
			switch field(m, "status") {
			case "running", "superseded":
				return c.print(v)
			case "failed":
				_ = c.print(v)
				return errors.New("deployment failed")
			}
			time.Sleep(time.Second)
		}
	}
	if cmd == "logs" && !c.raw {
		m, _ := v.(map[string]any)
		fmt.Fprintln(c.out, field(m, "log"))
		return nil
	}
	return c.print(v)
}

func Run() {
	c := &cli{in: bufio.NewReader(os.Stdin), out: os.Stdout, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if len(os.Args) > 1 && os.Args[1] == "config" {
		if err := c.runConfig(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "startship:", err)
			os.Exit(1)
		}
		return
	}
	// Services such as startship-update.service run without $HOME; only commands that save login state need it.
	dir, err := os.UserConfigDir()
	if err == nil {
		c.path = filepath.Join(dir, "startship", "config.json")
		var b []byte
		if b, err = os.ReadFile(c.path); err == nil {
			err = json.Unmarshal(b, &c.config)
		}
	} else {
		c.pathErr, err = err, nil
	}
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if s := os.Getenv("STARTSHIP_SERVER"); s != "" && s != c.Server {
		c.Server, c.Token, c.Refresh = s, "", ""
	}
	if s := os.Getenv("STARTSHIP_TOKEN"); s != "" {
		c.Token, c.Refresh = s, ""
	}
	if err = c.run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "startship:", err)
		os.Exit(1)
	}
}
