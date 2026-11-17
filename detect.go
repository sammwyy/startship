package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	for _, m := range [][2]string{{"Dockerfile", "dockerfile"}, {"package.json", "node"}, {"requirements.txt", "python"}, {"pyproject.toml", "python"}, {"go.mod", "go"}, {"Cargo.toml", "rust"}, {"index.html", "static"}} {
		if st, e := os.Stat(filepath.Join(dir, m[0])); e == nil && !st.IsDir() {
			return m[1]
		}
	}
	return ""
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
		var p struct{ Scripts map[string]string }
		b, _ := os.ReadFile(filepath.Join(dir, "package.json"))
		json.Unmarshal(b, &p)
		build := ""
		if p.Scripts["build"] != "" {
			build = "RUN npm run build\n"
		}
		return "FROM node:22-alpine\nWORKDIR /app\nCOPY . .\nRUN " + install + "\n" + build + base + "CMD [\"npm\",\"start\"]\n", nil
	case "python":
		install := "pip install --no-cache-dir ."
		if _, e := os.Stat(filepath.Join(dir, "requirements.txt")); e == nil {
			install = "pip install --no-cache-dir -r requirements.txt"
		}
		return "FROM python:3.13-slim\nWORKDIR /app\nCOPY . .\nRUN " + install + "\n" + base + "CMD [\"python\",\"app.py\"]\n", nil
	case "go":
		return "FROM golang:1.26 AS build\nWORKDIR /src\nCOPY . .\nRUN CGO_ENABLED=0 go build -o /app .\nFROM alpine:3.23\nRUN apk add --no-cache ca-certificates\nCOPY --from=build /app /app\n" + base + "CMD [\"/app\"]\n", nil
	case "rust":
		return "FROM rust:1-slim AS build\nWORKDIR /src\nCOPY . .\nRUN cargo build --release && mkdir /out && find target/release -maxdepth 1 -type f -executable -exec cp {} /out/app \\;\nFROM debian:trixie-slim\nCOPY --from=build /out/app /app\n" + base + "CMD [\"/app\"]\n", nil
	}
	return "", fmt.Errorf("unsupported build type %q", kind)
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
func (a *Server) detectAPI(w http.ResponseWriter, r *http.Request) {
	var v struct {
		GitURL   string `json:"git_url"`
		GitToken string `json:"git_token"`
		Branch   string `json:"branch"`
	}
	if decode(w, r, &v) != nil || validateGit(v.GitURL) != nil {
		fail(w, 400, "invalid git URL")
		return
	}
	if v.Branch == "" {
		v.Branch = "main"
	}
	dir, e := os.MkdirTemp(a.data, "detect-")
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	defer os.RemoveAll(dir)
	if e = a.clone(Record{GitURL: v.GitURL, GitToken: v.GitToken, Branch: v.Branch}, dir, nil); e != nil {
		fail(w, 400, "clone failed")
		return
	}
	reply(w, 200, candidates(dir))
}
