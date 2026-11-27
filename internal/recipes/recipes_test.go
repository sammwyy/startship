package recipes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistryMerge(t *testing.T) {
	r := Builtins()
	for _, id := range []string{"gitea", "jellyfin", "minecraft", "teamspeak"} {
		if r[id].ID != id {
			t.Fatal("missing built-in", id)
		}
	}
	recipe := r["minecraft"]
	recipe.Version = "local"
	serve := func(entries ...Recipe) error {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"recipes": entries})
		}))
		defer server.Close()
		return r.Merge(context.Background(), server.URL)
	}
	invalid := recipe
	invalid.Files = append([]File(nil), recipe.Files...)
	invalid.Files[0].Path = "../server.properties"
	if err := serve(recipe, invalid); err == nil || r["minecraft"].Version == "local" {
		t.Fatalf("invalid index was not rejected atomically: %v", err)
	}
	if err := serve(recipe); err != nil || r["minecraft"].Version != "local" || len(r) != 4 {
		t.Fatalf("registry overlay failed: %v", err)
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
