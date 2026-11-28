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

func TestNetworkFirewallBool(t *testing.T) {
	cleanEnvironment(t)
	t.Setenv("STARTSHIP_CONFIG", configFile(t, "network_firewall = true\n"))
	d, err := Load(nil, io.Discard)
	if err != nil || !d.NetworkFirewall {
		t.Fatal(d, err)
	}
	t.Setenv("STARTSHIP_NETWORK_FIREWALL", "0")
	d, err = Load(nil, io.Discard)
	if err != nil || d.NetworkFirewall {
		t.Fatal(d, err)
	}
	t.Setenv("STARTSHIP_NETWORK_FIREWALL", "1")
	d, err = Load([]string{"--network-firewall=false"}, io.Discard)
	if err != nil || d.NetworkFirewall {
		t.Fatal(d, err)
	}
	if err = d.Export(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("STARTSHIP_NETWORK_FIREWALL") != "false" {
		t.Fatal("export")
	}
	t.Setenv("STARTSHIP_NETWORK_FIREWALL", "invalid")
	if _, err = Load(nil, io.Discard); err == nil || !strings.Contains(err.Error(), "STARTSHIP_NETWORK_FIREWALL") {
		t.Fatal(err)
	}
	t.Setenv("STARTSHIP_NETWORK_FIREWALL", "")
	t.Setenv("STARTSHIP_CONFIG", configFile(t, "network_firewall = false\n"))
	d, err = Load([]string{"--network-firewall"}, io.Discard)
	if err != nil || !d.NetworkFirewall {
		t.Fatal(d, err)
	}
}
func configFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "startship.toml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRouterSettingsPrecedence(t *testing.T) {
	cleanEnvironment(t)
	path := configFile(t, "router_disabled = true\n")
	t.Setenv("STARTSHIP_CONFIG", path)
	d, err := Load(nil, io.Discard)
	if err != nil || !d.RouterDisabled || d.ConfigPath != path || d.RouterDisabledOverridden {
		t.Fatal(d, err)
	}
	t.Setenv("STARTSHIP_ROUTER_DISABLED", "true")
	t.Setenv("STARTSHIP_HTTP_ADDR", "127.0.0.1:18081")
	d, err = Load([]string{"--config", path, "--router-disabled=false", "--https-listen", "127.0.0.1:18443"}, io.Discard)
	if err != nil || d.RouterDisabled || !d.RouterDisabledOverridden || d.HTTPListen[0] != "127.0.0.1:18081" {
		t.Fatal(d, err)
	}
	if err := d.ExportRouter(); err != nil || os.Getenv("STARTSHIP_ROUTER_DISABLED") != "false" || os.Getenv("STARTSHIP_HTTPS_ADDR") != "127.0.0.1:18443" {
		t.Fatal("router export", err)
	}
	t.Setenv("STARTSHIP_ROUTER_DISABLED", "invalid")
	if _, err := Load(nil, io.Discard); err == nil || !strings.Contains(err.Error(), "STARTSHIP_ROUTER_DISABLED") {
		t.Fatal(err)
	}
}

func TestEditRouterSettings(t *testing.T) {
	path := configFile(t, "# keep this\nhttp_listen = [\n  '127.0.0.1:8080',\n]\n")
	if err := Edit(path, "router_disabled", []string{"true"}, false); err != nil {
		t.Fatal(err)
	}
	if err := EditSettings(path, map[string][]string{"http_listen": {}, "https_listen": {}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(before), "# keep this") {
		t.Fatal(string(before), err)
	}
	for _, key := range []string{"http_listen", "https_listen"} {
		if v, err := Get(before, key); err != nil || v != "[]" {
			t.Fatal(key, v, err)
		}
	}
	if err := Edit(path, "router_disabled", []string{"invalid"}, false); err == nil {
		t.Fatal("accepted invalid bool")
	}
	if err := EditSettings(path, map[string][]string{"router_disabled": {"false"}, "http_listen": {":80"}}); err == nil {
		t.Fatal("enabled with an empty HTTPS list")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatal("partial write", string(after), err)
	}
	if err := EditSettings(path, map[string][]string{"router_disabled": {"false"}, "http_listen": {":80"}, "https_listen": {":443"}}); err != nil {
		t.Fatal(err)
	}
}
func TestDefaultDashboardListener(t *testing.T) {
	cleanEnvironment(t)
	d, err := Load([]string{"--config", configFile(t, "")}, io.Discard)
	if err != nil || !reflect.DeepEqual(d.DashboardListen, []string{":9000"}) {
		t.Fatal(d.DashboardListen, err)
	}
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
	if err := EditSettings(path, map[string][]string{"dashboard_listen": nil}); err != nil {
		t.Fatal(err)
	}
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

func TestSavedListenersBeatStartupDefaults(t *testing.T) {
	cleanEnvironment(t)
	path := configFile(t, "")
	saved := map[string][]string{
		"dashboard_listen": {"127.0.0.1:19000", "[::1]:19000"},
		"http_listen":      {},
		"https_listen":     {"127.0.0.1:19443"},
	}
	if err := EditSettings(path, saved); err == nil {
		t.Fatal("accepted empty HTTP list with the router enabled")
	}
	saved["router_disabled"] = []string{"true"}
	if err := EditSettings(path, saved); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STARTSHIP_DASHBOARD_ADDR", ":9000")
	t.Setenv("STARTSHIP_HTTP_ADDR", ":80")
	t.Setenv("STARTSHIP_HTTPS_ADDR", ":443")
	d, err := Load([]string{"--config", path, "--dashboard-listen=:9001", "--http-listen=:8080", "--https-listen=:8443"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range d.lists() {
		if want, ok := saved[key]; ok && !reflect.DeepEqual(*value, want) {
			t.Errorf("%s = %v; want %v", key, *value, want)
		}
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
	if _, err := Load([]string{"--help"}, &out); !errors.Is(err, flag.ErrHelp) || !strings.Contains(out.String(), "dashboard-listen") || !strings.Contains(out.String(), "https-listen") || !strings.Contains(out.String(), "dashboard :9000") {
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
