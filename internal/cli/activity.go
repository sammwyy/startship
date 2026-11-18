package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/coder/websocket"
)

func (c *cli) runAccount(args []string) error {
	if len(args) == 0 {
		return errors.New("use account update|sessions|revoke|activity")
	}
	switch args[0] {
	case "update":
		a, err := parseAccessArgs(args[1:], "display-name email")
		if err != nil {
			return err
		}
		if len(a.positional) > 1 || len(a.positional) == 0 && len(a.flags) == 0 {
			return errors.New("use account update [USERNAME] [--display-name NAME] [--email EMAIL]")
		}
		body := map[string]string{}
		if len(a.positional) == 1 {
			body["username"] = a.positional[0]
		}
		for flag, key := range map[string]string{"display-name": "display_name", "email": "email"} {
			if a.has(flag) {
				body[key] = a.value(flag)
			}
		}
		body["current_password"], err = c.prompt("Current password", true)
		if err != nil {
			return err
		}
		return c.printRequest("PATCH", "me", body)
	case "sessions":
		if len(args) == 1 {
			return c.firewallTable("me/sessions", "", "id", "ip", "user_agent", "logged_at", "last_activity_at", "expires_at")
		}
	case "revoke":
		if len(args) == 2 {
			return c.printRequest("DELETE", "me/sessions/"+url.PathEscape(args[1]), nil)
		}
	case "activity":
		return c.showActivity("me/activity", args[1:], map[string]string{"topic": "account_activity"}, true)
	case "tokens":
		if len(args) == 1 {
			return c.firewallTable("me/tokens", "", "id", "name", "access", "all_workspaces", "workspaces", "last_used_ip", "last_used_at", "expires_at")
		}
	case "token":
		return c.runToken(args[1:])
	}
	return errors.New("use account update|sessions|revoke|activity")
}

func (c *cli) runActivity(args []string) error {
	if len(args) < 2 || args[0] != "workspace" && args[0] != "project" {
		return errors.New("use activity workspace|project TARGET [--follow]")
	}
	scope := args[0]
	id, err := c.resolve(scope+"s", args[1])
	if err != nil {
		return err
	}
	return c.showActivity(scope+"s/"+url.PathEscape(id)+"/activity", args[2:],
		map[string]string{"topic": scope + "_activity", scope + "_id": id}, false)
}

func (c *cli) showActivity(path string, args []string, sub map[string]string, personal bool) error {
	columns := []string{"at", "actor", "action", "target", "scope"}
	if personal {
		columns = append(columns, "session_id", "source")
	}
	if len(args) == 0 {
		return c.firewallTable(path, "", columns...)
	}
	if len(args) != 1 || args[0] != "--follow" {
		return errors.New("expected --follow")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var latest float64
	return c.followLive(ctx, sub, func(data json.RawMessage) error {
		var entries []map[string]any
		if err := json.Unmarshal(data, &entries); err != nil {
			return err
		}
		rows := []any{}
		for i := len(entries) - 1; i >= 0; i-- {
			id, _ := entries[i]["id"].(float64)
			if id <= latest {
				continue
			}
			latest = id
			if c.raw {
				if err := json.NewEncoder(c.out).Encode(entries[i]); err != nil {
					return err
				}
			} else {
				rows = append(rows, entries[i])
			}
		}
		if len(rows) > 0 {
			return c.printAccess(rows, columns...)
		}
		return nil
	})
}

func (c *cli) followLive(ctx context.Context, sub map[string]string, snapshot func(json.RawMessage) error) error {
	sub["type"], sub["id"] = "subscribe", sub["topic"]
	for {
		err := c.readLive(ctx, sub, snapshot)
		if ctx.Err() != nil || websocket.CloseStatus(err) == websocket.StatusNormalClosure {
			return nil
		}
		if websocket.CloseStatus(err) != 4408 {
			return err
		}
		if err = c.renewAccess(); err != nil {
			return err
		}
	}
}

func (c *cli) readLive(ctx context.Context, sub map[string]string, snapshot func(json.RawMessage) error) error {
	server, client, err := c.transport()
	if err != nil {
		return err
	}
	streamClient := *client
	streamClient.Timeout = 0
	dial := func() (*websocket.Conn, *http.Response, error) {
		headers := http.Header{}
		if c.Token != "" {
			headers.Set("Cookie", (&http.Cookie{Name: "startship_session", Value: c.Token}).String())
		}
		return websocket.Dial(ctx, server+"/api/live", &websocket.DialOptions{HTTPClient: &streamClient, HTTPHeader: headers})
	}
	conn, resp, err := dial()
	if err != nil && resp != nil && resp.StatusCode == http.StatusUnauthorized && c.Refresh != "" {
		if err = c.renewAccess(); err != nil {
			return err
		}
		conn, resp, err = dial()
	}
	if err != nil {
		if resp != nil {
			return fmt.Errorf("live connection: %s: %w", resp.Status, err)
		}
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 20)
	b, _ := json.Marshal(sub)
	if err = conn.Write(ctx, websocket.MessageText, b); err != nil {
		return err
	}
	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var msg struct {
			Type, ID, Error string
			Data            json.RawMessage
		}
		if err := json.Unmarshal(b, &msg); err != nil {
			return fmt.Errorf("invalid live message: %w", err)
		}
		if msg.ID != sub["id"] {
			continue
		}
		if msg.Type == "error" {
			return fmt.Errorf("live subscription: %s", msg.Error)
		}
		if msg.Type == "snapshot" {
			if err := snapshot(msg.Data); err != nil {
				return err
			}
		}
	}
}
