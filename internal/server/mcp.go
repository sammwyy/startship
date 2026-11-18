package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"

	"github.com/sammwyy/startship/internal/version"
)

// The MCP endpoint speaks the Streamable HTTP transport in its stateless form: every JSON-RPC
// message is a POST answered with one JSON response. Tools call the regular API as the token's
// owner, so roles, overrides, token scope and the activity log apply exactly as they do elsewhere.

const mcpLatest = "2025-06-18"

var mcpVersions = []string{mcpLatest, "2025-03-26", "2024-11-05"}

type mcpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations"`
	run         func(c *mcpCall, in map[string]any) (any, error)
}

type mcpCall struct {
	a *Server
	r *http.Request
}

type apiError struct {
	status  int
	message string
}

func (e apiError) Error() string { return e.message }

func (a *Server) mcpHandler(w http.ResponseWriter, r *http.Request, s sessionIdentity) {
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		fail(w, 405, "method not allowed")
		return
	}
	var m mcpMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&m); err != nil || m.JSONRPC != "2.0" || m.Method == "" {
		reply(w, 400, map[string]any{"jsonrpc": "2.0", "id": nil, "error": mcpError{-32600, "invalid JSON-RPC request"}})
		return
	}
	if len(m.ID) == 0 || string(m.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	result, rpcErr := a.mcpDispatch(r, m)
	out := map[string]any{"jsonrpc": "2.0", "id": m.ID}
	if rpcErr != nil {
		out["error"] = rpcErr
	} else {
		out["result"] = result
	}
	reply(w, 200, out)
}

func (a *Server) mcpDispatch(r *http.Request, m mcpMessage) (any, *mcpError) {
	switch m.Method {
	case "initialize":
		var in struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(m.Params, &in)
		v := mcpLatest
		if slices.Contains(mcpVersions, in.ProtocolVersion) {
			v = in.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "startship", "title": "Startship", "version": version.Version},
			"instructions": "Startship deploys projects into workspaces. Each project has one source; deploying it builds that source " +
				"and runs its containers. Tools act as the token's owner within the workspaces the token was created for. " +
				"Workspaces, projects and containers can be named by ID or by unique name.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpTools}, nil
	case "tools/call":
		var in struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(m.Params, &in); err != nil {
			return nil, &mcpError{-32602, "invalid tool call"}
		}
		i := slices.IndexFunc(mcpTools, func(t mcpTool) bool { return t.Name == in.Name })
		if i < 0 {
			return nil, &mcpError{-32602, "unknown tool " + in.Name}
		}
		if in.Arguments == nil {
			in.Arguments = map[string]any{}
		}
		v, err := mcpTools[i].run(&mcpCall{a, r}, in.Arguments)
		if err != nil {
			return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": err.Error()}}}, nil
		}
		text, _ := json.MarshalIndent(v, "", "  ")
		out := map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}}
		if _, ok := v.(map[string]any); ok {
			out["structuredContent"] = v
		}
		return out, nil
	}
	return nil, &mcpError{-32601, "method not found: " + m.Method}
}

// do runs one API request as the MCP caller, carrying its token and peer address.
func (c *mcpCall) do(method, path string, body any) (any, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	ctx := context.WithValue(c.r.Context(), mcpCallKey{}, true)
	req, err := http.NewRequestWithContext(ctx, method, "/api/"+path, reader)
	if err != nil {
		return nil, err
	}
	req.RemoteAddr, req.Host, req.TLS = c.r.RemoteAddr, c.r.Host, c.r.TLS
	req.Header.Set("Authorization", c.r.Header.Get("Authorization"))
	req.Header.Set("User-Agent", c.r.UserAgent())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c.a.api().ServeHTTP(rec, req)
	var out any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			out = rec.Body.String()
		}
	}
	if rec.Code >= 400 {
		msg := http.StatusText(rec.Code)
		if m, ok := out.(map[string]any); ok && m["error"] != nil {
			msg = fmt.Sprint(m["error"])
		}
		return nil, apiError{rec.Code, msg}
	}
	if out == nil {
		out = map[string]any{"ok": true}
	}
	return out, nil
}

// resolve finds a record by ID or by unique name among those the caller can see.
func (c *mcpCall) resolve(kind string, in map[string]any, key string) (string, error) {
	value := strings.TrimSpace(str(in, key))
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	list, err := c.do("GET", kind, nil)
	if err != nil {
		return "", err
	}
	rows, _ := list.([]any)
	var matches []string
	for _, row := range rows {
		m, _ := row.(map[string]any)
		if m["id"] == value {
			return value, nil
		}
		if strings.EqualFold(fmt.Sprint(m["name"]), value) {
			matches = append(matches, fmt.Sprint(m["id"]))
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("%s %q not found", key, value)
	}
	return "", fmt.Errorf("%s %q is ambiguous; use its ID", key, value)
}

func str(in map[string]any, key string) string {
	v, _ := in[key].(string)
	return v
}

func object(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func text(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func tool(name, title, description string, readOnly, destructive bool, schema map[string]any, run func(*mcpCall, map[string]any) (any, error)) mcpTool {
	return mcpTool{Name: name, Title: title, Description: description, InputSchema: schema, run: run,
		Annotations: map[string]any{"title": title, "readOnlyHint": readOnly, "destructiveHint": destructive, "openWorldHint": false}}
}

// get resolves one record and reads it, or a subpath of it.
func get(kind, key, suffix string) func(*mcpCall, map[string]any) (any, error) {
	return func(c *mcpCall, in map[string]any) (any, error) {
		id, err := c.resolve(kind, in, key)
		if err != nil {
			return nil, err
		}
		return c.do("GET", kind+"/"+url.PathEscape(id)+suffix, nil)
	}
}

// children lists records of a kind under a resolved parent, or all visible ones.
func children(kind, parentKind, key, query string) func(*mcpCall, map[string]any) (any, error) {
	return func(c *mcpCall, in map[string]any) (any, error) {
		path := kind
		if str(in, key) != "" {
			id, err := c.resolve(parentKind, in, key)
			if err != nil {
				return nil, err
			}
			path += "?" + query + "=" + url.QueryEscape(id)
			if kind == "releases" {
				path += "&logs=false"
			}
		}
		return c.do("GET", path, nil)
	}
}

func deploy(op string) func(*mcpCall, map[string]any) (any, error) {
	return func(c *mcpCall, in map[string]any) (any, error) {
		id, err := c.resolve("projects", in, "project")
		if err != nil {
			return nil, err
		}
		body := map[string]any{}
		if v, ok := in["no_cache"].(bool); ok {
			body["no_cache"] = v
		}
		if v := str(in, "deployment"); v != "" {
			body["deployment_id"] = v
		}
		return c.do("POST", "projects/"+url.PathEscape(id)+"/"+op, body)
	}
}

var (
	workspaceArg = text("Workspace ID or name.")
	projectArg   = text("Project ID or name.")
)

var mcpTools []mcpTool

func init() {
	mcpTools = []mcpTool{
		tool("whoami", "Who am I", "The account and token this MCP connection acts as, with platform permissions.", true, false, object(nil),
			func(c *mcpCall, _ map[string]any) (any, error) { return c.do("GET", "me", nil) }),
		tool("list_workspaces", "List workspaces", "Workspaces this token can reach.", true, false, object(nil),
			func(c *mcpCall, _ map[string]any) (any, error) { return c.do("GET", "workspaces", nil) }),
		tool("create_workspace", "Create workspace", "Create a workspace. Needs a token with administer access to all workspaces.", false, false,
			object(map[string]any{"name": text("Workspace name.")}, "name"),
			func(c *mcpCall, in map[string]any) (any, error) {
				return c.do("POST", "workspaces", map[string]any{"name": str(in, "name")})
			}),
		tool("rename_workspace", "Rename workspace", "Rename a workspace.", false, false,
			object(map[string]any{"workspace": workspaceArg, "name": text("New name.")}, "workspace", "name"),
			func(c *mcpCall, in map[string]any) (any, error) {
				id, err := c.resolve("workspaces", in, "workspace")
				if err != nil {
					return nil, err
				}
				return c.do("PATCH", "workspaces/"+url.PathEscape(id), map[string]any{"name": str(in, "name")})
			}),
		tool("delete_workspace", "Delete workspace", "Delete a workspace and everything in it.", false, true,
			object(map[string]any{"workspace": workspaceArg}, "workspace"),
			func(c *mcpCall, in map[string]any) (any, error) {
				id, err := c.resolve("workspaces", in, "workspace")
				if err != nil {
					return nil, err
				}
				return c.do("DELETE", "workspaces/"+url.PathEscape(id), nil)
			}),
		tool("list_members", "List members", "Members of a workspace and the roles they hold.", true, false,
			object(map[string]any{"workspace": workspaceArg}, "workspace"), get("workspaces", "workspace", "/members")),
		tool("workspace_activity", "Workspace audit activity", "Recent audit activity in a workspace.", true, false,
			object(map[string]any{"workspace": workspaceArg}, "workspace"), get("workspaces", "workspace", "/activity")),
		tool("list_projects", "List projects", "Projects, optionally only those in one workspace.", true, false,
			object(map[string]any{"workspace": workspaceArg}), children("projects", "workspaces", "workspace", "workspace_id")),
		tool("get_project", "Get project", "A project's source, settings, environment and status.", true, false,
			object(map[string]any{"project": projectArg}, "project"), get("projects", "project", "")),
		tool("create_project", "Create project", "Create a project in a workspace. Set its source with update_project.", false, false,
			object(map[string]any{"name": text("Project name."), "workspace": workspaceArg}, "name", "workspace"),
			func(c *mcpCall, in map[string]any) (any, error) {
				id, err := c.resolve("workspaces", in, "workspace")
				if err != nil {
					return nil, err
				}
				return c.do("POST", "projects", map[string]any{"name": str(in, "name"), "workspace_id": id})
			}),
		tool("update_project", "Update project", "Change project fields, as get_project returns them: for example source, branch, build_type, port or env.", false, false,
			object(map[string]any{"project": projectArg, "changes": map[string]any{"type": "object", "description": "Fields to change."}}, "project", "changes"),
			func(c *mcpCall, in map[string]any) (any, error) {
				id, err := c.resolve("projects", in, "project")
				if err != nil {
					return nil, err
				}
				changes, ok := in["changes"].(map[string]any)
				if !ok || len(changes) == 0 {
					return nil, errors.New("changes must be a non-empty object")
				}
				return c.do("PATCH", "projects/"+url.PathEscape(id), changes)
			}),
		tool("delete_project", "Delete project", "Delete a project, its deployments and its containers.", false, true,
			object(map[string]any{"project": projectArg}, "project"),
			func(c *mcpCall, in map[string]any) (any, error) {
				id, err := c.resolve("projects", in, "project")
				if err != nil {
					return nil, err
				}
				return c.do("DELETE", "projects/"+url.PathEscape(id), nil)
			}),
		tool("deploy_project", "Deploy project", "Build the project's source and roll out a new deployment.", false, false,
			object(map[string]any{"project": projectArg, "no_cache": map[string]any{"type": "boolean", "description": "Build without cache."}}, "project"),
			deploy("deploy")),
		tool("redeploy_project", "Redeploy project", "Roll out an earlier successful deployment again without rebuilding.", false, false,
			object(map[string]any{"project": projectArg, "deployment": text("Deployment ID; the active one when omitted.")}, "project"),
			deploy("redeploy")),
		tool("list_deployments", "List deployments", "Deployments of a project, newest first.", true, false,
			object(map[string]any{"project": projectArg}, "project"), children("releases", "projects", "project", "project_id")),
		tool("get_deployment", "Get deployment", "A deployment with its build log.", true, false,
			object(map[string]any{"deployment": text("Deployment ID.")}, "deployment"),
			func(c *mcpCall, in map[string]any) (any, error) {
				id := str(in, "deployment")
				if id == "" {
					return nil, errors.New("deployment is required")
				}
				return c.do("GET", "releases/"+url.PathEscape(id), nil)
			}),
		tool("list_containers", "List containers", "Containers a project's deployment runs.", true, false,
			object(map[string]any{"project": projectArg}, "project"), children("containers", "projects", "project", "project_id")),
		tool("container_logs", "Container logs", "Recent logs of a container.", true, false,
			object(map[string]any{"container": text("Container ID or name.")}, "container"), get("containers", "container", "/logs")),
		tool("list_routes", "List routes", "Domain routes of a project.", true, false,
			object(map[string]any{"project": projectArg}, "project"), children("routes", "projects", "project", "project_id")),
		tool("project_activity", "Project activity log", "Recent activity on a project.", true, false,
			object(map[string]any{"project": projectArg}, "project"), get("projects", "project", "/activity")),
		tool("api_request", "Startship API request", "Call any Startship API path (without /api/) as this token, for actions no other tool covers. See docs/api.md.", false, true,
			object(map[string]any{
				"method": map[string]any{"type": "string", "enum": []string{"GET", "POST", "PUT", "PATCH", "DELETE"}},
				"path":   text("Path such as workspaces/ID/services."),
				"body":   map[string]any{"type": "object", "description": "JSON body."},
			}, "method", "path"),
			func(c *mcpCall, in map[string]any) (any, error) {
				path := strings.TrimPrefix(strings.TrimPrefix(str(in, "path"), "/"), "api/")
				if path == "" || path == "mcp" || strings.HasPrefix(path, "mcp?") {
					return nil, errors.New("path is required")
				}
				var body any
				if v, ok := in["body"].(map[string]any); ok {
					body = v
				}
				return c.do(strings.ToUpper(str(in, "method")), path, body)
			}),
	}
}
