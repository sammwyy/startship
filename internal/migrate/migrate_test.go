package migrate

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeFileInfo struct{ name string }

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return fs.ModeDir | 0755 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return true }
func (f fakeFileInfo) Sys() any           { return nil }

type mockHost struct {
	home     string
	files    map[string]string
	dirs     map[string]bool
	onDocker func(args []string) (string, error)
	calls    [][]string
}

func (m *mockHost) RunDocker(_ context.Context, args ...string) (string, error) {
	cp := append([]string(nil), args...)
	m.calls = append(m.calls, cp)
	if m.onDocker != nil {
		return m.onDocker(args)
	}
	return "", errors.New("unexpected docker call")
}

func (m *mockHost) ReadFile(path string) ([]byte, error) {
	if s, ok := m.files[path]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}

func (m *mockHost) Stat(path string) (fs.FileInfo, error) {
	if m.dirs[path] {
		return fakeFileInfo{name: path}, nil
	}
	if _, ok := m.files[path]; ok {
		return fakeFileInfo{name: path}, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockHost) HomeDir() string {
	if m.home != "" {
		return m.home
	}
	return "/home/test"
}

func encryptOpenshipForTest(t *testing.T, secret, plaintext string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, 16)
	_, _ = rand.Read(iv)
	sealed := gcm.Seal(nil, iv, []byte(plaintext), nil)
	ct := sealed[:len(sealed)-16]
	tag := sealed[len(sealed)-16:]
	packed := append(append(iv, tag...), ct...)
	return base64.StdEncoding.EncodeToString(packed)
}

func encryptDokployForTest(t *testing.T, secret, plaintext string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("dokploy:db-encryption:v1"))
	key := mac.Sum(nil)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, 12)
	_, _ = rand.Read(iv)
	sealed := gcm.Seal(nil, iv, []byte(plaintext), nil)
	ct := sealed[:len(sealed)-16]
	tag := sealed[len(sealed)-16:]
	packed := append(append(iv, tag...), ct...)
	return "enc:v1:" + base64.StdEncoding.EncodeToString(packed)
}

func encryptCoolifyForTest(t *testing.T, rawKey []byte, plaintext string) string {
	t.Helper()
	iv := make([]byte, aes.BlockSize)
	_, _ = rand.Read(iv)
	pad := aes.BlockSize - (len(plaintext) % aes.BlockSize)
	padded := append([]byte(plaintext), make([]byte, pad)...)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(pad)
	}
	block, err := aes.NewCipher(rawKey)
	if err != nil {
		t.Fatal(err)
	}
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)

	ivB64 := base64.StdEncoding.EncodeToString(iv)
	valB64 := base64.StdEncoding.EncodeToString(ct)
	mac := hmac.New(sha256.New, rawKey)
	mac.Write([]byte(ivB64 + valB64))
	payload, _ := json.Marshal(map[string]string{
		"iv":    ivB64,
		"value": valB64,
		"mac":   hex.EncodeToString(mac.Sum(nil)),
	})
	return base64.StdEncoding.EncodeToString(payload)
}

func TestCryptoDecryptions(t *testing.T) {
	// Openship
	osSealed := encryptOpenshipForTest(t, "openship-secret-123", "postgres://user:pass@db/app")
	got, err := DecryptOpenship(osSealed, []string{"wrong", "openship-secret-123"})
	if err != nil || got != "postgres://user:pass@db/app" {
		t.Fatalf("DecryptOpenship got (%q, %v)", got, err)
	}

	// Dokploy encrypted + plaintext passthrough
	dkSealed := encryptDokployForTest(t, "dokploy-secret-456", "API_KEY=hello\nPORT=4000")
	got, err = DecryptDokploy(dkSealed, []string{"dokploy-secret-456"}, nil)
	if err != nil || got != "API_KEY=hello\nPORT=4000" {
		t.Fatalf("DecryptDokploy got (%q, %v)", got, err)
	}
	got, err = DecryptDokploy("PLAIN=1", nil, nil)
	if err != nil || got != "PLAIN=1" {
		t.Fatalf("DecryptDokploy plaintext got (%q, %v)", got, err)
	}

	// Coolify (Laravel Crypt, including PHP serialized string)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	appKey := "base64:" + base64.StdEncoding.EncodeToString(key)
	clSealed := encryptCoolifyForTest(t, key, `s:11:"supersecret";`)
	got, err = DecryptCoolify(clSealed, []string{appKey})
	if err != nil || got != "supersecret" {
		t.Fatalf("DecryptCoolify got (%q, %v)", got, err)
	}
}

func TestParseDotEnv(t *testing.T) {
	env := ParseDotEnv(`
# comment
export FOO=bar
QUOTED="hello\nworld"
SINGLE='literal'
EMPTY=
`)
	if env["FOO"] != "bar" || env["QUOTED"] != "hello\nworld" || env["SINGLE"] != "literal" || env["EMPTY"] != "" {
		t.Fatalf("unexpected dotenv parse: %#v", env)
	}
}

func TestDetectOpenship(t *testing.T) {
	secret := "os-test-secret"
	encVal := encryptOpenshipForTest(t, secret, "s3cr3t-value")
	host := &mockHost{
		home: "/home/test",
		files: map[string]string{
			"/home/test/.openship/compose/.env": "POSTGRES_USER=openship\nPOSTGRES_DB=openship\nBETTER_AUTH_SECRET=" + secret + "\n",
		},
		onDocker: func(args []string) (string, error) {
			switch args[0] {
			case "ps":
				return `{"ID":"1","Names":"openship-postgres-1","Image":"postgres:16-alpine","State":"running","Labels":"com.docker.compose.project=openship,com.docker.compose.service=postgres"}` + "\n" +
					`{"ID":"2","Names":"openship-api-1","Image":"ghcr.io/oblien/openship-api:latest","State":"running","Labels":"com.docker.compose.project=openship,com.docker.compose.service=api"}`, nil
			case "inspect":
				if strings.Contains(args[2], ".Config.Env") {
					return `["POSTGRES_USER=openship","POSTGRES_DB=openship","BETTER_AUTH_SECRET=` + secret + `"]`, nil
				}
				return `[]`, nil
			case "exec":
				resp := map[string]any{
					"orgs": []map[string]any{{"id": "org_1", "name": "Acme"}},
					"projects": []map[string]any{{
						"id": "proj_1", "organization_id": "org_1", "name": "Web App", "slug": "web-app",
						"environment_name": "Production", "git_provider": "github", "git_owner": "acme", "git_repo": "web",
						"git_branch": "main", "port": 3000, "has_server": true, "volumes": []string{"uploads:/app/uploads"},
					}},
					"env_vars": []map[string]any{{
						"project_id": "proj_1", "service_id": "", "key": "API_SECRET", "value": encVal, "environment": "production",
					}},
					"services": []map[string]any{{
						"id": "svc_1", "project_id": "proj_1", "kind": "compose", "name": "main-db",
						"image": "postgres:16-alpine", "environment": map[string]string{"POSTGRES_USER": "app", "POSTGRES_PASSWORD": "pw", "POSTGRES_DB": "appdb"},
						"volumes": []string{"pgdata:/var/lib/postgresql/data"},
					}},
					"domains": []map[string]any{{
						"project_id": "proj_1", "hostname": "web.acme.test", "target_port": 3000, "target_path": "/",
					}},
					"deployments": []map[string]any{{
						"id": "dep_1", "project_id": "proj_1", "branch": "main", "commit_sha": "abc1234",
						"commit_message": "Initial deploy", "status": "ready", "created_at": "2026-11-20T12:00:00Z",
					}},
				}
				b, _ := json.Marshal(resp)
				return string(b), nil
			case "logs":
				return "server listening on :3000", nil
			}
			return "", nil
		},
	}

	got := DetectOpenship(context.Background(), host)
	if !got.Detected || got.Status != "detected" {
		t.Fatalf("expected detected openship, got %+v", got)
	}
	if len(got.Projects) != 1 || len(got.Services) != 1 {
		t.Fatalf("expected 1 project and 1 service, got %d projects / %d services", len(got.Projects), len(got.Services))
	}
	p := got.Projects[0]
	if p.GitURL != "https://github.com/acme/web.git" || p.Env["API_SECRET"] != "s3cr3t-value" {
		t.Fatalf("unexpected project mapping: %+v", p)
	}
	if len(p.Domains) != 1 || p.Domains[0].Host != "web.acme.test" {
		t.Fatalf("unexpected domains: %+v", p.Domains)
	}
	if len(p.Volumes) != 1 || p.Volumes[0].MountPath != "/app/uploads" {
		t.Fatalf("unexpected volumes: %+v", p.Volumes)
	}
	s := got.Services[0]
	if s.Engine != "postgresql" || s.Config["database"] != "appdb" || s.Version != "16" {
		t.Fatalf("unexpected service mapping: %+v", s)
	}
}

func TestDetectDokploy(t *testing.T) {
	secret := "dokploy-enc-key"
	encEnv := encryptDokployForTest(t, secret, "NODE_ENV=production\nSECRET=xyz")
	host := &mockHost{
		dirs: map[string]bool{"/etc/dokploy": true},
		files: map[string]string{
			"/etc/dokploy/logs/app1/dep1.log": "Build succeeded in 12s",
		},
		onDocker: func(args []string) (string, error) {
			switch args[0] {
			case "ps":
				return `{"ID":"1","Names":"dokploy-postgres.1.abc","Image":"postgres:16","State":"running","Labels":"com.docker.swarm.service.name=dokploy-postgres"}` + "\n" +
					`{"ID":"2","Names":"dokploy.1.def","Image":"dokploy/dokploy:latest","State":"running","Labels":"com.docker.swarm.service.name=dokploy"}`, nil
			case "inspect":
				if strings.Contains(args[2], ".Config.Env") {
					return `["POSTGRES_USER=dokploy","POSTGRES_DB=dokploy","ENCRYPTION_KEY=` + secret + `"]`, nil
				}
				return `[]`, nil
			case "exec":
				resp := map[string]any{
					"orgs":         []map[string]any{{"id": "o1", "name": "Core"}},
					"projects":     []map[string]any{{"projectId": "p1", "name": "Store", "organizationId": "o1", "env": ""}},
					"environments": []map[string]any{{"environmentId": "e1", "name": "production", "projectId": "p1", "env": ""}},
					"applications": []map[string]any{{
						"applicationId": "a1", "name": "store-api", "appName": "store-api-xyz",
						"env": encEnv, "sourceType": "github", "buildType": "dockerfile",
						"owner": "acme", "repository": "store", "branch": "main", "buildPath": "/",
						"dockerfile": "Dockerfile", "autoDeploy": true, "environmentId": "e1",
					}},
					"composes": []any{},
					"postgres": []map[string]any{{
						"id": "pg1", "name": "store-db", "appName": "store-db-123",
						"databaseName": "store", "databaseUser": "storeuser", "databasePassword": "pw",
						"dockerImage": "postgres:16", "externalPort": 5439, "environmentId": "e1",
					}},
					"mysql": []any{}, "mariadb": []any{}, "mongo": []any{}, "redis": []any{},
					"domains": []map[string]any{{
						"host": "api.store.test", "https": true, "port": 8080, "path": "/", "applicationId": "a1",
					}},
					"mounts": []map[string]any{{
						"type": "volume", "volumeName": "store_data", "mountPath": "/data", "applicationId": "a1",
					}},
					"deployments": []map[string]any{{
						"deploymentId": "d1", "title": "Deploy v1", "status": "done",
						"logPath": "/etc/dokploy/logs/app1/dep1.log", "applicationId": "a1",
						"createdAt": "2026-11-21T10:00:00Z", "finishedAt": "2026-11-21T10:01:00Z",
					}},
				}
				b, _ := json.Marshal(resp)
				return string(b), nil
			}
			return "", nil
		},
	}

	got := DetectDokploy(context.Background(), host)
	if !got.Detected || len(got.Projects) != 1 || len(got.Services) != 1 {
		t.Fatalf("unexpected dokploy detection: %+v", got)
	}
	p := got.Projects[0]
	if p.Env["SECRET"] != "xyz" || p.Port != 8080 || p.BuildType != "dockerfile" {
		t.Fatalf("unexpected dokploy project: %+v", p)
	}
	if len(p.Logs) != 1 || !strings.Contains(p.Logs[0].Content, "Build succeeded") {
		t.Fatalf("expected deployment log from file, got %+v", p.Logs)
	}
	if len(p.LinkedServices) != 1 || p.LinkedServices[0] != "pg1" {
		t.Fatalf("expected linked service pg1, got %+v", p.LinkedServices)
	}
}

func TestDetectCoolify(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	appKey := "base64:" + base64.StdEncoding.EncodeToString(key)
	encEnv := encryptCoolifyForTest(t, key, "coolify-token")
	encPW := encryptCoolifyForTest(t, key, "redis-pass")

	host := &mockHost{
		files: map[string]string{
			"/data/coolify/source/.env": "DB_USERNAME=coolify\nDB_DATABASE=coolify\nAPP_KEY=" + appKey + "\n",
		},
		onDocker: func(args []string) (string, error) {
			switch args[0] {
			case "ps":
				return `{"ID":"1","Names":"coolify-db","Image":"postgres:15-alpine","State":"running","Labels":""}`, nil
			case "inspect":
				return `[]`, nil
			case "exec":
				resp := map[string]any{
					"teams":        []map[string]any{{"id": 1, "name": "Root Team"}},
					"projects":     []map[string]any{{"id": 10, "uuid": "p-uuid", "name": "SaaS", "team_id": 1}},
					"environments": []map[string]any{{"id": 20, "name": "production", "project_id": 10}},
					"applications": []map[string]any{{
						"id": 100, "uuid": "app-uuid", "name": "frontend", "fqdn": "https://app.saas.test:3000",
						"git_repository": "acme/frontend", "git_branch": "main", "build_pack": "nixpacks",
						"ports_exposes": "3000", "base_directory": "/", "environment_id": 20,
					}},
					"env_vars": []map[string]any{{
						"resourceable_type": `App\Models\Application`, "resourceable_id": 100, "key": "TOKEN", "value": encEnv,
					}},
					"volumes": []map[string]any{{
						"name": "app-uuid-cache", "mount_path": "/cache", "resource_type": `App\Models\Application`, "resource_id": 100,
					}},
					"postgresqls": []any{}, "mysqls": []any{}, "mariadbs": []any{}, "mongodbs": []any{}, "clickhouses": []any{}, "services": []any{},
					"redis": []map[string]any{{
						"id": 5, "uuid": "redis-uuid", "name": "cache", "image": "redis:7.2-alpine",
						"environment_id": 20, "public_port": 0, "password": encPW,
					}},
					"deployments": []map[string]any{{
						"id": 1, "application_id": "100", "deployment_uuid": "dep-uuid", "commit": "deadbeef",
						"status": "finished", "logs": `[{"output":"Building image...","timestamp":"2026-11-22T01:00:00Z"}]`,
						"created_at": "2026-11-22T01:00:00Z", "updated_at": "2026-11-22T01:01:00Z",
					}},
				}
				b, _ := json.Marshal(resp)
				return string(b), nil
			}
			return "", nil
		},
	}

	got := DetectCoolify(context.Background(), host)
	if !got.Detected || len(got.Projects) != 1 || len(got.Services) != 1 {
		t.Fatalf("unexpected coolify detection: %+v", got)
	}
	p := got.Projects[0]
	if p.GitURL != "https://github.com/acme/frontend" || p.Env["TOKEN"] != "coolify-token" {
		t.Fatalf("unexpected coolify project: %+v", p)
	}
	if len(p.Domains) != 1 || p.Domains[0].Host != "app.saas.test" || !p.Domains[0].HTTPS {
		t.Fatalf("unexpected coolify domains: %+v", p.Domains)
	}
	if len(p.Logs) != 1 || !strings.Contains(p.Logs[0].Content, "Building image...") {
		t.Fatalf("unexpected coolify logs: %+v", p.Logs)
	}
	s := got.Services[0]
	if s.Engine != "redis" || s.Config["password"] != "redis-pass" {
		t.Fatalf("unexpected coolify service: %+v", s)
	}

	redacted := RedactPlatform(got)
	if redacted.Projects[0].Env != nil || redacted.Services[0].Config != nil {
		t.Fatalf("expected secrets stripped in redacted output")
	}
	if redacted.Projects[0].EnvCount != 1 || len(redacted.Projects[0].EnvKeys) != 1 {
		t.Fatalf("expected metadata kept in redacted output")
	}
}

func TestCopyVolumeData(t *testing.T) {
	host := &mockHost{
		onDocker: func(args []string) (string, error) {
			return "ok", nil
		},
	}
	if err := CopyVolumeData(context.Background(), host, "volume", "src_vol", "startship-vol-123"); err != nil {
		t.Fatalf("CopyVolumeData failed: %v", err)
	}
	if len(host.calls) != 2 || host.calls[0][0] != "volume" || host.calls[1][0] != "run" {
		t.Fatalf("unexpected docker calls: %#v", host.calls)
	}
}
