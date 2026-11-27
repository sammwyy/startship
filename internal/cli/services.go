package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func parseResourceArgs(args []string, allowed string) (accessArgs, error) {
	a := accessArgs{flags: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		s := args[i]
		if !strings.HasPrefix(s, "--") {
			a.positional = append(a.positional, s)
			continue
		}
		key, value, hasValue := strings.Cut(strings.TrimPrefix(s, "--"), "=")
		if !strings.Contains(" "+allowed+" ", " "+key+" ") || key == "" {
			return a, fmt.Errorf("unknown option --%s", key)
		}
		if (key == "delete-volume" || key == "purge" || key == "restart" || key == "force-https") && !hasValue {
			value = "true"
		} else if !hasValue {
			i++
			if i == len(args) || strings.HasPrefix(args[i], "--") {
				return a, fmt.Errorf("--%s requires a value", key)
			}
			value = args[i]
		}
		if a.has(key) && key != "set" {
			return a, fmt.Errorf("--%s may only be supplied once", key)
		}
		a.flags[key] = append(a.flags[key], value)
	}
	return a, nil
}

func (c *cli) resourceList(path, key string, columns ...string) error {
	v, err := c.request("GET", path, nil)
	if err != nil {
		return err
	}
	if c.raw {
		return c.print(v)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("expected %s response", key)
	}
	rows, ok := m[key].([]any)
	if !ok {
		return fmt.Errorf("expected %s list", key)
	}
	return c.printAccess(rows, columns...)
}

func (c *cli) runCatalog(args []string) error {
	if len(args) != 0 {
		return errors.New("use catalog without arguments")
	}
	v, err := c.request("GET", "catalog", nil)
	if err != nil {
		return err
	}
	return c.printAccess(v, "id", "name", "category", "versions", "port", "image")
}

func (c *cli) runServices(args []string) error {
	if len(args) == 1 && !strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "list", "create", "show", "patch", "start", "stop", "restart", "logs", "delete", "attach", "detach", "connections":
		default:
			args = append([]string{"list"}, args...)
		}
	}
	if len(args) == 0 {
		return errors.New("services action or workspace required; run startship help")
	}
	op := args[0]
	allowed := map[string]string{"list": "", "create": "version set port volume", "show": "", "patch": "version set port", "start": "", "stop": "", "restart": "", "logs": "tail", "delete": "delete-volume", "attach": "", "detach": "", "connections": ""}
	flags, ok := allowed[op]
	if !ok {
		return fmt.Errorf("unknown services action %q", op)
	}
	a, err := parseResourceArgs(args[1:], flags)
	if err != nil {
		return err
	}
	p := a.positional
	n := 1
	switch op {
	case "create", "attach":
		n = 3
	case "detach":
		n = 2
	}
	if len(p) != n {
		return errors.New("invalid services arguments; run startship help")
	}
	body := map[string]any{}
	if a.has("version") {
		if a.value("version") == "" {
			return errors.New("--version cannot be empty")
		}
		body["version"] = a.value("version")
	}
	if a.has("port") {
		port, err := strconv.Atoi(a.value("port"))
		if err != nil || port < 0 || port > 65535 {
			return errors.New("--port must be 0-65535 (0 disables external access)")
		}
		body["external_port"] = port
	}
	if a.has("set") {
		config := map[string]string{}
		for _, value := range a.flags["set"] {
			key, value, ok := strings.Cut(value, "=")
			if !ok || key == "" {
				return errors.New("--set requires KEY=VALUE")
			}
			config[key] = value
		}
		body["config"] = config
	}
	if op == "patch" && len(body) == 0 {
		return errors.New("provide --version, --set or --port to patch")
	}
	tail := 200
	if a.has("tail") {
		tail, err = strconv.Atoi(a.value("tail"))
		if err != nil || tail < 1 || tail > 10000 {
			return errors.New("--tail must be 1-10000")
		}
	}
	deleteVolume := false
	if a.has("delete-volume") {
		deleteVolume, err = strconv.ParseBool(a.value("delete-volume"))
		if err != nil {
			return errors.New("--delete-volume must be true or false")
		}
	}
	path, method := "services/"+url.PathEscape(p[0]), "GET"
	switch op {
	case "list", "create":
		ws, err := c.resolve("workspaces", p[0])
		if err != nil {
			return err
		}
		path = "workspaces/" + url.PathEscape(ws) + "/services"
		if op == "list" {
			return c.resourceList(path, "services", "id", "name", "engine", "version", "status", "external_port", "volume_id", "attachments")
		}
		method = "POST"
		body["engine"], body["name"] = p[1], p[2]
		if a.has("volume") {
			body["volume_id"] = a.value("volume")
		}
	case "patch":
		method = "PATCH"
	case "delete":
		method = "DELETE"
		if deleteVolume {
			path += "?delete_volume=true"
		}
	case "start", "stop", "restart":
		method, path = "POST", path+"/"+op
	case "logs":
		path += "/logs?tail=" + strconv.Itoa(tail)
	case "attach", "detach":
		project, err := c.resolve("projects", p[1])
		if err != nil {
			return err
		}
		path += "/attachments"
		method = "POST"
		if op == "attach" {
			body["project_id"], body["alias"] = project, p[2]
		} else {
			method, path = "DELETE", path+"/"+url.PathEscape(project)
		}
	case "connections":
		project, err := c.resolve("projects", p[0])
		if err != nil {
			return err
		}
		path = "projects/" + url.PathEscape(project) + "/services"
	}
	var payload any
	if method == "PATCH" || method == "POST" && (op == "create" || op == "attach") {
		payload = body
	}
	v, err := c.request(method, path, payload)
	if err != nil {
		return err
	}
	if op == "logs" && !c.raw {
		m, ok := v.(map[string]any)
		if !ok {
			return errors.New("expected service logs response")
		}
		log, ok := m["log"].(string)
		if !ok {
			return errors.New("expected service log text")
		}
		fmt.Fprint(c.out, log)
		return nil
	}
	if !c.raw && (op == "attach" || op == "detach") {
		fmt.Fprintln(c.out, "Redeploy the project to apply connection changes.")
	}
	return c.print(v)
}

func (c *cli) runVolumes(args []string) error {
	if len(args) == 1 && !strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "list", "create", "attach", "detach", "delete":
		default:
			args = append([]string{"list"}, args...)
		}
	}
	if len(args) == 0 {
		return errors.New("volumes action or workspace required; run startship help")
	}
	op := args[0]
	allowed := map[string]string{"list": "", "create": "project mount", "attach": "", "detach": "", "delete": ""}
	flags, ok := allowed[op]
	if !ok {
		return fmt.Errorf("unknown volumes action %q", op)
	}
	a, err := parseResourceArgs(args[1:], flags)
	if err != nil {
		return err
	}
	p := a.positional
	n := 1
	if op == "create" {
		n = 2
	} else if op == "attach" {
		n = 3
	}
	if len(p) != n {
		return errors.New("invalid volumes arguments; run startship help")
	}
	if a.has("project") != a.has("mount") || a.has("project") && (a.value("project") == "" || a.value("mount") == "") {
		return errors.New("--project and --mount must be supplied together and not empty")
	}
	path, method := "volumes/"+url.PathEscape(p[0]), "GET"
	body := map[string]string{}
	switch op {
	case "list", "create":
		ws, err := c.resolve("workspaces", p[0])
		if err != nil {
			return err
		}
		path = "workspaces/" + url.PathEscape(ws) + "/volumes"
		if op == "list" {
			return c.resourceList(path, "volumes", "id", "name", "project_id", "mount_path", "service_id", "size_bytes")
		}
		method, body["name"] = "POST", p[1]
		if a.has("project") {
			project, err := c.resolve("projects", a.value("project"))
			if err != nil {
				return err
			}
			body["project_id"], body["mount_path"] = project, a.value("mount")
		}
	case "attach":
		project, err := c.resolve("projects", p[1])
		if err != nil {
			return err
		}
		method, body["project_id"], body["mount_path"] = "PATCH", project, p[2]
	case "detach":
		method, body["project_id"], body["mount_path"] = "PATCH", "", ""
	case "delete":
		method = "DELETE"
	}
	var payload any
	if method == "POST" || method == "PATCH" {
		payload = body
	}
	v, err := c.request(method, path, payload)
	if err != nil {
		return err
	}
	if !c.raw && (op == "attach" || op == "detach" || op == "create" && a.has("project")) {
		fmt.Fprintln(c.out, "Redeploy the project to apply volume changes.")
	}
	return c.print(v)
}
