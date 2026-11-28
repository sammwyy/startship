package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

func cliFileTarget(target string) (string, error) {
	kind, id, ok := strings.Cut(target, ":")
	if ok && id != "" {
		switch kind {
		case "volume", "container":
			if !strings.Contains(id, "/") && id != "." && id != ".." {
				return kind + "s/" + url.PathEscape(id) + "/files/", nil
			}
		case "app", "app-volume":
			parts := strings.Split(id, "/")
			if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
				for i, part := range parts {
					if part == "." || part == ".." {
						return "", errors.New("invalid app file target")
					}
					parts[i] = url.PathEscape(part)
				}
				resource := "containers"
				if kind == "app-volume" {
					resource = "volumes"
				}
				return fmt.Sprintf("workspaces/%s/apps/%s/%s/%s/files/", parts[0], parts[1], resource, parts[2]), nil
			}
		}
	}
	return "", errors.New("file target must be volume:ID, container:ID, app:WS/ID/CONTAINER or app-volume:WS/ID/VOLUME")
}

func cliFilePath(base, op, name, format string) string {
	q := url.Values{"path": {name}}
	if format != "" {
		q.Set("format", format)
	}
	return base + op + "?" + q.Encode()
}

func (c *cli) fileTransfer(method, endpoint string, src io.ReadSeeker, receive func(*http.Response) error) error {
	resp, err := c.doAPIReader(method, endpoint, src, "application/octet-stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var body struct{ Error string }
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body)
		if body.Error != "" {
			return fmt.Errorf("%s: %s", resp.Status, body.Error)
		}
		return fmt.Errorf("API: %s", resp.Status)
	}
	if err := c.readCredentials(resp); err != nil {
		return err
	}
	if receive != nil {
		return receive(resp)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}

func (c *cli) runFiles(args []string) error {
	usage := errors.New("usage: startship files ls|cat|get|put|rm|mkdir|mv|edit|zip|unzip TARGET [PATH ...]; see the README for examples")
	if len(args) < 2 {
		return usage
	}
	base, err := cliFileTarget(args[1])
	if err != nil {
		return err
	}
	p := args[2:]
	endpoint := func(op, name string) string { return cliFilePath(base, op, name, "") }
	mutate := func(method, op, name string, body any) error {
		_, err := c.request(method, endpoint(op, name), body)
		return err
	}
	switch args[0] {
	case "ls":
		name := "/"
		if len(p) == 1 {
			name = p[0]
		} else if len(p) > 1 {
			return usage
		}
		result, err := c.request("GET", endpoint("list", name), nil)
		if err != nil {
			return err
		}
		if c.raw {
			return json.NewEncoder(c.out).Encode(result)
		}
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		var listing struct {
			Entries []struct {
				Name, Type, Mtime, Link string
				Mode, Size              int64
			}
		}
		if err := json.Unmarshal(b, &listing); err != nil {
			return err
		}
		out := tabwriter.NewWriter(c.out, 0, 2, 2, ' ', 0)
		fmt.Fprintln(out, "TYPE\tMODE\tSIZE\tMODIFIED\tNAME")
		for _, e := range listing.Entries {
			name := e.Name
			if e.Link != "" {
				name += " -> " + e.Link
			}
			fmt.Fprintf(out, "%s\t%04o\t%d\t%s\t%s\n", e.Type, e.Mode, e.Size, e.Mtime, name)
		}
		return out.Flush()
	case "cat":
		if len(p) != 1 {
			return usage
		}
		return c.fileTransfer("GET", endpoint("download", p[0]), nil, func(resp *http.Response) error {
			_, err := io.Copy(c.out, resp.Body)
			return err
		})
	case "get":
		if len(p) != 2 {
			return usage
		}
		format := "zip"
		if strings.HasSuffix(p[1], ".tar.gz") || strings.HasSuffix(p[1], ".tgz") {
			format = "tar.gz"
		}
		return c.fileTransfer("GET", cliFilePath(base, "download", p[0], format), nil, func(resp *http.Response) error {
			f, err := os.OpenFile(p[1], os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, resp.Body)
			err = errors.Join(err, f.Close())
			if err != nil {
				_ = os.Remove(p[1])
			}
			return err
		})
	case "put":
		if len(p) != 2 {
			return usage
		}
		f, err := os.Open(p[0])
		if err != nil {
			return err
		}
		defer f.Close()
		return c.fileTransfer("PUT", endpoint("upload", p[1]), f, nil)
	case "rm", "mkdir", "touch":
		if len(p) != 1 {
			return usage
		}
		switch args[0] {
		case "rm":
			return mutate("DELETE", "delete", p[0], nil)
		case "mkdir":
			return mutate("POST", "mkdir", p[0], nil)
		default:
			return mutate("POST", "create", p[0], nil)
		}
	case "mv":
		if len(p) != 2 {
			return usage
		}
		return mutate("POST", "move", p[0], map[string]string{"to": p[1]})
	case "edit":
		if len(p) != 1 {
			return usage
		}
		return c.editFile(endpoint("text", p[0]), endpoint("save", p[0]), p[0])
	case "zip":
		if len(p) < 2 {
			return usage
		}
		format := "zip"
		if strings.HasSuffix(p[0], ".tar.gz") {
			format = "tar.gz"
		}
		return mutate("POST", "compress", p[0], map[string]any{"format": format, "paths": p[1:]})
	case "unzip":
		if len(p) < 1 || len(p) > 2 {
			return usage
		}
		to := ""
		if len(p) == 2 {
			to = p[1]
		}
		return mutate("POST", "extract", p[0], map[string]string{"to": to})
	default:
		return usage
	}
}

func (c *cli) editFile(read, write, name string) error {
	result, err := c.request("GET", read, nil)
	if err != nil {
		return err
	}
	m, _ := result.(map[string]any)
	text, ok := m["text"].(string)
	if !ok {
		return errors.New("invalid file text response")
	}
	dir, err := os.MkdirTemp("", "startship-edit-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	local := filepath.Join(dir, path.Base(name))
	if err := os.WriteFile(local, []byte(text), 0600); err != nil {
		return err
	}
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) == 0 {
		editor = []string{"vi"}
	}
	cmd := exec.Command(editor[0], append(editor[1:], local)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	return c.fileTransfer("PUT", write, f, nil)
}
