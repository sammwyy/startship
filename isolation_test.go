package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestAppRunArgs(t *testing.T) {
	for _, test := range []bool{false, true} {
		for _, override := range []bool{false, true} {
			t.Run(fmtTestName(test, override), func(t *testing.T) {
				memory, cpus, pids := "512m", "1", "512"
				for _, name := range []string{"STARTSHIP_APP_MEMORY", "STARTSHIP_APP_CPUS", "STARTSHIP_APP_PIDS"} {
					t.Setenv(name, "")
				}
				if override {
					memory, cpus, pids = "256m", "0.5", "64"
					t.Setenv("STARTSHIP_APP_MEMORY", memory)
					t.Setenv("STARTSHIP_APP_CPUS", cpus)
					t.Setenv("STARTSHIP_APP_PIDS", pids)
				}
				args := appRunArgs(Record{ID: "id", Name: "My API!", Port: 8080, Env: map[string]string{"KEY": "value"}}, "startship-project", test)
				flags := map[string][]string{}
				for i := 1; i < len(args); i++ {
					key := args[i]
					if key == "--rm" || key == "-d" {
						flags[key] = []string{"true"}
						continue
					}
					i++
					flags[key] = append(flags[key], args[i])
				}
				for key, expected := range map[string][]string{
					"--network": {"startship-project"}, "--network-alias": {"my-api"},
					"--memory": {memory}, "--cpus": {cpus}, "--pids-limit": {pids},
					"--cap-drop": {"ALL"}, "--cap-add": {"CHOWN", "SETUID", "SETGID", "NET_BIND_SERVICE"},
					"--security-opt": {"no-new-privileges"}, "-e": {"PORT=8080", "KEY=value"},
				} {
					if !reflect.DeepEqual(flags[key], expected) {
						t.Errorf("%s = %v, want %v", key, flags[key], expected)
					}
				}
				if (flags["--rm"] != nil) != test || (flags["-d"] != nil) == test {
					t.Fatalf("incorrect lifecycle flags: %v", args)
				}
				for _, forbidden := range []string{"--privileged", "--mount", "-v", "--read-only"} {
					if flags[forbidden] != nil {
						t.Fatal("unexpected flag", forbidden)
					}
				}
			})
		}
	}
}

func fmtTestName(test, override bool) string {
	if test {
		if override {
			return "test-overrides"
		}
		return "test-defaults"
	}
	if override {
		return "app-overrides"
	}
	return "app-defaults"
}

func TestAppAlias(t *testing.T) {
	for input, want := range map[string]string{"": "app", "!!!": "app", " My API! ": "my-api", "web": "web", strings.Repeat("x", 80): strings.Repeat("x", 63)} {
		if got := appAlias(input); got != want {
			t.Errorf("appAlias(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInvalidBuildCPU(t *testing.T) {
	for _, value := range []string{"invalid", "0", "-1", "1025", "NaN", "Inf", "0.001"} {
		t.Setenv("STARTSHIP_BUILD_CPUS", value)
		if err := buildImage("", nil, "unused", "unused"); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}
