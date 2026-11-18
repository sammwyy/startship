package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

var weekdays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func (c *cli) runBackups(args []string) error {
	if len(args) == 1 && !strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "list", "create", "schedule", "restore", "delete", "download":
		default:
			args = append([]string{"list"}, args...)
		}
	}
	if len(args) == 0 {
		return errors.New("backups action or workspace required; run startship help")
	}
	op := args[0]
	allowed := map[string]string{"list": "service", "create": "", "schedule": "every at minute weekday keep off", "restore": "service", "delete": "", "download": "output"}
	flags, ok := allowed[op]
	if !ok {
		return fmt.Errorf("unknown backups action %q", op)
	}
	a, err := parseResourceArgs(args[1:], flags)
	if err != nil {
		return err
	}
	if len(a.positional) != 1 {
		return errors.New("invalid backups arguments; run startship help")
	}
	id := url.PathEscape(a.positional[0])
	switch op {
	case "list":
		ws, err := c.resolve("workspaces", a.positional[0])
		if err != nil {
			return err
		}
		path := "workspaces/" + url.PathEscape(ws) + "/backups"
		if a.has("service") {
			path += "?service_id=" + url.QueryEscape(a.value("service"))
		}
		return c.resourceList(path, "backups", "id", "service_name", "status", "trigger", "size_bytes", "created")
	case "create":
		return c.printRequest("POST", "services/"+id+"/backups", nil)
	case "schedule":
		body, err := backupPolicy(a)
		if err != nil {
			return err
		}
		v, err := c.request("PUT", "services/"+id+"/backup-schedule", body)
		if err != nil {
			return err
		}
		if m, ok := v.(map[string]any); ok && !c.raw {
			return c.print(map[string]any{"backup": m["backup"], "next_backup": m["next_backup"]})
		}
		return c.print(v)
	case "restore":
		var body any
		if a.has("service") {
			body = map[string]string{"service_id": a.value("service")}
		}
		return c.printRequest("POST", "backups/"+id+"/restore", body)
	case "delete":
		return c.printRequest("DELETE", "backups/"+id, nil)
	}
	out := io.Writer(os.Stdout)
	if a.has("output") && a.value("output") != "-" {
		f, err := os.Create(a.value("output"))
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}
	return c.download("backups/"+id+"/download", out)
}

func (c *cli) printRequest(method, path string, body any) error {
	v, err := c.request(method, path, body)
	if err != nil {
		return err
	}
	return c.print(v)
}

func backupPolicy(a accessArgs) (map[string]any, error) {
	if a.has("off") {
		if len(a.flags) != 1 {
			return nil, errors.New("--off cannot be combined with other options")
		}
		return map[string]any{"enabled": false, "frequency": "daily", "keep": 7}, nil
	}
	every := a.value("every")
	if every != "hourly" && every != "daily" && every != "weekly" {
		return nil, errors.New("--every must be hourly, daily or weekly")
	}
	p := map[string]any{"enabled": true, "frequency": every, "hour": 3, "minute": 0, "weekday": 0, "keep": 7}
	if a.has("at") {
		h, m, ok := strings.Cut(a.value("at"), ":")
		hour, e1 := strconv.Atoi(h)
		minute, e2 := strconv.Atoi(m)
		if !ok || e1 != nil || e2 != nil {
			return nil, errors.New("--at must be HH:MM (UTC)")
		}
		p["hour"], p["minute"] = hour, minute
	}
	if a.has("minute") {
		minute, err := strconv.Atoi(a.value("minute"))
		if err != nil {
			return nil, errors.New("--minute must be a number")
		}
		p["minute"] = minute
	}
	if a.has("weekday") {
		w := strings.ToLower(a.value("weekday"))
		found := false
		for i, d := range weekdays {
			if strings.HasPrefix(w, d) || w == strconv.Itoa(i) {
				p["weekday"], found = i, true
			}
		}
		if !found {
			return nil, errors.New("--weekday must be sun..sat or 0-6")
		}
	}
	if a.has("keep") {
		keep, err := strconv.Atoi(a.value("keep"))
		if err != nil {
			return nil, errors.New("--keep must be a number")
		}
		p["keep"] = keep
	}
	return p, nil
}

func (c *cli) download(path string, out io.Writer) error {
	server, client, err := c.transport()
	if err != nil {
		return err
	}
	r, err := http.NewRequest("GET", server+"/api/"+path, nil)
	if err != nil {
		return err
	}
	if c.Token != "" {
		r.AddCookie(&http.Cookie{Name: "startship_session", Value: c.Token})
	}
	resp, err := client.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	_, err = io.Copy(out, resp.Body)
	return err
}
