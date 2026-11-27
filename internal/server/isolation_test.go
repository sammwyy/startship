package server

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
					if key == "--rm" || key == "-d" || key == "--no-healthcheck" {
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
					"--security-opt": {"no-new-privileges"}, "--no-healthcheck": {"true"}, "-e": {"PORT=8080", "KEY=value"},
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
		if _, _, err := buildConfig("project"); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestPersistentBuildConfig(t *testing.T) {
	t.Setenv("STARTSHIP_BUILD_MEMORY", "")
	t.Setenv("STARTSHIP_BUILD_CPUS", "")
	first, opts, err := buildConfig("project")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "startship-build-project-") || !strings.Contains(strings.Join(opts, " "), "memory=1g --driver-opt cpu-period=100000 --driver-opt cpu-quota=100000") {
		t.Fatal(first, opts)
	}
	again, _, _ := buildConfig("project")
	if first != again {
		t.Fatal("builder not stable")
	}
	t.Setenv("STARTSHIP_BUILD_CPUS", "0.5")
	changed, _, _ := buildConfig("project")
	if changed == first {
		t.Fatal("CPU change did not replace builder")
	}
	t.Setenv("STARTSHIP_BUILD_MEMORY", "768m")
	memoryChanged, _, _ := buildConfig("project")
	if memoryChanged == changed {
		t.Fatal("memory change did not replace builder")
	}
	other, _, _ := buildConfig("other")
	if other == memoryChanged {
		t.Fatal("projects share builder")
	}
	if projectBuildLock("project") != projectBuildLock("project") || projectBuildLock("project") == projectBuildLock("other") {
		t.Fatal("incorrect build serialization")
	}
}

func TestBuildArgs(t *testing.T) {
	for _, noCache := range []bool{false, true} {
		args := buildArgs("builder", "image", imageBuild{Dockerfile: "Dockerfile", NoCache: noCache})
		if strings.Contains(strings.Join(args, " "), "--no-cache") != noCache {
			t.Fatal(args)
		}
		if args[len(args)-1] != "." || !strings.Contains(strings.Join(args, " "), "--builder builder --load") {
			t.Fatal(args)
		}
	}
}
