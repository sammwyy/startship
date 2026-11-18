package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func bearerCall(t *testing.T, h http.Handler, method, path, body, token string, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/api/"+path, strings.NewReader(body))
	r.RemoteAddr = "203.0.113.7:4242"
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	return w
}

func mintToken(t *testing.T, h http.Handler, cookie *http.Cookie, body string) (string, string) {
	t.Helper()
	var out struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	w := accountRequest(t, h, "POST", "me/tokens", body, cookie, 201)
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || !strings.HasPrefix(out.Token, tokenPrefix) {
		t.Fatal("bad token", w.Body.String())
	}
	return out.ID, out.Token
}

func TestAccessTokenScopeAndRevocation(t *testing.T) {
	a := testServer(t)
	alice, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	h := a.api()
	cookie := accountLogin(t, h, "alice", "password123")
	home := a.store.List("workspaces", alice)[0]
	other := accountRequest(t, h, "POST", "workspaces", `{"name":"other"}`, cookie, 200)
	var otherWS Record
	json.Unmarshal(other.Body.Bytes(), &otherWS)

	accountRequest(t, h, "POST", "me/tokens", `{"name":"x","api":false,"mcp":false,"all_workspaces":true}`, cookie, 400)
	accountRequest(t, h, "POST", "me/tokens", `{"name":"x"}`, cookie, 400)
	id, op := mintToken(t, h, cookie, `{"name":"ci","workspaces":["`+home.ID+`"],"access":"operate","mcp":false}`)

	var list []Record
	json.Unmarshal(bearerCall(t, h, "GET", "workspaces", "", op, 200).Body.Bytes(), &list)
	if len(list) != 1 || list[0].ID != home.ID {
		t.Fatal("token should only reach its workspace", list)
	}
	bearerCall(t, h, "GET", "workspaces/"+otherWS.ID, "", op, 404)
	bearerCall(t, h, "PATCH", "workspaces/"+home.ID, `{"name":"renamed"}`, op, 403)
	bearerCall(t, h, "POST", "workspaces", `{"name":"new"}`, op, 403)
	bearerCall(t, h, "POST", "projects", `{"name":"site","workspace_id":"`+home.ID+`"}`, op, 200)
	bearerCall(t, h, "POST", "projects", `{"name":"site","workspace_id":"`+otherWS.ID+`"}`, op, 400)
	bearerCall(t, h, "GET", "me/tokens", "", op, 403)
	bearerCall(t, h, "PATCH", "me", `{"display_name":"x"}`, op, 403)
	bearerCall(t, h, "POST", "mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, op, 403)

	var tokens []accessToken
	json.Unmarshal(accountRequest(t, h, "GET", "me/tokens", "", cookie, 200).Body.Bytes(), &tokens)
	if len(tokens) != 1 || tokens[0].LastUsedIP != "203.0.113.7" || tokens[0].LastUsedAt == "" || tokens[0].Access != "operate" {
		t.Fatal("token usage not recorded", tokens)
	}
	entries, err := a.activity(scopeAccount, alice, false)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Action != "project.create" || entries[0].Source != "token" || entries[0].Token != "ci" || entries[0].TokenID != id {
		t.Fatal("token activity not attributed", entries[0])
	}
	if entries, _ := a.activity(scopeWorkspace, home.ID, false); entries[0].TokenID != "" || entries[0].Source != "token" {
		t.Fatal("token id should stay in account activity", entries[0])
	}

	_, admin := mintToken(t, h, cookie, `{"name":"admin","all_workspaces":true,"access":"admin","api":true,"mcp":true}`)
	bearerCall(t, h, "PATCH", "workspaces/"+home.ID, `{"name":"renamed"}`, admin, 200)
	created := bearerCall(t, h, "POST", "workspaces", `{"name":"future"}`, admin, 200)
	var future Record
	json.Unmarshal(created.Body.Bytes(), &future)
	bearerCall(t, h, "GET", "workspaces/"+future.ID, "", admin, 200)
	bearerCall(t, h, "POST", "users", `{"username":"eve","password":"password123"}`, admin, 403)

	accountRequest(t, h, "DELETE", "me/tokens/"+id, "", cookie, 204)
	bearerCall(t, h, "GET", "workspaces", "", op, 401)
	accountRequest(t, h, "DELETE", "me/tokens/"+id, "", cookie, 404)
}

func TestMCPServer(t *testing.T) {
	a := testServer(t)
	alice, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	h := a.api()
	cookie := accountLogin(t, h, "alice", "password123")
	home := a.store.List("workspaces", alice)[0]
	_, mcp := mintToken(t, h, cookie, `{"name":"agent","workspaces":["`+home.ID+`"],"api":false}`)

	unauth := httptest.NewRecorder()
	h.ServeHTTP(unauth, httptest.NewRequest("POST", "/api/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)))
	if unauth.Code != 401 || !strings.HasPrefix(unauth.Header().Get("WWW-Authenticate"), "Bearer") {
		t.Fatal("MCP should challenge for a token", unauth.Code)
	}
	bearerCall(t, h, "GET", "workspaces", "", mcp, 403)
	call := func(method, params string) map[string]any {
		t.Helper()
		w := bearerCall(t, h, "POST", "mcp", `{"jsonrpc":"2.0","id":7,"method":"`+method+`","params":`+params+`}`, mcp, 200)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out["id"] != float64(7) {
			t.Fatal(w.Body.String())
		}
		return out
	}
	init := call("initialize", `{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}`)
	if init["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatal(init)
	}
	bearerCall(t, h, "POST", "mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, mcp, 202)
	tools := call("tools/list", `{}`)["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 10 {
		t.Fatal("expected tools", len(tools))
	}
	result := call("tools/call", `{"name":"create_project","arguments":{"name":"site","workspace":"`+home.Name+`"}}`)["result"].(map[string]any)
	if result["isError"] == true {
		t.Fatal(result)
	}
	projects := call("tools/call", `{"name":"list_projects","arguments":{"workspace":"`+home.ID+`"}}`)["result"].(map[string]any)
	if text := projects["content"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(text, `"site"`) {
		t.Fatal(text)
	}
	denied := call("tools/call", `{"name":"rename_workspace","arguments":{"workspace":"`+home.ID+`","name":"x"}}`)["result"].(map[string]any)
	if denied["isError"] != true {
		t.Fatal("operate token renamed a workspace", denied)
	}
	if call("bogus", `{}`)["error"] == nil {
		t.Fatal("unknown method should fail")
	}
	entries, _ := a.activity(scopeAccount, alice, false)
	if entries[0].Action != "project.create" || entries[0].Token != "agent" {
		t.Fatal("MCP action not attributed to its token", entries[0])
	}
}
