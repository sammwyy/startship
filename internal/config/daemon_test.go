package config

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func cleanEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range environment {
		t.Setenv(name, "")
	}
	t.Setenv("STARTSHIP_CONFIG", "")
}
func configFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "startship.toml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestDaemonPrecedence(t *testing.T) {
	cleanEnvironment(t)
	path := configFile(t, "dashboard_listen = ['127.0.0.1:9000']\ndata_dir = '/file-data'\nacme_email = 'file@example.com'\n")
	t.Setenv("STARTSHIP_CONFIG", path)
	d, err := Load(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if d.DataDir != "/file-data" || d.DashboardListen[0] != "127.0.0.1:9000" || d.HTTPListen[0] != ":80" || d.HTTPSListen[0] != ":443" {
		t.Fatal(d)
	}
	t.Setenv("STARTSHIP_DATA", "/env-data")
	t.Setenv("STARTSHIP_DASHBOARD_ADDR", "127.0.0.1:9001,[::1]:9001")
	d, err = Load(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if d.DataDir != "/env-data" || len(d.DashboardListen) != 2 || d.DashboardListen[0] != "127.0.0.1:9001" {
		t.Fatal(d)
	}
	d, err = Load([]string{"--data-dir", "/flag-data", "--dashboard-listen", "127.0.0.1:9002,[::1]:9002", "--dashboard-listen", "lo:9003", "--acme-email="}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if d.DataDir != "/flag-data" || len(d.DashboardListen) != 3 || d.DashboardListen[2] != "lo:9003" || d.ACMEEmail != "" {
		t.Fatal(d)
	}
	other := configFile(t, "http_listen = [':9080']\n")
	d, err = Load([]string{"--config", other}, io.Discard)
	if err != nil || d.HTTPListen[0] != ":9080" {
		t.Fatal(d, err)
	}
	if Path("") != path || Path(other) != other {
		t.Fatal("config path precedence")
	}
	t.Setenv("STARTSHIP_CONFIG", "")
	if Path("") != DefaultPath {
		t.Fatal(Path(""))
	}
}
func TestDaemonErrorsAndHelp(t *testing.T) {
	cleanEnvironment(t)
	for _, text := range []string{"http_listen = []", "unknown = true", "http_listen = 'bad'", "data_dir = ''", "http_listen = [':99999']", "http_listen = ['missing-port']", "acme_directory = 'http://insecure'", "acme_email = 'bad'", "dashboard_domain = 'https://example.com'", "[broken"} {
		path := configFile(t, text)
		if _, err := Load([]string{"--config", path}, io.Discard); err == nil {
			t.Fatal("accepted", text)
		}
	}
	if _, err := Load([]string{"--config", filepath.Join(t.TempDir(), "missing")}, io.Discard); err == nil {
		t.Fatal("missing explicit config accepted")
	}
	t.Setenv("STARTSHIP_CONFIG", filepath.Join(t.TempDir(), "missing"))
	if _, err := Load(nil, io.Discard); err == nil {
		t.Fatal("missing env config accepted")
	}
	var out bytes.Buffer
	if _, err := Load([]string{"--help"}, &out); !errors.Is(err, flag.ErrHelp) || !strings.Contains(out.String(), "dashboard-listen") || !strings.Contains(out.String(), "https-listen") {
		t.Fatal(err, out.String())
	}
}
func TestInterfaceResolution(t *testing.T) {
	got, err := Resolve([]string{":80", "127.0.0.1:8080", "[::1]:443", ":80"})
	if err != nil || !reflect.DeepEqual(got, []string{":80", "127.0.0.1:8080", "[::1]:443"}) {
		t.Fatal(got, err)
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	loopback := ""
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			loopback = iface.Name
			break
		}
	}
	if loopback == "" {
		t.Fatal("no loopback interface")
	}
	got, err = Resolve([]string{net.JoinHostPort(loopback, "8080")})
	if err != nil || len(got) == 0 {
		t.Fatal(got, err)
	}
	for _, addr := range got {
		host, port, _ := net.SplitHostPort(addr)
		if !net.ParseIP(host).IsLoopback() || port != "8080" {
			t.Fatal(addr)
		}
	}
	if _, err := Resolve([]string{"startship-missing-interface.invalid:8080"}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatal(err)
	}
	if _, err := Resolve([]string{"bad"}); err == nil {
		t.Fatal("invalid listen accepted")
	}
}
func TestEditPreservesOtherContent(t *testing.T) {
	cleanEnvironment(t)
	original := "# Header\ndata_dir = '''/tmp/\nhttp_listen = fake\n'''\n# Keep this comment\nhttp_listen = [\n ':80', # old\n]\n\"acme_email\" = 'admin@example.com'\n"
	path := configFile(t, original)
	if err := Edit(path, "http_listen", []string{":8081", ":8082"}, false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "data_dir = '''/tmp/\nhttp_listen = fake\n'''\n# Keep this comment\n") || !strings.Contains(string(b), `"acme_email" = 'admin@example.com'`) {
		t.Fatal(string(b))
	}
	value, err := Get(b, "http_listen")
	if err != nil || value != `[":8081",":8082"]` {
		t.Fatal(value, err)
	}
	before := string(b)
	if err := Edit(path, "http_listen", []string{"bad"}, false); err == nil {
		t.Fatal("invalid edit accepted")
	}
	b, _ = os.ReadFile(path)
	if string(b) != before {
		t.Fatal("failed edit changed file")
	}
	if err := Edit(path, "acme_email", nil, true); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if _, err := Get(b, "acme_email"); err == nil {
		t.Fatal("unset failed")
	}
	if err := Edit(path, "socket_path", []string{"/run/startship/custom.sock"}, false); err != nil {
		t.Fatal(err)
	}
	stat, _ := os.Stat(path)
	if stat.Mode().Perm() != 0600 {
		t.Fatal(stat.Mode())
	}
	if err := Edit(path, "unknown", []string{"x"}, false); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestFirewallIdentitySettings(t *testing.T) {
	cleanEnvironment(t)
	path := configFile(t, "trusted_proxies = ['10.0.0.0/8', '2001:db8::/32']\ngeoip_database = '/file/Country.mmdb'\n")
	d, err := Load([]string{"--config", path}, io.Discard)
	if err != nil || len(d.TrustedProxies) != 2 || d.GeoIPDatabase != "/file/Country.mmdb" {
		t.Fatal(d, err)
	}
	t.Setenv("STARTSHIP_TRUSTED_PROXIES", " 192.0.2.0/24 ,198.51.100.0/24")
	t.Setenv("STARTSHIP_GEOIP_DB", "/env/Country.mmdb")
	d, err = Load([]string{"--config", path}, io.Discard)
	if err != nil || d.TrustedProxies[0] != "192.0.2.0/24" || d.GeoIPDatabase != "/env/Country.mmdb" {
		t.Fatal(d, err)
	}
	d, err = Load([]string{"--config", path, "--trusted-proxies", "127.0.0.0/8", "--trusted-proxies", "::1/128", "--geoip-database="}, io.Discard)
	if err != nil || !reflect.DeepEqual(d.TrustedProxies, []string{"127.0.0.0/8", "::1/128"}) || d.GeoIPDatabase != "" {
		t.Fatal(d, err)
	}
	if err = d.Export(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("STARTSHIP_TRUSTED_PROXIES") != "127.0.0.0/8,::1/128" || os.Getenv("STARTSHIP_GEOIP_DB") != "" {
		t.Fatal("export lost identity settings")
	}
	for _, bad := range []string{"not-a-cidr", "192.0.2.1", "10.0.0.0/99"} {
		d := Defaults()
		d.TrustedProxies = []string{bad}
		if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "trusted_proxies[0]") {
			t.Fatalf("accepted %s: %v", bad, err)
		}
	}
	d = Defaults()
	if err = d.Export(); err != nil || os.Getenv("STARTSHIP_TRUSTED_PROXIES") != "" {
		t.Fatal("empty proxies export", err)
	}
	if err = Edit(path, "trusted_proxies", []string{"127.0.0.0/8,::1/128"}, false); err != nil {
		t.Fatal(err)
	}
	if err = Edit(path, "geoip_database", []string{"/new/Country.mmdb"}, false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	d = Defaults()
	if err = Parse(b, &d); err != nil || len(d.TrustedProxies) != 2 || d.GeoIPDatabase != "/new/Country.mmdb" {
		t.Fatal(d, err)
	}
}
