package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func (c *cli) runUptime(args []string) error {
	if len(args) < 1 || len(args) > 2 || len(args) == 2 && args[1] != "enable" && args[1] != "disable" {
		return errors.New("usage: uptime PROJECT [enable|disable]")
	}
	id, err := c.resolve("projects", args[0])
	if err != nil {
		return err
	}
	path := "projects/" + url.PathEscape(id)
	if len(args) == 2 {
		enabled := args[1] == "enable"
		if _, err := c.request("PATCH", path, map[string]bool{"uptime_monitoring": enabled}); err != nil {
			return err
		}
		return c.print(map[string]bool{"uptime_monitoring": enabled})
	}
	out, err := c.request("GET", path+"/uptime", nil)
	if err != nil {
		return err
	}
	if c.raw {
		return c.print(out)
	}
	m, ok := out.(map[string]any)
	if !ok {
		return errors.New("expected uptime summary")
	}
	fmt.Fprintf(c.out, "State: %s\n", field(m, "state"))
	for _, window := range []string{"24h", "7d", "30d"} {
		if value, ok := m["uptime_"+window].(float64); ok {
			fmt.Fprintf(c.out, "%s uptime: %.2f%%\n", window, value)
		} else {
			fmt.Fprintf(c.out, "%s uptime: no checks\n", window)
		}
	}
	if samples, ok := m["checks"].([]any); ok && len(samples) > 0 {
		if sample, ok := samples[0].(map[string]any); ok {
			if latency, ok := sample["latency_ms"].(float64); ok {
				fmt.Fprintf(c.out, "Latest check: %.1f ms\n", latency)
			}
		}
	}
	incidents, _ := m["incidents"].([]any)
	if len(incidents) == 0 {
		fmt.Fprintln(c.out, "No incidents recorded.")
	}
	for _, row := range incidents {
		incident, ok := row.(map[string]any)
		if !ok {
			continue
		}
		end := field(incident, "end")
		if end == "" {
			end = "ongoing"
		}
		fmt.Fprintf(c.out, "%s → %s: %s\n", field(incident, "start"), end, field(incident, "cause"))
	}
	return nil
}

func (c *cli) runStatusPage(args []string) error {
	if len(args) < 2 || args[0] != "show" && args[0] != "set" || args[0] == "show" && len(args) != 2 || args[0] == "set" && len(args) < 3 {
		return errors.New("usage: status-page show WORKSPACE | status-page set WORKSPACE [--slug SLUG] [--title TITLE] [--projects LIST] [--enable|--disable]")
	}
	id, err := c.resolve("workspaces", args[1])
	if err != nil {
		return err
	}
	path := "workspaces/" + url.PathEscape(id) + "/status-page"
	method := "GET"
	var body any
	if args[0] == "set" {
		method = "PATCH"
		settings := map[string]any{}
		for i := 2; i < len(args); i++ {
			flag := args[i]
			switch flag {
			case "--enable", "--disable":
				settings["enabled"] = flag == "--enable"
			case "--title", "--slug", "--projects":
				if i+1 >= len(args) {
					return fmt.Errorf("%s requires a value", flag)
				}
				i++
				if flag == "--projects" {
					ids, err := c.resolveStatusProjects(id, args[i])
					if err != nil {
						return err
					}
					settings["projects"] = ids
				} else {
					settings[flag[2:]] = args[i]
				}
			default:
				return fmt.Errorf("unknown status-page option %s", flag)
			}
		}
		body = settings
	}
	out, err := c.request(method, path, body)
	if err != nil {
		return err
	}
	return c.print(out)
}

func (c *cli) resolveStatusProjects(workspace, list string) ([]string, error) {
	ids := []string{}
	if list == "" {
		return ids, nil
	}
	out, err := c.request("GET", "projects?workspace_id="+url.QueryEscape(workspace), nil)
	if err != nil {
		return nil, err
	}
	projects, ok := out.([]any)
	if !ok {
		return nil, errors.New("expected project list")
	}
	for _, value := range strings.Split(list, ",") {
		value = strings.TrimSpace(value)
		var matches []string
		for _, row := range projects {
			p, _ := row.(map[string]any)
			if field(p, "id") == value {
				matches = []string{value}
				break
			}
			if field(p, "name") == value {
				matches = append(matches, field(p, "id"))
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("project %q must identify one project in this workspace", value)
		}
		ids = append(ids, matches[0])
	}
	return ids, nil
}
