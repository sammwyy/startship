package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if _, ok := environment[key]; !ok {
		return fmt.Errorf("unknown daemon setting %q", key)
	}
	b, err := Read(path)
	if err != nil {
		return err
	}
	d := Defaults()
	if err = Parse(b, &d); err != nil {
		return err
	}
	var replacement string
	if !unset {
		if p, ok := d.strings()[key]; ok {
			if len(values) != 1 {
				return fmt.Errorf("%s requires exactly one value", key)
			}
			*p = values[0]
		} else {
			list := []string{}
			for _, v := range values {
				list = append(list, strings.Split(v, ",")...)
			}
			*d.lists()[key] = list
		}
		if err = d.Validate(); err != nil {
			return err
		}
		var value any
		if p, ok := d.strings()[key]; ok {
			value = *p
		} else {
			value = *d.lists()[key]
		}
		var out bytes.Buffer
		if err = toml.NewEncoder(&out).Encode(map[string]any{key: value}); err != nil {
			return err
		}
		replacement = out.String()
	}
	lines := strings.SplitAfter(string(b), "\n")
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
	var content string
	if start >= 0 {
		if end < 0 {
			return fmt.Errorf("cannot locate complete TOML value for %s", key)
		}
		content = strings.Join(lines[:start], "") + replacement + strings.Join(lines[end:], "")
	} else {
		content = string(b)
		if replacement != "" {
			if content != "" && !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			content += replacement
		}
	}
	check := Defaults()
	if err = Parse([]byte(content), &check); err != nil {
		return err
	}
	if err = check.Validate(); err != nil {
		return err
	}
	mode := os.FileMode(0644)
	if stat, err := os.Stat(path); err == nil {
		mode = stat.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".startship-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
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
