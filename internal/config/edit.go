package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"
)

func Get(b []byte, key string) (string, error) {
	if _, ok := environment[key]; !ok {
		return "", fmt.Errorf("unknown daemon setting %q", key)
	}
	var values map[string]any
	if _, err := toml.Decode(string(b), &values); err != nil {
		return "", err
	}
	v, ok := values[key]
	if !ok {
		return "", fmt.Errorf("%s is not set in this file", key)
	}
	encoded, err := json.Marshal(v)
	return string(encoded), err
}

func Edit(path, key string, values []string, unset bool) error {
	d := Defaults()
	if _, ok := d.lists()[key]; ok && !unset {
		list := []string{}
		for _, v := range values {
			list = append(list, strings.Split(v, ",")...)
		}
		values = list
	}
	if unset {
		values = nil
	} else if values == nil {
		values = []string{}
	}
	return EditSettings(path, map[string][]string{key: values})
}

// EditSettings writes all changes atomically. A nil value unsets a key.
func EditSettings(path string, changes map[string][]string) error {
	b, err := Read(path)
	if err != nil {
		return err
	}
	d := Defaults()
	if err = Parse(b, &d); err != nil {
		return err
	}
	for key, values := range changes {
		if _, ok := environment[key]; !ok {
			return fmt.Errorf("unknown daemon setting %q", key)
		}
		if values == nil {
			continue
		}
		switch {
		case d.strings()[key] != nil:
			if len(values) != 1 {
				return fmt.Errorf("%s requires exactly one value", key)
			}
			*d.strings()[key] = values[0]
		case d.bools()[key] != nil:
			if len(values) != 1 {
				return fmt.Errorf("%s requires exactly one value", key)
			}
			value, err := strconv.ParseBool(values[0])
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			*d.bools()[key] = value
		default:
			*d.lists()[key] = append([]string{}, values...)
		}
	}
	content := string(b)
	for key, values := range changes {
		var replacement string
		if values != nil {
			var value any
			switch {
			case d.strings()[key] != nil:
				value = *d.strings()[key]
			case d.bools()[key] != nil:
				value = *d.bools()[key]
			default:
				list := *d.lists()[key]
				for i := range list {
					list[i] = strings.TrimSpace(list[i])
				}
				value = list
			}
			var out bytes.Buffer
			if err = toml.NewEncoder(&out).Encode(map[string]any{key: value}); err != nil {
				return err
			}
			replacement = out.String()
		}
		content = replaceSetting(content, key, replacement)
	}
	check := Defaults()
	if err = Parse([]byte(content), &check); err != nil {
		return err
	}
	if err = check.Validate(); err != nil {
		return err
	}
	return writeSettings(path, content)
}

func replaceSetting(content, key, replacement string) string {
	lines := strings.SplitAfter(content, "\n")
	start, end := -1, -1
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for j := i; j < len(lines); j++ {
			var v map[string]any
			if _, err := toml.Decode(strings.Join(lines[i:j+1], ""), &v); err == nil {
				if _, ok := v[key]; ok {
					start, end = i, j+1
				}
				i = j
				break
			}
		}
		if start >= 0 {
			break
		}
	}
	if start >= 0 {
		return strings.Join(lines[:start], "") + replacement + strings.Join(lines[end:], "")
	}
	if replacement != "" && content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + replacement
}

func writeSettings(path, content string) error {
	mode := os.FileMode(0644)
	var owner *syscall.Stat_t
	if stat, err := os.Stat(path); err == nil {
		mode = stat.Mode().Perm()
		owner, _ = stat.Sys().(*syscall.Stat_t)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".startship-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if owner != nil && os.Geteuid() == 0 {
		err = f.Chown(int(owner.Uid), int(owner.Gid))
	}
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		_, err = f.WriteString(content)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
