package firewall

type FieldInfo struct {
	ID     string   `json:"id"`
	Ops    []string `json:"ops"`
	Values []string `json:"values,omitempty"`
}
type ManagedInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	DefaultAction  Action `json:"default_action"`
	DefaultEnabled bool   `json:"default_enabled"`
}
type CatalogInfo struct {
	Fields       []FieldInfo   `json:"fields"`
	Actions      []Action      `json:"actions"`
	ManagedRules []ManagedInfo `json:"managed_rules"`
	GeoIP        bool          `json:"geoip"`
}

func Catalog() CatalogInfo {
	textOps := []string{"eq", "in", "contains", "prefix", "suffix", "matches", "exists"}
	c := CatalogInfo{Actions: []Action{Block, Allow, Log, RateLimit, Ban}}
	c.Fields = append(c.Fields, FieldInfo{"ip", []string{"in_cidr", "in_list"}, nil}, FieldInfo{"country", []string{"eq", "in"}, nil})
	for _, id := range []string{"host", "path", "method", "query", "header.<name>", "user_agent", "ua_class", "device", "scheme"} {
		f := FieldInfo{ID: id, Ops: append([]string(nil), textOps...)}
		switch id {
		case "ua_class":
			f.Values = []string{"browser", "crawler", "bot", "cli", "library", "unknown", "empty"}
		case "device":
			f.Values = []string{"desktop", "mobile", "tablet", "bot", "unknown"}
		case "scheme":
			f.Values = []string{"http", "https"}
		}
		c.Fields = append(c.Fields, f)
	}
	for i, v := range [][3]string{{"sqli", "SQL injection", "Conservative SQL injection signatures"}, {"xss", "Cross-site scripting", "Script tags, handlers and script URLs"}, {"path_traversal", "Path traversal", "Parent directory traversal and null bytes"}, {"scanner_paths", "Scanner paths", "Sensitive files and common scanner targets"}, {"crawlers", "Crawlers", "Known search and AI crawlers"}, {"bad_bots", "Bad bots", "Command-line clients, libraries and attack tools"}, {"unknown_user_agents", "Unknown user agents", "Empty or unrecognized user agents"}} {
		c.ManagedRules = append(c.ManagedRules, ManagedInfo{v[0], v[1], v[2], Block, i < 4})
	}
	return c
}
