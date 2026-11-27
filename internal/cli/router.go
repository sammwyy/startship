package cli

import (
	"fmt"
	"strings"

	daemonconfig "github.com/sammwyy/startship/internal/config"
)

func (c *cli) runRouter(args []string) error {
	const path = "system/router"
	const usage = "usage: router [show | set [--http ADDR[,ADDR]] [--https ADDR[,ADDR]] [--interface IFACE[,IFACE]|any] [--http-port PORT] [--https-port PORT] | disable | enable]"
	op := "show"
	if len(args) > 0 {
		op, args = args[0], args[1:]
	}
	changes := map[string]any{}
	var ifaceValue, httpPortValue, httpsPortValue string
	switch op {
	case "show", "enable", "disable":
		if len(args) != 0 {
			return fmt.Errorf("%s", usage)
		}
		if op != "show" {
			changes["enabled"] = op == "enable"
		}
	case "set":
		opts, err := parseAccessArgs(args, "http https interface http-port https-port")
		if err != nil {
			return err
		}
		if len(opts.positional) != 0 || len(opts.flags) == 0 {
			return fmt.Errorf("%s", usage)
		}
		if opts.has("interface") && (opts.has("http") || opts.has("https")) {
			return fmt.Errorf("cannot combine --interface with --http or --https")
		}
		if opts.has("http-port") && opts.has("http") {
			return fmt.Errorf("cannot combine --http-port with --http")
		}
		if opts.has("https-port") && opts.has("https") {
			return fmt.Errorf("cannot combine --https-port with --https")
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
		if opts.has("http-port") {
			p, err := validatePort(opts.value("http-port"))
			if err != nil {
				return fmt.Errorf("--http-port: %w", err)
			}
			httpPortValue = p
		}
		if opts.has("https-port") {
			p, err := validatePort(opts.value("https-port"))
			if err != nil {
				return fmt.Errorf("--https-port: %w", err)
			}
			httpsPortValue = p
		}
		if opts.has("interface") {
			ifaceValue = opts.value("interface")
			if _, err := buildInterfaceListens(ifaceValue, "80"); err != nil {
				return fmt.Errorf("--interface: %w", err)
			}
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
	if ifaceValue != "" {
		httpPort := httpPortValue
		if httpPort == "" {
			httpPort = listenPort(httpListen, "80")
		}
		httpsPort := httpsPortValue
		if httpsPort == "" {
			httpsPort = listenPort(httpsListen, "443")
		}
		nextHTTP, err := buildInterfaceListens(ifaceValue, httpPort)
		if err != nil {
			return err
		}
		nextHTTPS, err := buildInterfaceListens(ifaceValue, httpsPort)
		if err != nil {
			return err
		}
		changes["http_listen"] = nextHTTP
		changes["https_listen"] = nextHTTPS
	} else {
		if httpPortValue != "" {
			nextHTTP, err := replaceListenPort(httpListen, httpPortValue)
			if err != nil {
				return err
			}
			changes["http_listen"] = nextHTTP
		}
		if httpsPortValue != "" {
			nextHTTPS, err := replaceListenPort(httpsListen, httpsPortValue)
			if err != nil {
				return err
			}
			changes["https_listen"] = nextHTTPS
		}
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
