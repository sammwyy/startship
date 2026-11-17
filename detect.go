package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Candidate struct {
	RootDir   string `json:"root_dir"`
	BuildType string `json:"build_type"`
	Port      int    `json:"port"`
}

func validBuildType(t string) bool {
	for _, v := range []string{"auto", "dockerfile", "static", "node", "python", "go", "rust"} {
		if t == v {
			return true
		}
	}
	return false
}
func detect(dir string) string {
	for _, m := range [][2]string{{"Dockerfile", "dockerfile"}, {"package.json", "node"}, {"requirements.txt", "python"}, {"pyproject.toml", "python"}, {"manage.py", "python"}, {"main.py", "python"}, {"app.py", "python"}, {"go.mod", "go"}, {"Cargo.toml", "rust"}, {"index.html", "static"}} {
		if st, e := os.Stat(filepath.Join(dir, m[0])); e == nil && !st.IsDir() {
			return m[1]
		}
	}
	if cmd := procfileWeb(dir); cmd != "" {
		if strings.Contains(cmd, "python") || strings.Contains(cmd, "gunicorn") || strings.Contains(cmd, "uvicorn") {
			return "python"
		}
		if strings.Contains(cmd, "node") || strings.Contains(cmd, "npm") {
			return "node"
		}
	}
	return ""
}
func procfileWeb(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "Procfile"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		name, command, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(name) == "web" {
			return strings.TrimSpace(command)
		}
	}
	return ""
}
func launchCMD(command string) string {
	b, _ := json.Marshal([]string{"sh", "-c", "exec " + command})
	return "CMD " + string(b) + "\n"
}
func candidates(dir string) []Candidate {
	out := []Candidate{}
	add := func(root string) {
		t := detect(filepath.Join(dir, root))
		if t != "" {
			port := 8080
			if t == "node" {
				port = 3000
			}
			out = append(out, Candidate{root, t, port})
		}
	}
	add(".")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			add(e.Name())
		}
	}
	return out
}
func dockerfile(kind string, port int, dir string) (string, error) {
	base := fmt.Sprintf("ENV PORT=%d\nEXPOSE %d\n", port, port)
	switch kind {
	case "node":
		install := "npm install"
		if _, e := os.Stat(filepath.Join(dir, "package-lock.json")); e == nil {
			install = "npm ci"
		}
		var p struct {
			Scripts map[string]string
			Engines struct{ Node string }
		}
		b, _ := os.ReadFile(filepath.Join(dir, "package.json"))
		json.Unmarshal(b, &p)
		build := ""
		if p.Scripts["build"] != "" {
			build = "RUN npm run build\n"
		}
		cmd := "CMD [\"npm\",\"start\"]\n"
		if web := procfileWeb(dir); web != "" {
			cmd = launchCMD(web)
		}
		return fmt.Sprintf("FROM node:%d-alpine\n", nodeMajor(p.Engines.Node)) + "WORKDIR /app\nCOPY . .\nRUN " + install + "\n" + build + base + cmd, nil
	case "python":
		install := ""
		if _, e := os.Stat(filepath.Join(dir, "pyproject.toml")); e == nil {
			install = "RUN pip install --no-cache-dir .\n"
		}
		if _, e := os.Stat(filepath.Join(dir, "requirements.txt")); e == nil {
			install = "RUN pip install --no-cache-dir -r requirements.txt\n"
		}
		cmd := procfileWeb(dir)
		if cmd == "" {
			for _, file := range []string{"main.py", "app.py", "manage.py"} {
				if _, err := os.Stat(filepath.Join(dir, file)); err == nil {
					cmd = "python " + file
					if file == "manage.py" {
						cmd += " runserver 0.0.0.0:$PORT"
					}
					break
				}
			}
		}
		if cmd == "" {
			return "", fmt.Errorf("no Python entrypoint found: add main.py, app.py, manage.py or a Procfile web command")
		}
		return "FROM python:3.13-slim\nWORKDIR /app\nCOPY . .\n" + install + base + launchCMD(cmd), nil
	case "go":
		return "FROM golang:1.26 AS build\nWORKDIR /src\nCOPY . .\nRUN CGO_ENABLED=0 go build -o /app .\nFROM alpine:3.23\nRUN apk add --no-cache ca-certificates\nCOPY --from=build /app /app\n" + base + "CMD [\"/app\"]\n", nil
	case "rust":
		return "FROM rust:1-slim AS build\nWORKDIR /src\nCOPY . .\nRUN cargo build --release && mkdir /out && find target/release -maxdepth 1 -type f -executable -exec cp {} /out/app \\;\nFROM debian:trixie-slim\nCOPY --from=build /out/app /app\n" + base + "CMD [\"/app\"]\n", nil
	}
	return "", fmt.Errorf("unsupported build type %q", kind)
}

var nodeConstraint = regexp.MustCompile(`^(>=|<=|>|<|\^|~|=)?(\d+)(?:\.(?:x|\*|\d+)(?:\.(?:x|\*|\d+))?)?$`)
var nodeOperatorSpace = regexp.MustCompile(`(>=|<=|>|<|\^|~|=)\s+`)

func nodeMajor(engine string) int {
	engine = nodeOperatorSpace.ReplaceAllString(engine, "$1")
	if strings.TrimSpace(engine) == "" {
		return 22
	}
	for _, major := range []int{26, 24, 22, 20, 18} {
		for _, alternative := range strings.Split(engine, "||") {
			parts := strings.Fields(strings.ReplaceAll(strings.TrimSpace(alternative), ",", " "))
			match := len(parts) > 0
			for _, part := range parts {
				if part == "*" || part == "x" {
					continue
				}
				m := nodeConstraint.FindStringSubmatch(part)
				if m == nil {
					match = false
					break
				}
				n, _ := strconv.Atoi(m[2])
				ok := major == n
				switch m[1] {
				case ">=":
					ok = major >= n
				case ">":
					ok = major > n
				case "<=":
					ok = major <= n
				case "<":
					ok = major < n
				}
				match = match && ok
			}
			if match {
				return major
			}
		}
	}
	return 22
}
func validateGit(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || strings.HasPrefix(raw, "-") || u.Scheme == "" {
		return fmt.Errorf("git URL must use https://, ssh:// or file://")
	}
	switch u.Scheme {
	case "https", "http", "ssh", "file":
		return nil
	}
	return fmt.Errorf("unsupported git URL scheme")
}
func (a *Server) detectAPI(w http.ResponseWriter, r *http.Request, x *access) {
	var v Record
	if decode(w, r, &v) != nil || v.ProjectID == "" || v.GitURL != "" || v.GitToken != "" || v.Branch != "" {
		fail(w, 400, "detect requires project_id; configure the source on the project")
		return
	}
	project := x.project(v.ProjectID)
	if !x.can(project, "project.settings") {
		fail(w, 400, "project source unavailable")
		return
	}
	source, e := a.projectSource(Record{ProjectID: v.ProjectID, Owner: project.Owner})
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	dir, e := os.MkdirTemp(a.data, "detect-")
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	defer os.RemoveAll(dir)
	if e = a.clone(source, dir, nil); e != nil {
		fail(w, 400, "clone failed")
		return
	}
	reply(w, 200, candidates(dir))
}
