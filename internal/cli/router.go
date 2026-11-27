package cli

import (
	"fmt"
	"strings"

	daemonconfig "github.com/sammwyy/startship/internal/config"
)

func (c *cli) runRouter(args []string) error {
	const path = "system/router"
	const usage = "usage: router [show | set [--http ADDR[,ADDR]] [--https ADDR[,ADDR]] | disable | enable]"
	op := "show"
	if len(args) > 0 {
		op, args = args[0], args[1:]
	}
	changes := map[string]any{}
	switch op {
	case "show", "enable", "disable":
		if len(args) != 0 {
			return fmt.Errorf("%s", usage)
		}
		if op != "show" {
			changes["enabled"] = op == "enable"
		}
	case "set":
		opts, err := parseAccessArgs(args, "http https")
		if err != nil {
			return err
		}
		if len(opts.positional) != 0 || len(opts.flags) == 0 {
			return fmt.Errorf("%s", usage)
		}
		for _, flag := range []string{"http", "https"} {
			if !opts.has(flag) {
				continue
			}
			entries := strings.Split(opts.value(flag), ",")
			for i := range entries {
				entries[i] = strings.TrimSpace(entries[i])
				if err := daemonconfig.ValidateListen(entries[i]); err != nil {
					return fmt.Errorf("--%s: %w", flag, err)
				}
			}
			changes[flag+"_listen"] = entries
		}
	default:
		return fmt.Errorf("unknown router command %q; %s", op, usage)
	}

	current, err := c.request("GET", path, nil)
	if err != nil {
		return err
	}
	if op == "show" {
		return c.print(current)
	}
	m, ok := current.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid router settings response")
	}
	enabled, isBool := m["enabled"].(bool)
	httpListen, isHTTPList := m["http_listen"].([]any)
	httpsListen, isHTTPSList := m["https_listen"].([]any)
	if !isBool || !isHTTPList || !isHTTPSList {
		return fmt.Errorf("invalid router settings response")
	}
	body := map[string]any{"enabled": enabled, "http_listen": httpListen, "https_listen": httpsListen}
	for key, value := range changes {
		body[key] = value
	}
	result, err := c.request("PUT", path, body)
	if err != nil {
		return err
	}
	return c.print(result)
}
