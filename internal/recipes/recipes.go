package recipes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/sammwyy/startship/registry"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
var variable = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z_0-9]*)\}`)
var imageName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$`)
var propertyName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z_0-9.-]*$`)

type Registry map[string]Recipe

type Recipe struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Icon        string      `json:"icon"`
	Logo        string      `json:"logo,omitempty"`
	Category    string      `json:"category,omitempty"`
	Description string      `json:"description"`
	Version     string      `json:"version"`
	Containers  []Container `json:"containers"`
	Volumes     []string    `json:"volumes"`
	HTTP        []Endpoint  `json:"http"`
	Ports       []Port      `json:"ports"`
	Config      Schema      `json:"config_schema"`
	Files       []File      `json:"files"`
}

type Container struct {
	ID          string            `json:"id"`
	Image       string            `json:"image"`
	Entrypoint  string            `json:"entrypoint,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Mounts      map[string]string `json:"mounts,omitempty"`
	Healthcheck string            `json:"healthcheck,omitempty"`
}

type Endpoint struct {
	ID        string `json:"id"`
	Container string `json:"container"`
	Port      int    `json:"port"`
}

type Port struct {
	Endpoint
	Protocol string `json:"protocol"`
	HostPort int    `json:"host_port"`
}

type File struct {
	ID       string         `json:"id"`
	Volume   string         `json:"volume"`
	Path     string         `json:"path"`
	Format   string         `json:"format"`
	Schema   Schema         `json:"schema"`
	Defaults map[string]any `json:"defaults,omitempty"`
}

type Schema struct {
	Type       string           `json:"type"`
	Properties map[string]Field `json:"properties"`
	Required   []string         `json:"required,omitempty"`
	Additional bool             `json:"additionalProperties"`
}

type Field struct {
	Type        string   `json:"type"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default,omitempty"`
	Enum        []any    `json:"enum,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
	MinLength   *int     `json:"minLength,omitempty"`
	MaxLength   *int     `json:"maxLength,omitempty"`
}

func (f Field) valid(v any) bool {
	switch f.Type {
	case "string":
		s, ok := v.(string)
		if !ok || (f.MinLength != nil && len([]rune(s)) < *f.MinLength) || (f.MaxLength != nil && len([]rune(s)) > *f.MaxLength) {
			return false
		}
	case "integer", "number":
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || (f.Type == "integer" && math.Trunc(n) != n) || (f.Minimum != nil && n < *f.Minimum) || (f.Maximum != nil && n > *f.Maximum) {
			return false
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return false
		}
	default:
		return false
	}
	return len(f.Enum) == 0 || slices.ContainsFunc(f.Enum, func(x any) bool { return x == v })
}

func (s Schema) Validate(values map[string]any, defaults bool) (map[string]any, error) {
	out := make(map[string]any, len(values))
	maps.Copy(out, values)
	if defaults {
		for k, f := range s.Properties {
			if _, ok := out[k]; !ok && f.Default != nil {
				out[k] = f.Default
			}
		}
	}
	for k, value := range out {
		if f, ok := s.Properties[k]; !ok || !f.valid(value) {
			return nil, fmt.Errorf("invalid %s", k)
		}
	}
	for _, k := range s.Required {
		if _, ok := out[k]; !ok {
			return nil, fmt.Errorf("%s is required", k)
		}
	}
	return out, nil
}

func (s Schema) definition() bool {
	if s.Type != "object" || s.Additional {
		return false
	}
	for k, f := range s.Properties {
		if !propertyName.MatchString(k) || !slices.Contains([]string{"string", "integer", "number", "boolean"}, f.Type) {
			return false
		}
		for _, v := range f.Enum {
			switch v.(type) {
			case string, bool, float64:
			default:
				return false
			}
		}
		if f.Default != nil && !f.valid(f.Default) {
			return false
		}
	}
	for _, k := range s.Required {
		if _, ok := s.Properties[k]; !ok {
			return false
		}
	}
	return true
}

func Expand(s string, values map[string]any) string {
	return variable.ReplaceAllStringFunc(s, func(m string) string {
		v := values[m[2:len(m)-1]]
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	})
}

func (r Recipe) validate() error {
	if !identifier.MatchString(r.ID) || r.Name == "" || r.Version == "" || len(r.Containers) == 0 || !r.Config.definition() {
		return fmt.Errorf("invalid recipe metadata or install schema")
	}
	volumes := map[string]bool{}
	for _, id := range r.Volumes {
		if !identifier.MatchString(id) || volumes[id] {
			return fmt.Errorf("invalid volume %q", id)
		}
		volumes[id] = true
	}
	containers := map[string]bool{}
	for _, c := range r.Containers {
		if !identifier.MatchString(c.ID) || containers[c.ID] || !imageName.MatchString(c.Image) {
			return fmt.Errorf("invalid container %q", c.ID)
		}
		if c.Entrypoint != "" && (!path.IsAbs(c.Entrypoint) || path.Clean(c.Entrypoint) != c.Entrypoint || strings.ContainsAny(c.Entrypoint, "\x00\r\n")) {
			return fmt.Errorf("invalid entrypoint for container %q", c.ID)
		}
		containers[c.ID] = true
		for id, mount := range c.Mounts {
			if !volumes[id] || !strings.HasPrefix(mount, "/") || mount == "/" || path.Clean(mount) != mount || strings.ContainsAny(mount, ":,\x00\r\n") {
				return fmt.Errorf("invalid mount %q", mount)
			}
		}
	}
	endpoints := map[string]bool{}
	check := func(e Endpoint) bool {
		if !identifier.MatchString(e.ID) || endpoints[e.ID] || !containers[e.Container] || e.Port < 1 || e.Port > 65535 {
			return false
		}
		endpoints[e.ID] = true
		return true
	}
	for _, e := range r.HTTP {
		if !check(e) {
			return fmt.Errorf("invalid HTTP endpoint %q", e.ID)
		}
	}
	for _, p := range r.Ports {
		if !check(p.Endpoint) || (p.Protocol != "tcp" && p.Protocol != "udp") || p.HostPort < 0 || p.HostPort > 65535 {
			return fmt.Errorf("invalid published port %q", p.ID)
		}
	}
	files := map[string]bool{}
	for _, f := range r.Files {
		if !identifier.MatchString(f.ID) || files[f.ID] || !volumes[f.Volume] || !f.Schema.definition() ||
			(f.Format != "json" && f.Format != "properties" && f.Format != "ini") ||
			f.Path == "." || path.Clean(f.Path) != f.Path ||
			strings.HasPrefix(f.Path, "/") || f.Path == ".." || strings.HasPrefix(f.Path, "../") || strings.ContainsAny(f.Path, "\\:\x00\r\n") {
			return fmt.Errorf("invalid config file %q", f.ID)
		}
		files[f.ID] = true
		if _, err := f.Schema.Validate(f.Defaults, true); err != nil {
			return fmt.Errorf("defaults for %s: %w", f.ID, err)
		}
	}
	return nil
}

func Builtins() Registry {
	out, err := Parse(registry.Recipes)
	if err != nil {
		panic(fmt.Errorf("built-in recipes: %w", err))
	}
	return out
}

func (r Registry) All() []Recipe {
	out := make([]Recipe, 0, len(r))
	for _, id := range slices.Sorted(maps.Keys(r)) {
		out = append(out, r[id])
	}
	return out
}

func Parse(body []byte) (Registry, error) {
	var index struct {
		Recipes []Recipe `json:"recipes"`
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&index); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("registry must contain one JSON object")
	}
	if index.Recipes == nil {
		return nil, fmt.Errorf("registry requires a recipes array")
	}
	entries := Registry{}
	for _, recipe := range index.Recipes {
		if err := recipe.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", recipe.ID, err)
		}
		if _, duplicate := entries[recipe.ID]; duplicate {
			return nil, fmt.Errorf("duplicate recipe %s", recipe.ID)
		}
		entries[recipe.ID] = recipe
	}
	return entries, nil
}

func (f File) Read(data string) (map[string]any, error) {
	values := map[string]any{}
	if f.Format == "json" {
		if err := json.Unmarshal([]byte(data), &values); err != nil {
			return nil, err
		}
	} else {
		for _, line := range strings.Split(data, "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
			field, known := f.Schema.Properties[strings.TrimSpace(k)]
			if !ok || !known {
				continue
			}
			switch field.Type {
			case "integer", "number":
				n, err := strconv.ParseFloat(v, 64)
				if err != nil {
					return nil, err
				}
				values[strings.TrimSpace(k)] = n
			case "boolean":
				b, err := strconv.ParseBool(v)
				if err != nil {
					return nil, err
				}
				values[strings.TrimSpace(k)] = b
			default:
				values[strings.TrimSpace(k)] = v
			}
		}
	}
	return f.Schema.Validate(values, true)
}

func (f File) Write(existing string, values map[string]any) (string, error) {
	values, err := f.Schema.Validate(values, true)
	if err != nil {
		return "", err
	}
	if f.Format == "json" {
		b, err := json.MarshalIndent(values, "", "  ")
		return string(b) + "\n", err
	}
	lines := strings.Split(strings.TrimRight(existing, "\n"), "\n")
	out := make([]string, 0, len(lines)+len(values))
	for _, line := range lines {
		k, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		if _, managed := f.Schema.Properties[strings.TrimSpace(k)]; line != "" && (!ok || !managed) {
			out = append(out, line)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(values)) {
		v := fmt.Sprint(values[k])
		if strings.ContainsAny(v, "\r\n\\\x00") {
			return "", fmt.Errorf("invalid characters in %s", k)
		}
		out = append(out, k+"="+v)
	}
	return strings.Join(out, "\n") + "\n", nil
}
