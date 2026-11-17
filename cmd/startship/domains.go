package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func (c *cli) runDomains(args []string) error {
	var positional []string
	subdomains, hasSubdomains := false, false
	for _, arg := range args {
		switch arg {
		case "--subdomains", "--subdomains=true", "--subdomains=false":
			if hasSubdomains {
				return errors.New("--subdomains may only be supplied once")
			}
			hasSubdomains, subdomains = true, arg != "--subdomains=false"
		default:
			if strings.HasPrefix(arg, "--") {
				return fmt.Errorf("unknown domain option %s", arg)
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) < 2 {
		return errors.New("use domains list WORKSPACE, add WORKSPACE DOMAIN [--subdomains], or rm WORKSPACE DOMAIN")
	}
	op := positional[0]
	expected := 3
	switch op {
	case "list":
		expected = 2
	case "add", "rm", "delete", "remove":
	default:
		return fmt.Errorf("unknown domains action %q; supported: list, add, rm", op)
	}
	if len(positional) != expected {
		return errors.New("invalid domains arguments; run startship help")
	}
	if hasSubdomains && op != "add" {
		return errors.New("--subdomains is only valid with domains add")
	}
	workspaceID, err := c.resolve("workspaces", positional[1])
	if err != nil {
		return err
	}
	path := "workspaces/" + url.PathEscape(workspaceID) + "/domains"
	method := "GET"
	var body any
	if op == "add" {
		method, body = "POST", map[string]any{"name": positional[2], "subdomains": subdomains}
	} else if op != "list" {
		v, err := c.request("GET", path, nil)
		if err != nil {
			return err
		}
		rows, err := domainList(v)
		if err != nil {
			return err
		}
		id := ""
		name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(positional[2])), "*.")
		name = strings.TrimSuffix(name, ".")
		for _, row := range rows {
			domain, _ := row.(map[string]any)
			if field(domain, "id") == positional[2] {
				id = positional[2]
				break
			}
			if field(domain, "name") == name {
				if id != "" {
					return errors.New("ambiguous domain name; use an ID")
				}
				id = field(domain, "id")
			}
		}
		if id == "" {
			return fmt.Errorf("workspace domain %q not found", positional[2])
		}
		method, path = "DELETE", path+"/"+url.PathEscape(id)
	}
	v, err := c.request(method, path, body)
	if err != nil {
		return err
	}
	if op != "list" || c.raw {
		return c.print(v)
	}
	rows, err := domainList(v)
	if err != nil {
		return err
	}
	m := v.(map[string]any)
	fmt.Fprintf(c.out, "Can manage: %v\n", m["can_manage"])
	return c.printAccess(rows, "id", "name", "apex", "subdomains", "routes")
}

func domainList(v any) ([]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("expected workspace domains response")
	}
	rows, ok := m["domains"].([]any)
	if !ok {
		return nil, errors.New("expected workspace domains list")
	}
	return rows, nil
}
