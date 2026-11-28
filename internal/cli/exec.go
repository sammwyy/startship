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
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"golang.org/x/term"
)

type terminalExit int

func (e terminalExit) Error() string { return fmt.Sprintf("terminal process exited with code %d", e) }

func execArgs(args []string) (string, []string, error) {
	usage := errors.New("usage: startship exec TARGET [-- COMMAND ARGS...] (container:ID, app:WS/ID/CONTAINER, service:ID)")
	if len(args) == 0 || len(args) > 1 && (len(args) < 3 || args[1] != "--" || args[2] == "") {
		return "", nil, usage
	}
	kind, id, _ := strings.Cut(args[0], ":")
	parts := strings.Split(id, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", nil, usage
		}
		parts[i] = url.PathEscape(part)
	}
	var path string
	switch {
	case (kind == "container" || kind == "service") && len(parts) == 1:
		path = kind + "s/" + parts[0]
	case kind == "app" && len(parts) == 3:
		path = "workspaces/" + parts[0] + "/apps/" + parts[1] + "/containers/" + parts[2]
	default:
		return "", nil, usage
	}
	var command []string
	if len(args) > 1 {
		command = args[2:]
	}
	return path + "/exec", command, nil
}

func (c *cli) runExec(args []string) error {
	path, command, err := execArgs(args)
	if err != nil {
		return err
	}
	fd := int(os.Stdin.Fd())
	cols, rows, err := term.GetSize(fd)
	if err != nil {
		return errors.New("exec requires an interactive terminal")
	}
	if _, err := c.request("GET", path, nil); err != nil {
		return err
	}
	server, client, err := c.transport()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	headers := http.Header{}
	if strings.HasPrefix(c.Token, "sst_") {
		headers.Set("Authorization", "Bearer "+c.Token)
	} else if c.Token != "" {
		headers.Set("Cookie", (&http.Cookie{Name: "startship_session", Value: c.Token}).String())
	}
	handshake, done := context.WithTimeout(ctx, 10*time.Second)
	conn, _, err := websocket.Dial(handshake, server+"/api/"+path, &websocket.DialOptions{HTTPClient: client, HTTPHeader: headers})
	done()
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 10)
	state, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, state)
	if err = wsjson.Write(ctx, conn, map[string]any{
		"type": "start", "rows": rows, "cols": cols, "command": command,
	}); err != nil {
		return err
	}
	resizes := make(chan os.Signal, 1)
	signal.Notify(resizes, syscall.SIGWINCH)
	defer signal.Stop(resizes)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-resizes:
				if cols, rows, err := term.GetSize(fd); err == nil && rows > 0 && cols > 0 {
					if wsjson.Write(ctx, conn, map[string]any{"type": "resize", "rows": rows, "cols": cols}) != nil {
						return
					}
				}
			}
		}
	}()
	go func() {
		buf := make([]byte, 16<<10)
		for {
			n, err := c.in.Read(buf)
			if n > 0 && conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
				return
			}
			if err != nil {
				stop()
				return
			}
		}
	}()
	for {
		kind, b, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if kind == websocket.MessageBinary {
			if _, err := c.out.Write(b); err != nil {
				return err
			}
			continue
		}
		var event struct {
			Type string `json:"type"`
			Code int    `json:"code"`
		}
		if err := json.Unmarshal(b, &event); err != nil {
			return err
		}
		if event.Type == "exit" {
			if event.Code == 0 {
				return nil
			}
			return terminalExit(event.Code)
		}
	}
}
