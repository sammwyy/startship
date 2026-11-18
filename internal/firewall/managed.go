package firewall

import (
	"encoding/json"
	"mime"
	"net/url"
	"regexp"
	"strings"
)

func hasAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
func ClassifyUA(ua string) (class, device string) {
	u := strings.ToLower(strings.TrimSpace(ua))
	if u == "" {
		return "empty", "unknown"
	}
	if hasAny(u, "googlebot", "bingbot", "gptbot", "claudebot", "ccbot", "bytespider", "ahrefsbot", "semrushbot", "duckduckbot", "applebot", "yandexbot", "baiduspider", "anthropic-ai", "chatgpt-user", "oai-searchbot") {
		return "crawler", "bot"
	}
	if hasAny(u, "sqlmap", "nikto", "masscan", "nmap", "scrapy") {
		return "bot", "bot"
	}
	if hasAny(u, "curl/", "wget/", "httpie/") {
		return "cli", "unknown"
	}
	if hasAny(u, "python-requests", "go-http-client", "python-urllib", "okhttp", "libwww-perl", "axios/", "node-fetch") {
		return "library", "unknown"
	}
	if strings.Contains(u, "mozilla/5.0") && hasAny(u, "chrome/", "firefox/", "safari/", "fxios/", "crios/", "edg/") {
		d := "desktop"
		if hasAny(u, "ipad", "tablet") || strings.Contains(u, "android") && !strings.Contains(u, "mobile") {
			d = "tablet"
		} else if hasAny(u, "mobile", "iphone", "ipod") {
			d = "mobile"
		}
		return "browser", d
	}
	return "unknown", "unknown"
}

var sqlPattern = regexp.MustCompile(`(?i)(\bunion\s+(?:all\s+)?select\b|['"]\s*(?:or|and)\s+(?:\d+\s*=\s*\d+|['"][^'"]*['"]\s*=\s*['"])|;\s*(?:drop|delete|truncate)\s+(?:table|from)\b|\b(?:sleep|benchmark)\s*\()`)
var xssPattern = regexp.MustCompile(`(?i)(<\s*script\b|javascript\s*:|\bon(?:error|load|click|mouseover|focus)\s*=|<\s*(?:iframe|svg)\b[^>]*\bon\w+\s*=)`)

func decoded(s string) string {
	for i := 0; i < 2; i++ {
		v, err := url.QueryUnescape(s)
		if err != nil || v == s {
			break
		}
		s = v
	}
	return s
}
func scanValues(r *Request) []string {
	out := []string{decoded(r.Path)}
	q, _ := url.ParseQuery(r.RawQuery)
	for _, vs := range q {
		for _, v := range vs {
			out = append(out, decoded(v))
		}
	}
	// ParseQuery can discard malformed pairs; retain the raw representation too.
	out = append(out, decoded(r.RawQuery))
	ct, _, _ := mime.ParseMediaType(r.Headers.Get("Content-Type"))
	if len(r.BodySample) > 0 && len(r.BodySample) <= 65536 && (r.ContentLength <= 65536) {
		if ct == "application/x-www-form-urlencoded" {
			v, _ := url.ParseQuery(string(r.BodySample))
			for _, vs := range v {
				out = append(out, vs...)
			}
		} else if ct == "application/json" || strings.HasSuffix(ct, "+json") {
			var v any
			if json.Unmarshal(r.BodySample, &v) == nil {
				var walk func(any)
				walk = func(v any) {
					switch x := v.(type) {
					case string:
						out = append(out, x)
					case []any:
						for _, e := range x {
							walk(e)
						}
					case map[string]any:
						for _, e := range x {
							walk(e)
						}
					}
				}
				walk(v)
			}
		}
	}
	return out
}
func managedMatch(id string, r *Request) bool {
	class, _ := ClassifyUA(r.UA)
	switch id {
	case "crawlers":
		return class == "crawler"
	case "bad_bots":
		return member(class, []string{"bot", "cli", "library"})
	case "unknown_user_agents":
		return class == "empty" || class == "unknown"
	case "scanner_paths":
		p := strings.ToLower(decoded(r.Path))
		return hasAny(p, "/.env", "/.git", "/wp-login.php", "/wp-admin", "/phpmyadmin", "/xmlrpc.php", "/.htpasswd", "/server-status")
	case "path_traversal":
		for _, v := range append(scanValues(r), decoded(r.Path)) {
			if hasAny(strings.ReplaceAll(strings.ToLower(v), `\`, "/"), "../", "\x00", "%00", "%2e%2e") {
				return true
			}
		}
	case "sqli", "xss":
		pattern := sqlPattern
		if id == "xss" {
			pattern = xssPattern
		}
		for _, v := range scanValues(r) {
			if pattern.MatchString(v) {
				return true
			}
		}
	}
	return false
}
