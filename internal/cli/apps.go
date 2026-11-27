package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func (c *cli) runApps(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("use apps recipes, ls, install, show, start, stop, restart, update, delete, config or domains")
	}
	group, op := args[0], args[0]
	args = args[1:]
	switch group {
	case "recipes":
		if len(args) == 0 || (len(args) == 1 && args[0] == "list") {
			v, err := c.request("GET", "recipes", nil)
			if err != nil {
				return err
			}
			return c.printAccess(v, "id", "name", "version", "description")
		}
		if len(args) != 2 || args[0] != "show" {
			return fmt.Errorf("use apps recipes [list|show RECIPE]")
		}
		return c.appRequest("GET", "recipes/"+url.PathEscape(args[1]), nil)
	case "config", "domains":
		if len(args) == 0 {
			return fmt.Errorf("missing %s action", group)
		}
		op, args = group+" "+args[0], args[1:]
	}
	var flags string
	var count int
	switch op {
	case "ls", "list":
		count = 1
	case "install":
		count, flags = 3, "config ports"
	case "show", "start", "stop", "restart", "update", "domains list":
		count = 2
	case "delete":
		count, flags = 2, "purge"
	case "config get":
		count, flags = 2, "file"
	case "config set":
		count, flags = 2, "file config ports restart"
	case "domains add":
		count, flags = 4, "certificate force-https"
	case "domains rm":
		count = 3
	default:
		return fmt.Errorf("unknown apps action %q", op)
	}
	a, err := parseResourceArgs(args, flags)
	if err != nil {
		return err
	}
	v := a.positional
	if len(v) != count {
		return fmt.Errorf("incorrect arguments for apps %s; see startship help", op)
	}
	body := map[string]any{}
	for _, key := range []string{"config", "ports"} {
		if a.has(key) {
			var values map[string]any
			if err := json.Unmarshal([]byte(a.value(key)), &values); err != nil || values == nil {
				return fmt.Errorf("--%s requires a JSON object", key)
			}
			body[key] = values
		}
	}
	ws, err := c.resolve("workspaces", v[0])
	if err != nil {
		return err
	}
	collection := "workspaces/" + url.PathEscape(ws) + "/apps"
	switch op {
	case "ls", "list":
		return c.resourceList(collection, "apps", "id", "name", "status")
	case "install":
		body["recipe"], body["name"] = v[1], v[2]
		return c.appRequest("POST", collection, body)
	}
	app, err := c.findRecipeApp(collection, v[1])
	if err != nil {
		return err
	}
	path := collection + "/" + url.PathEscape(field(app, "id"))
	switch op {
	case "show":
		return c.print(app)
	case "start", "stop", "restart", "update":
		return c.appRequest("POST", path+"/"+op, nil)
	case "delete":
		if a.value("purge") == "true" {
			path += "?purge=true"
		}
		return c.appRequest("DELETE", path, nil)
	case "config get", "config set":
		method, suffix := "GET", "/config"
		if a.has("file") {
			suffix = "/files/" + url.PathEscape(a.value("file"))
		}
		if op == "config set" {
			if a.has("file") {
				if !a.has("config") || a.has("ports") {
					return fmt.Errorf("use --config JSON to edit a declared file")
				}
				method, body = "PUT", map[string]any{"values": body["config"], "restart": a.value("restart") == "true"}
			} else {
				if len(body) == 0 || a.has("restart") {
					return fmt.Errorf("use --config JSON or --ports JSON to edit install settings")
				}
				method = "PATCH"
			}
			return c.appRequest(method, path+suffix, body)
		}
		return c.appRequest(method, path+suffix, nil)
	case "domains list":
		return c.appRequest("GET", path+"/domains", nil)
	case "domains add":
		return c.appRequest("POST", path+"/domains", map[string]any{
			"endpoint": v[2], "host": v[3], "certificate_id": a.value("certificate"), "force_https": a.value("force-https") == "true",
		})
	case "domains rm":
		domains, _ := app["domains"].([]any)
		for _, row := range domains {
			if d, ok := row.(map[string]any); ok && (field(d, "id") == v[2] || strings.EqualFold(field(d, "host"), strings.TrimSuffix(v[2], "."))) {
				return c.appRequest("DELETE", path+"/domains/"+url.PathEscape(field(d, "id")), nil)
			}
		}
		return fmt.Errorf("domain %q not found", v[2])
	}
	return fmt.Errorf("unknown apps action %q", op)
}

func (c *cli) appRequest(method, path string, body any) error {
	v, err := c.request(method, path, body)
	if err != nil {
		return err
	}
	return c.print(v)
}

func (c *cli) findRecipeApp(path, name string) (map[string]any, error) {
	v, err := c.request("GET", path, nil)
	if err != nil {
		return nil, err
	}
	result, _ := v.(map[string]any)
	rows, _ := result["apps"].([]any)
	for _, row := range rows {
		m, _ := row.(map[string]any)
		if field(m, "id") == name || field(m, "name") == name {
			return m, nil
		}
	}
	return nil, fmt.Errorf("app %q not found", name)
}
