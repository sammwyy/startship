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
	Server string `json:"server"`
	Token  string `json:"token"`
}
type cli struct {
	config
	path                   string
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
	server, client, err := c.transport()
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequest(method, server+"/api/"+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		r.AddCookie(&http.Cookie{Name: "startship_session", Value: c.Token})
	}
	resp, err := client.Do(r)
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
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "startship_session" {
			c.Token = cookie.Value
			if cookie.MaxAge < 0 {
				c.Token = ""
			}
			if err := c.save(); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
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
  account update [USERNAME]      prompts for current password
  workspaces list|get ID|create NAME|rename ID NAME|delete ID
  projects list [WORKSPACE] | projects get ID|create NAME WORKSPACE|delete ID
  projects update ID JSON        project settings (same fields as API)
  projects source ID JSON        git_url, branch, git_token, build defaults
  projects env get ID | projects env set ID KEY=VALUE...
  deploy PROJECT [--wait] [--no-cache]
  redeploy PROJECT [RELEASE] [--wait]
  deployments list [PROJECT] | deployments get RELEASE
  logs PROJECT | logs --container CONTAINER | logs --deployment RELEASE
  containers list [PROJECT] | containers get ID
  routes list [PROJECT] | routes add PROJECT CONTAINER HOST [PATH] | routes rm ID
  routes update ID JSON          edit an existing route
  domains list WORKSPACE
  domains add WORKSPACE DOMAIN [--subdomains]
  domains rm WORKSPACE DOMAIN     domain ID or name; delete/remove also accepted
  workspaces domains ...         alias for domains; quote wildcard names
  traffic PROJECT | detect JSON | version
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
	if cmd == "update" {
		return c.runUpdate(a)
	}
	if cmd == "domains" {
		return c.runDomains(a)
	}
	if cmd == "workspaces" && len(a) > 0 && a[0] == "domains" {
		return c.runDomains(a[1:])
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
	case "version":
		fmt.Fprintln(c.out, version.Version)
		return nil
	case "login":
		if len(a) > 1 {
			return need(1)
		}
		if len(a) == 1 {
			c.Server = a[0]
			c.Token = ""
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
			c.Token = ""
			return c.save()
		}
		method, path = "POST", "logout"
	case "whoami":
		if e := need(0); e != nil {
			return e
		}
		path = "me"
	case "passwd", "account":
		m := map[string]any{}
		if cmd == "account" {
			if len(a) < 1 || a[0] != "update" || len(a) > 2 {
				return errors.New("use account update [USERNAME]")
			}
			if len(a) == 2 {
				m["username"] = a[1]
			}
		} else if e := need(0); e != nil {
			return e
		}
		p, e := c.prompt("Current password", true)
		if e != nil {
			return e
		}
		m["current_password"] = p
		if cmd == "passwd" {
			p, e = c.prompt("New password", true)
			if e != nil {
				return e
			}
			m["new_password"] = p
		}
		method, path, body = "PATCH", "me", m
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
	dir, err := os.UserConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	c := &cli{path: filepath.Join(dir, "startship", "config.json"), in: bufio.NewReader(os.Stdin), out: os.Stdout, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	b, err := os.ReadFile(c.path)
	if err == nil {
		err = json.Unmarshal(b, &c.config)
	}
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if s := os.Getenv("STARTSHIP_SERVER"); s != "" {
		c.Server = s
	}
	if s := os.Getenv("STARTSHIP_TOKEN"); s != "" {
		c.Token = s
	}
	if err = c.run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "startship:", err)
		os.Exit(1)
	}
}
