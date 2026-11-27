package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"text/tabwriter"
)

func (c *cli) runNotifications(args []string) error {
	usageErr := errors.New("usage: notifications list|add|update|remove|test|log WORKSPACE (see startship help)")
	if len(args) < 2 {
		return usageErr
	}
	action, args := args[0], args[1:]
	workspace, err := c.resolve("workspaces", args[0])
	if err != nil {
		return err
	}
	args = args[1:]
	base := "workspaces/" + url.PathEscape(workspace) + "/notifications"
	path, method := base+"/channels", "GET"
	var body any
	switch action {
	case "list", "log":
		if len(args) != 0 {
			return usageErr
		}
		if action == "log" {
			path = base + "/deliveries"
		}
	case "add", "update":
		method = "POST"
		if action == "update" {
			if len(args) < 2 {
				return usageErr
			}
			path += "/" + url.PathEscape(args[0])
			args, method = args[1:], "PATCH"
		}
		settings := map[string]any{}
		for i := 0; i < len(args); i++ {
			flag := args[i]
			switch flag {
			case "--enable", "--disable":
				settings["enabled"] = flag == "--enable"
			case "--name", "--kind", "--url", "--events":
				i++
				if i == len(args) {
					return fmt.Errorf("%s requires a value", flag)
				}
				if flag == "--events" {
					events := []string{}
					if args[i] != "" {
						for _, event := range strings.Split(args[i], ",") {
							events = append(events, strings.TrimSpace(event))
						}
					}
					settings["events"] = events
				} else {
					settings[flag[2:]] = args[i]
				}
			default:
				return errors.New("unknown notifications option; see startship help")
			}
		}
		if action == "add" && (settings["name"] == nil || settings["kind"] == nil || settings["url"] == nil) {
			return errors.New("notifications add requires --name, --kind and --url")
		}
		body = settings
	case "remove", "test":
		if len(args) != 1 {
			return usageErr
		}
		path, method = path+"/"+url.PathEscape(args[0]), "DELETE"
		if action == "test" {
			path, method = path+"/test", "POST"
		}
	default:
		return usageErr
	}
	out, err := c.request(method, path, body)
	if err != nil {
		return err
	}
	if !c.raw && (action == "list" || action == "log") {
		return c.printNotifications(action, out)
	}
	return c.print(out)
}

func (c *cli) printNotifications(action string, data any) error {
	rows, ok := data.([]any)
	if !ok {
		return errors.New("expected a notification list")
	}
	w := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	if action == "list" {
		fmt.Fprintln(w, "ID\tNAME\tKIND\tHOST\tENABLED\tEVENTS")
	} else {
		fmt.Fprintln(w, "WHEN\tCHANNEL ID\tEVENT\tSTATUS\tHTTP\tERROR")
	}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		if action == "list" {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\t%v\n", field(m, "id"), field(m, "name"), field(m, "kind"), field(m, "host"), m["enabled"], m["events"])
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\t%s\n", field(m, "created_at"), field(m, "channel_id"), field(m, "event"), field(m, "status"), m["http_status"], field(m, "error"))
		}
	}
	return w.Flush()
}
