package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func (c *cli) endpoint(value string) (map[string]string, error) {
	kind, ref, ok := strings.Cut(value, ":")
	if !ok || ref == "" || (kind != "project" && kind != "app" && kind != "service") {
		return nil, fmt.Errorf("%q must be project:REF, app:ID or service:ID", value)
	}
	if kind == "project" {
		id, err := c.resolve("projects", ref)
		if err != nil {
			return nil, err
		}
		ref = id
	}
	return map[string]string{"kind": kind, "id": ref}, nil
}

func (c *cli) runConnections(args []string) error {
	if len(args) == 1 && args[0] != "list" && args[0] != "create" && args[0] != "delete" {
		args = append([]string{"list"}, args...)
	}
	want := map[string]int{"list": 2, "create": 5, "delete": 2}
	if len(args) == 0 || want[args[0]] != len(args) {
		return errors.New("invalid connections arguments; run startship help")
	}
	switch args[0] {
	case "list":
		ws, err := c.resolve("workspaces", args[1])
		if err != nil {
			return err
		}
		return c.resourceList("workspaces/"+url.PathEscape(ws)+"/connections", "connections", "id", "alias", "from", "to")
	case "create":
		ws, err := c.resolve("workspaces", args[1])
		if err != nil {
			return err
		}
		from, err := c.endpoint(args[2])
		if err != nil {
			return err
		}
		to, err := c.endpoint(args[3])
		if err != nil {
			return err
		}
		v, err := c.request("POST", "workspaces/"+url.PathEscape(ws)+"/connections", map[string]any{"from": from, "to": to, "alias": args[4]})
		if err != nil {
			return err
		}
		if !c.raw {
			fmt.Fprintln(c.out, "Redeploy or restart the connecting resource to receive its environment variables.")
		}
		return c.print(v)
	}
	v, err := c.request("DELETE", "connections/"+url.PathEscape(args[1]), nil)
	if err != nil {
		return err
	}
	return c.print(v)
}

func (c *cli) runNetworks(args []string) error {
	ops := map[string]int{"list": 2, "create": 3, "rename": 3, "delete": 2, "join": 3, "leave": 3}
	if len(args) == 1 && ops[args[0]] == 0 {
		args = append([]string{"list"}, args...)
	}
	if len(args) == 0 || ops[args[0]] != len(args) {
		return errors.New("invalid networks arguments; run startship help")
	}
	op, path := args[0], "networks/"+url.PathEscape(args[1])
	var body any
	method := "DELETE"
	switch op {
	case "list", "create":
		ws, err := c.resolve("workspaces", args[1])
		if err != nil {
			return err
		}
		path = "workspaces/" + url.PathEscape(ws) + "/networks"
		if op == "list" {
			return c.resourceList(path, "networks", "id", "name", "members")
		}
		method, body = "POST", map[string]string{"name": args[2]}
	case "rename":
		method, body = "PATCH", map[string]string{"name": args[2]}
	case "join", "leave":
		e, err := c.endpoint(args[2])
		if err != nil {
			return err
		}
		path += "/members"
		if op == "join" {
			method, body = "POST", e
		} else {
			path += "/" + e["kind"] + "/" + url.PathEscape(e["id"])
		}
	}
	v, err := c.request(method, path, body)
	if err != nil {
		return err
	}
	return c.print(v)
}
