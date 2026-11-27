package cli

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	daemonconfig "github.com/sammwyy/startship/internal/config"
)

func listenPort(entries []any, fallback string) string {
	for _, entry := range entries {
		if s, ok := entry.(string); ok {
			if _, port, err := net.SplitHostPort(strings.TrimSpace(s)); err == nil && port != "" {
				return port
			}
		}
	}
	return fallback
}

func validatePort(port string) (string, error) {
	port = strings.TrimSpace(port)
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return "", fmt.Errorf("invalid port %q", port)
	}
	return strconv.Itoa(n), nil
}

func buildInterfaceListens(raw, port string) ([]string, error) {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	anyFound := false
	for _, part := range parts {
		iface := strings.TrimSpace(part)
		if iface == "" {
			return nil, fmt.Errorf("empty interface entry")
		}
		switch strings.ToLower(iface) {
		case "any", "all", "*":
			anyFound = true
			out = append(out, ":"+port)
		default:
			addr := net.JoinHostPort(iface, port)
			if err := daemonconfig.ValidateListen(addr); err != nil {
				return nil, err
			}
			out = append(out, addr)
		}
	}
	if anyFound && len(out) > 1 {
		return nil, fmt.Errorf("cannot combine %q with specific interfaces", "any")
	}
	return out, nil
}

func replaceListenPort(entries []any, port string) ([]string, error) {
	if len(entries) == 0 {
		return []string{":" + port}, nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		s, ok := entry.(string)
		if !ok {
			return nil, fmt.Errorf("invalid listen address entry")
		}
		host, _, err := net.SplitHostPort(strings.TrimSpace(s))
		if err != nil {
			return nil, err
		}
		if host == "" {
			out = append(out, ":"+port)
		} else {
			out = append(out, net.JoinHostPort(host, port))
		}
	}
	return out, nil
}

func (c *cli) runInterfaces(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: interfaces")
	}
	v, err := c.request("GET", "system/interfaces", nil)
	if err != nil {
		return err
	}
	if c.raw {
		return c.print(v)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return c.print(v)
	}
	list, ok := m["interfaces"].([]any)
	if !ok {
		return c.print(v)
	}
	if len(list) == 0 {
		fmt.Fprintln(c.out, "none")
		return nil
	}
	for _, item := range list {
		iface, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := iface["name"].(string)
		up, _ := iface["up"].(bool)
		loopback, _ := iface["loopback"].(bool)
		var addrs []string
		if rawAddrs, ok := iface["addresses"].([]any); ok {
			for _, a := range rawAddrs {
				if s, ok := a.(string); ok {
					addrs = append(addrs, s)
				}
			}
		}
		state := "down"
		if up {
			state = "up"
		}
		if loopback {
			state += ",loopback"
		}
		fmt.Fprintf(c.out, "%s\t%s\t%s\n", name, state, strings.Join(addrs, ","))
	}
	return nil
}

func (c *cli) runDashboard(args []string) error {
	const path = "system/dashboard"
	const usage = "usage: dashboard [show | set [--listen ADDR[,ADDR]] [--interface IFACE[,IFACE]|any] [--port PORT]]"
	op := "show"
	if len(args) > 0 {
		op, args = args[0], args[1:]
	}
	var listenEntries []string
	var ifaceValue string
	var portValue string
	switch op {
	case "show":
		if len(args) != 0 {
			return fmt.Errorf("%s", usage)
		}
	case "set":
		opts, err := parseAccessArgs(args, "listen interface port")
		if err != nil {
			return err
		}
		if len(opts.positional) != 0 || len(opts.flags) == 0 {
			return fmt.Errorf("%s", usage)
		}
		if opts.has("listen") && (opts.has("interface") || opts.has("port")) {
			return fmt.Errorf("cannot combine --listen with --interface or --port")
		}
		if opts.has("listen") {
			entries := strings.Split(opts.value("listen"), ",")
			for i := range entries {
				entries[i] = strings.TrimSpace(entries[i])
				if err := daemonconfig.ValidateListen(entries[i]); err != nil {
					return fmt.Errorf("--listen: %w", err)
				}
			}
			listenEntries = entries
		}
		if opts.has("port") {
			p, err := validatePort(opts.value("port"))
			if err != nil {
				return fmt.Errorf("--port: %w", err)
			}
			portValue = p
		}
		if opts.has("interface") {
			ifaceValue = opts.value("interface")
			if _, err := buildInterfaceListens(ifaceValue, "8080"); err != nil {
				return fmt.Errorf("--interface: %w", err)
			}
		}
	default:
		return fmt.Errorf("unknown dashboard command %q; %s", op, usage)
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
		return fmt.Errorf("invalid dashboard settings response")
	}
	currentListen, isList := m["dashboard_listen"].([]any)
	if !isList {
		return fmt.Errorf("invalid dashboard settings response")
	}
	nextListen := listenEntries
	if len(nextListen) == 0 {
		port := portValue
		if port == "" {
			port = listenPort(currentListen, "8080")
		}
		if ifaceValue != "" {
			nextListen, err = buildInterfaceListens(ifaceValue, port)
		} else {
			nextListen, err = replaceListenPort(currentListen, port)
		}
		if err != nil {
			return err
		}
	}
	result, err := c.request("PUT", path, map[string]any{"dashboard_listen": nextListen})
	if err != nil {
		return err
	}
	return c.print(result)
}
