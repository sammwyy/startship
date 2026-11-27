package recipes

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/sammwyy/startship/internal/config"
)

func TestRegistryParse(t *testing.T) {
	r := Builtins()
	for _, id := range []string{"gitea", "jellyfin", "minecraft", "teamspeak"} {
		if r[id].ID != id {
			t.Fatal("missing built-in", id)
		}
	}
	recipe := r["minecraft"]
	recipe.Version = "remote"
	parse := func(entries ...Recipe) (Registry, error) {
		b, err := json.Marshal(map[string]any{"recipes": entries})
		if err != nil {
			t.Fatal(err)
		}
		return Parse(b)
	}
	invalid := recipe
	invalid.Files = append([]File(nil), recipe.Files...)
	invalid.Files[0].Path = "../server.properties"
	if result, err := parse(recipe, invalid); err == nil || result != nil {
		t.Fatalf("invalid index was not rejected atomically: %v", err)
	}
	if result, err := parse(recipe); err != nil || result["minecraft"].Version != "remote" || len(result) != 1 {
		t.Fatalf("registry replacement failed: %v", err)
	}
}

func TestBundledRecipePortsAvoidDefaultDashboardPort(t *testing.T) {
	for id, recipe := range Builtins() {
		for _, port := range recipe.Ports {
			if strconv.Itoa(port.HostPort) == config.DefaultDashboardPort {
				t.Errorf("%s/%s publishes the default dashboard port %d", id, port.ID, port.HostPort)
			}
		}
	}
}

func TestSchemaAndProperties(t *testing.T) {
	recipe := Builtins()["minecraft"]
	if _, err := recipe.Config.Validate(nil, true); err == nil {
		t.Fatal("install accepted without EULA")
	}
	values, err := recipe.Config.Validate(map[string]any{"eula": "TRUE"}, true)
	if err != nil || Expand("version=${version},eula=${eula}", values) != "version=LATEST,eula=TRUE" {
		t.Fatal(values, err)
	}
	file := recipe.Files[0]
	if _, err := file.Write("", map[string]any{"max-players": float64(101)}); err == nil {
		t.Fatal("invalid config passed")
	}
	contents, err := file.Write("# keep\nserver-port=25565\nmotd=previous\n", map[string]any{"motd": "Hello"})
	if err != nil || !strings.Contains(contents, "# keep\nserver-port=25565\n") || strings.Contains(contents, "previous") {
		t.Fatal(contents, err)
	}
	got, err := file.Read(contents)
	if err != nil || got["motd"] != "Hello" || got["max-players"] != float64(20) || got["online-mode"] != true {
		t.Fatal(got, err)
	}
}

func TestContainerEntrypoint(t *testing.T) {
	for _, tc := range []struct {
		name, entrypoint string
		valid            bool
	}{
		{"omitted", "", true},
		{"absolute", "/bin/sh", true},
		{"relative", "bin/sh", false},
		{"unclean", "/bin/../bin/sh", false},
		{"NUL", "/bin/sh\x00", false},
		{"CR", "/bin/sh\r", false},
		{"LF", "/bin/sh\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recipe := Builtins()["minecraft"]
			recipe.Containers[0].Entrypoint = tc.entrypoint
			b, err := json.Marshal(map[string][]Recipe{"recipes": {recipe}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := Parse(b)
			if !tc.valid {
				if err == nil || !strings.Contains(err.Error(), "invalid entrypoint for container") {
					t.Fatalf("expected entrypoint error, got %v", err)
				}
				return
			}
			if err != nil || got[recipe.ID].Containers[0].Entrypoint != tc.entrypoint {
				t.Fatalf("entrypoint not retained: %v", err)
			}
		})
	}
}
