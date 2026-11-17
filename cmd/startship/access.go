package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
)

func (c *cli) accessPassword() (string, error) {
	if c.raw {
		out := c.out
		c.out = os.Stderr
		defer func() { c.out = out }()
	}
	return c.prompt("Temporary password", true)
}

type accessArgs struct {
	positional []string
	flags      map[string][]string
}

func parseAccessArgs(args []string, allowed string) (accessArgs, error) {
	out := accessArgs{flags: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		s := args[i]
		if !strings.HasPrefix(s, "--") {
			out.positional = append(out.positional, s)
			continue
		}
		key, value, hasValue := strings.Cut(strings.TrimPrefix(s, "--"), "=")
		if !strings.Contains(" "+allowed+" ", " "+key+" ") {
			return out, fmt.Errorf("unknown option --%s", key)
		}
		if !hasValue {
			i++
			if i == len(args) || strings.HasPrefix(args[i], "--") {
				return out, fmt.Errorf("--%s requires a value", key)
			}
			value = args[i]
		}
		if len(out.flags[key]) > 0 && key != "perm" {
			return out, fmt.Errorf("--%s may only be supplied once", key)
		}
		out.flags[key] = append(out.flags[key], value)
	}
	return out, nil
}
func (a accessArgs) value(key string) string {
	if len(a.flags[key]) == 0 {
		return ""
	}
	return a.flags[key][0]
}
func (a accessArgs) has(key string) bool { _, ok := a.flags[key]; return ok }
func permissionArgs(values []string) []string {
	out := []string{}
	for _, v := range values {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func (c *cli) accessScope(a accessArgs) (string, string, error) {
	if a.has("workspace") && a.has("project") {
		return "", "", errors.New("choose --workspace or --project, not both")
	}
	for _, scope := range []string{"workspace", "project"} {
		if a.has(scope) {
			if a.value(scope) == "" {
				return "", "", fmt.Errorf("--%s requires a target", scope)
			}
			id, e := c.resolve(scope+"s", a.value(scope))
			return scope, id, e
		}
	}
	return "platform", "", nil
}

func (c *cli) printAccess(v any, columns ...string) error {
	if c.raw {
		return c.print(v)
	}
	rows, ok := v.([]any)
	if !ok {
		return c.print(v)
	}
	w := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, strings.ToUpper(strings.Join(columns, "\t")))
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		cells := make([]string, len(columns))
		for i, k := range columns {
			if value, exists := m[k]; exists {
				if s, ok := value.(string); ok {
					cells[i] = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
				} else {
					b, _ := json.Marshal(value)
					cells[i] = string(b)
				}
			}
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	return w.Flush()
}

func (c *cli) runAccess(cmd string, args []string) (bool, error) {
	if cmd == "workspaces" && len(args) > 0 && args[0] == "members" {
		cmd, args = "members", args[1:]
	}
	allowed := ""
	switch cmd {
	case "users", "permissions", "members":
	case "roles":
		allowed = "clone-of name color icon perm"
	case "grant", "revoke", "effective":
		allowed = "workspace project"
	case "assignments":
		allowed = "user workspace project"
	case "overrides":
		allowed = "role user allow deny"
	case "audit":
		allowed = "workspace"
	default:
		return false, nil
	}
	a, e := parseAccessArgs(args, allowed)
	if e != nil {
		return true, e
	}
	p := a.positional
	run := func() error {
		method, path := "GET", cmd
		var body any
		columns := []string{"id", "name"}
		count := func(n int) error {
			if len(p) != n {
				return errors.New("invalid arguments; run startship help")
			}
			return nil
		}
		switch cmd {
		case "permissions":
			if e := count(0); e != nil {
				return e
			}
			v, e := c.request("GET", path, nil)
			if e != nil {
				return e
			}
			if c.raw {
				return c.print(v)
			}
			m, ok := v.(map[string]any)
			if !ok {
				return errors.New("expected permission catalog")
			}
			if e = c.printAccess(m["permissions"], "key", "label", "scope", "description"); e != nil {
				return e
			}
			fmt.Fprintf(c.out, "Role icons: %v\n", m["icons"])
			return nil
		case "users":
			columns = []string{"id", "username", "must_change_password", "platform_roles"}
			if len(p) == 0 {
				return errors.New("user action required")
			}
			op := p[0]
			switch op {
			case "list":
				if e := count(1); e != nil {
					return e
				}
			case "create":
				if e := count(2); e != nil {
					return e
				}
				password, e := c.accessPassword()
				if e != nil {
					return e
				}
				method, body = "POST", map[string]string{"username": p[1], "password": password}
			case "rename", "reset-password", "delete":
				n := 2
				if op == "rename" {
					n = 3
				}
				if e := count(n); e != nil {
					return e
				}
				id, e := c.resolve("users", p[1])
				if e != nil {
					return e
				}
				path += "/" + url.PathEscape(id)
				method = "PATCH"
				if op == "delete" {
					method = "DELETE"
				} else if op == "rename" {
					body = map[string]string{"username": p[2]}
				} else {
					password, e := c.accessPassword()
					if e != nil {
						return e
					}
					body = map[string]string{"password": password}
				}
			default:
				return errors.New("unknown users action")
			}
		case "roles":
			columns = []string{"id", "name", "color", "icon", "permissions", "members"}
			if len(p) == 0 {
				return errors.New("role action required")
			}
			op := p[0]
			if op == "list" {
				if e := count(1); e != nil {
					return e
				}
			} else {
				if e := count(2); e != nil {
					return e
				}
				if op != "create" {
					id, e := c.resolve("roles", p[1])
					if e != nil {
						return e
					}
					path += "/" + url.PathEscape(id)
				}
				switch op {
				case "get":
				case "delete":
					method = "DELETE"
				case "create", "edit":
					m := map[string]any{}
					method = "PATCH"
					if op == "create" {
						method = "POST"
						m["name"] = p[1]
					}
					for _, key := range []string{"name", "color", "icon"} {
						if a.has(key) {
							if key == "name" && op == "create" {
								return errors.New("use the positional role name on create")
							}
							m[key] = a.value(key)
						}
					}
					if a.has("clone-of") {
						if op != "create" {
							return errors.New("--clone-of is only valid on create")
						}
						id, e := c.resolve("roles", a.value("clone-of"))
						if e != nil {
							return e
						}
						m["clone_of"] = id
					}
					if a.has("perm") {
						m["permissions"] = permissionArgs(a.flags["perm"])
					}
					if len(m) == 0 {
						return errors.New("provide role fields to edit")
					}
					body = m
				default:
					return errors.New("unknown roles action")
				}
			}
			if op != "create" && op != "edit" && len(a.flags) > 0 {
				return errors.New("role options require create or edit")
			}
		case "members":
			if e := count(1); e != nil {
				return e
			}
			id, e := c.resolve("workspaces", p[0])
			if e != nil {
				return e
			}
			v, e := c.request("GET", "workspaces/"+url.PathEscape(id)+"/members", nil)
			if e != nil {
				return e
			}
			if c.raw {
				return c.print(v)
			}
			m, ok := v.(map[string]any)
			if !ok {
				return errors.New("expected members response")
			}
			fmt.Fprintf(c.out, "Can manage: %v\n", m["can_manage"])
			return c.printAccess(m["members"], "user_id", "username", "roles")
		case "grant", "revoke":
			if e := count(2); e != nil {
				return e
			}
			scope, id, e := c.accessScope(a)
			if e != nil {
				return e
			}
			user, e := c.resolve("users", p[0])
			if e != nil {
				return e
			}
			role, e := c.resolve("roles", p[1])
			if e != nil {
				return e
			}
			path = "assignments"
			method = "POST"
			body = map[string]string{"user_id": user, "role_id": role, "scope": scope, "scope_id": id}
			if cmd == "revoke" {
				q := url.Values{"user_id": {user}, "scope": {scope}, "scope_id": {id}}
				v, e := c.request("GET", path+"?"+q.Encode(), nil)
				if e != nil {
					return e
				}
				rows, ok := v.([]any)
				if !ok {
					return errors.New("expected assignments list")
				}
				assignmentID := ""
				for _, row := range rows {
					m, _ := row.(map[string]any)
					if field(m, "user_id") == user && field(m, "role_id") == role && field(m, "scope") == scope && field(m, "scope_id") == id {
						if assignmentID != "" {
							return errors.New("ambiguous assignment")
						}
						assignmentID = field(m, "id")
					}
				}
				if assignmentID == "" {
					return errors.New("assignment not found")
				}
				path += "/" + url.PathEscape(assignmentID)
				method, body = "DELETE", nil
			}
		case "assignments":
			if e := count(1); e != nil {
				return e
			}
			if p[0] != "list" {
				return errors.New("use assignments list")
			}
			scope, id, e := c.accessScope(a)
			if e != nil {
				return e
			}
			q := url.Values{}
			if scope != "platform" {
				q.Set("scope", scope)
				q.Set("scope_id", id)
			}
			if a.has("user") {
				id, e := c.resolve("users", a.value("user"))
				if e != nil {
					return e
				}
				q.Set("user_id", id)
			}
			path += "?" + q.Encode()
			columns = []string{"id", "username", "user_id", "role_id", "scope", "scope_id"}
		case "overrides":
			if e := count(3); e != nil {
				return e
			}
			if p[0] != "list" && p[0] != "set" {
				return errors.New("use overrides list or set")
			}
			if p[1] != "workspace" && p[1] != "project" {
				return errors.New("overrides require workspace or project scope")
			}
			id, e := c.resolve(p[1]+"s", p[2])
			if e != nil {
				return e
			}
			if p[0] == "list" {
				if len(a.flags) > 0 {
					return errors.New("overrides list takes no options")
				}
				path += "?" + url.Values{"scope": {p[1]}, "scope_id": {id}}.Encode()
			} else {
				if a.has("role") == a.has("user") {
					return errors.New("choose exactly one --role or --user")
				}
				subject := "role"
				if a.has("user") {
					subject = "user"
				}
				sid, e := c.resolve(subject+"s", a.value(subject))
				if e != nil {
					return e
				}
				method, body = "PUT", map[string]any{"scope": p[1], "scope_id": id, "subject_type": subject, "subject_id": sid, "allow": permissionArgs(a.flags["allow"]), "deny": permissionArgs(a.flags["deny"])}
			}
			columns = []string{"scope", "scope_id", "subject_type", "subject_id", "allow", "deny"}
		case "effective", "audit":
			if len(p) > 1 || (cmd == "audit" && len(p) > 0) {
				return errors.New("unexpected arguments")
			}
			scope, id, e := c.accessScope(a)
			if e != nil {
				return e
			}
			q := url.Values{}
			if scope != "platform" {
				q.Set(scope+"_id", id)
			}
			if len(p) == 1 {
				uid, e := c.resolve("users", p[0])
				if e != nil {
					return e
				}
				q.Set("user_id", uid)
			}
			path += "?" + q.Encode()
			columns = []string{"id", "at", "actor", "action", "target", "scope", "scope_id", "detail"}
		}
		v, e := c.request(method, path, body)
		if e != nil {
			return e
		}
		return c.printAccess(v, columns...)
	}
	return true, run()
}
