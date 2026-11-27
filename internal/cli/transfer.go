package cli

import (
	"fmt"
	"slices"
	"strings"
)

func (c *cli) runTransfer(args []string) error {
	usage := "transfer plan KIND ID | transfer copy|move KIND ID DESTINATION [--only | --include KIND:ID ...]"
	kinds := []string{"projects", "apps", "services", "volumes"}
	if len(args) < 3 || !slices.Contains(kinds, args[1]) {
		return fmt.Errorf("usage: %s", usage)
	}
	method, kind, id := args[0], args[1], args[2]
	body := map[string]any{"resource": map[string]string{"kind": kind, "id": id}}
	switch {
	case method == "plan" && len(args) == 3:
	case (method == "copy" || method == "move") && len(args) >= 4:
		include := []map[string]string{}
		flags := args[4:]
		if len(flags) != 1 || flags[0] != "--only" {
			for len(flags) > 0 {
				if len(flags) < 2 || flags[0] != "--include" {
					return fmt.Errorf("usage: %s", usage)
				}
				linkKind, linkID, _ := strings.Cut(flags[1], ":")
				if !slices.Contains(kinds, linkKind) || linkID == "" {
					return fmt.Errorf("invalid linked resource %q; expected KIND:ID", flags[1])
				}
				include = append(include, map[string]string{"kind": linkKind, "id": linkID})
				flags = flags[2:]
			}
		}
		ws, err := c.resolve("workspaces", args[3])
		if err != nil {
			return err
		}
		body["destination_workspace_id"] = ws
		if len(args) > 4 {
			body["include"] = include
		}
	default:
		return fmt.Errorf("usage: %s", usage)
	}
	result, err := c.request("POST", "transfers/"+method, body)
	if err != nil {
		return err
	}
	return c.print(result)
}
