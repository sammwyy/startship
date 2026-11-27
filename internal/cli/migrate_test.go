package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sammwyy/startship/internal/migrate"
)

func TestCLIMigrateCommands(t *testing.T) {
	var lastImport migrate.ImportRequest
	var lastImportQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/workspaces":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "ws_123", "name": "Production"},
			})
		case r.Method == "GET" && r.URL.Path == "/api/system/migrations/detect":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"platforms": []migrate.DetectedPlatform{{
					Platform: migrate.PlatformDokploy,
					Name:     "Dokploy",
					Detected: true,
					Status:   "detected",
					DataDir:  "/etc/dokploy",
					Projects: []migrate.DiscoveredProject{{
						ID:       "a1",
						Name:     "store-api",
						GitURL:   "https://github.com/acme/store.git",
						EnvCount: 3,
						LogCount: 2,
					}},
					Services: []migrate.DiscoveredService{{
						ID:         "pg1",
						Name:       "store-db",
						Engine:     "postgresql",
						Version:    "16",
						Compatible: true,
					}},
				}},
			})
		case r.Method == "POST" && r.URL.Path == "/api/system/migrations/import":
			lastImportQuery = r.URL.RawQuery
			_ = json.NewDecoder(r.Body).Decode(&lastImport)
			_ = json.NewEncoder(w).Encode(migrate.MigrationJob{
				ID:                  "job_1",
				Platform:            lastImport.Platform,
				Mode:                lastImport.Mode,
				Status:              "completed",
				TargetWorkspaceID:   "ws_123",
				TargetWorkspaceName: "Production",
				Projects: []migrate.MigratedProjectResult{{
					SourceID:    "a1",
					SourceName:  "store-api",
					ProjectID:   "p_new",
					ProjectName: "Renamed API",
					Status:      "ok",
					EnvCount:    3,
				}},
			})
		case r.Method == "GET" && r.URL.Path == "/api/system/migrations/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobs": []migrate.MigrationJob{{
					ID:                  "job_1",
					Platform:            migrate.PlatformDokploy,
					Mode:                "cherry_pick",
					Status:              "completed",
					TargetWorkspaceName: "Production",
				}},
			})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	runCmd := func(args ...string) (string, error) {
		var buf bytes.Buffer
		c := &cli{
			config: config{Server: srv.URL},
			out:    &buf,
			client: srv.Client(),
		}
		err := c.run(args)
		return buf.String(), err
	}

	// 1. detect
	out, err := runCmd("migrate", "detect")
	if err != nil {
		t.Fatalf("migrate detect failed: %v", err)
	}
	if !strings.Contains(out, "dokploy") || !strings.Contains(out, "store-api") || !strings.Contains(out, "store-db") {
		t.Fatalf("unexpected detect output:\n%s", out)
	}

	// 2. cherry-pick import with rename + workspace resolution by name
	out, err = runCmd("migrate", "import", "dokploy", "--workspace", "Production", "--project", "a1:Renamed API", "--service", "pg1", "--deploy")
	if err != nil {
		t.Fatalf("migrate import failed: %v", err)
	}
	if lastImportQuery != "sync=true" {
		t.Fatalf("expected sync=true, got %q", lastImportQuery)
	}
	if lastImport.Mode != "cherry_pick" || lastImport.TargetWorkspaceID != "ws_123" || len(lastImport.Projects) != 1 || lastImport.Projects[0].TargetName != "Renamed API" || lastImport.DeployProjects == nil || !*lastImport.DeployProjects {
		t.Fatalf("unexpected import request: %+v", lastImport)
	}
	if !strings.Contains(out, "Migration job_1") || !strings.Contains(out, "Renamed API") {
		t.Fatalf("unexpected import output:\n%s", out)
	}

	// 3. import all into new workspace
	_, err = runCmd("migrate", "import", "dokploy", "--all", "--create-workspace", "Imported All", "--no-volumes")
	if err != nil {
		t.Fatalf("migrate import --all failed: %v", err)
	}
	if lastImport.Mode != "all" || lastImport.CreateWorkspaceName != "Imported All" || lastImport.CopyVolumes == nil || *lastImport.CopyVolumes {
		t.Fatalf("unexpected import --all request: %+v", lastImport)
	}

	// 4. jobs list
	out, err = runCmd("migrate", "jobs")
	if err != nil {
		t.Fatalf("migrate jobs failed: %v", err)
	}
	if !strings.Contains(out, "job_1") || !strings.Contains(out, "Production") {
		t.Fatalf("unexpected jobs output:\n%s", out)
	}
}
