package cli

import (
	"fmt"
	"strings"

	daemonconfig "github.com/sammwyy/startship/internal/config"
)

func (c *cli) runConfig(args []string) error {
	var file string
	var rest []string
	for i := 0; i < len(args); i++ {
		s := args[i]
		if s == "--config" {
			i++
			if i == len(args) {
				return fmt.Errorf("--config requires a path")
			}
			file = args[i]
		} else if strings.HasPrefix(s, "--config=") {
			file = strings.TrimPrefix(s, "--config=")
		} else {
			rest = append(rest, s)
		}
	}
	path := daemonconfig.Path(file)
	if len(rest) == 0 {
		return fmt.Errorf("usage: startship config [--config PATH] path|show|get KEY|set KEY VALUE...|unset KEY")
	}
	switch rest[0] {
	case "path":
		if len(rest) != 1 {
			break
		}
		fmt.Fprintln(c.out, path)
		return nil
	case "show":
		if len(rest) != 1 {
			break
		}
		b, err := daemonconfig.Read(path)
		if err != nil {
			return err
		}
		fmt.Fprint(c.out, string(b))
		return nil
	case "get":
		if len(rest) != 2 {
			break
		}
		b, err := daemonconfig.Read(path)
		if err != nil {
			return err
		}
		value, err := daemonconfig.Get(b, rest[1])
		if err != nil {
			return err
		}
		fmt.Fprintln(c.out, value)
		return nil
	case "set", "unset":
		if rest[0] == "set" && len(rest) < 3 || rest[0] == "unset" && len(rest) != 2 {
			break
		}
		if err := daemonconfig.Edit(path, rest[1], rest[2:], rest[0] == "unset"); err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Updated %s. Restart the daemon: sudo systemctl restart startshipd\nEnvironment variables and daemon flags override file settings.\n", path)
		return nil
	}
	return fmt.Errorf("invalid config arguments; run startship config")
}
