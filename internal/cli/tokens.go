package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const tokenUsage = "use account token create NAME [--workspaces W1,W2 | --all] [--admin] [--expires DAYS] [--no-api] [--no-mcp] | account token revoke TOKEN_ID"

func (c *cli) runToken(args []string) error {
	if len(args) == 2 && args[0] == "revoke" {
		return c.printRequest("DELETE", "me/tokens/"+url.PathEscape(args[1]), nil)
	}
	if len(args) < 2 || args[0] != "create" {
		return errors.New(tokenUsage)
	}
	body := map[string]any{"name": args[1], "access": "operate", "api": true, "mcp": true, "expires_in_days": 90}
	var workspaces []string
	all := false
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--all":
			all = true
		case "--admin":
			body["access"] = "admin"
		case "--no-api":
			body["api"] = false
		case "--no-mcp":
			body["mcp"] = false
		case "--workspaces", "--expires":
			if i+1 == len(args) {
				return errors.New(tokenUsage)
			}
			i++
			if args[i-1] == "--expires" {
				days, err := strconv.Atoi(args[i])
				if err != nil {
					return fmt.Errorf("--expires takes a number of days (0 never expires)")
				}
				body["expires_in_days"] = days
				continue
			}
			for _, w := range strings.Split(args[i], ",") {
				id, err := c.resolve("workspaces", strings.TrimSpace(w))
				if err != nil {
					return err
				}
				workspaces = append(workspaces, id)
			}
		default:
			return errors.New(tokenUsage)
		}
	}
	if all == (len(workspaces) > 0) {
		return errors.New("choose --workspaces or --all")
	}
	body["all_workspaces"], body["workspaces"] = all, workspaces
	v, err := c.request("POST", "me/tokens", body)
	if err != nil {
		return err
	}
	m, _ := v.(map[string]any)
	fmt.Fprintf(c.out, "Created token %v. Copy it now; it is not shown again:\n\n  %v\n\nUse it as STARTSHIP_TOKEN or as an MCP Bearer token for %s/api/mcp\n", m["name"], m["token"], strings.TrimRight(c.Server, "/"))
	return nil
}
