package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

const DefaultPath = "/etc/startship/startship.toml"

type Daemon struct {
	DashboardListen []string `toml:"dashboard_listen"`
	HTTPListen      []string `toml:"http_listen"`
	HTTPSListen     []string `toml:"https_listen"`
	DataDir         string   `toml:"data_dir"`
	DashboardDomain string   `toml:"dashboard_domain"`
	ACMEEmail       string   `toml:"acme_email"`
	ACMEDirectory   string   `toml:"acme_directory"`
	SocketPath      string   `toml:"socket_path"`
	TrustedProxies  []string `toml:"trusted_proxies"`
	GeoIPDatabase   string   `toml:"geoip_database"`
	NetworkFirewall bool     `toml:"network_firewall"`
}

var environment = map[string]string{
	"dashboard_listen": "STARTSHIP_DASHBOARD_ADDR", "http_listen": "STARTSHIP_HTTP_ADDR", "https_listen": "STARTSHIP_HTTPS_ADDR",
	"data_dir": "STARTSHIP_DATA", "dashboard_domain": "STARTSHIP_DASHBOARD_DOMAIN", "acme_email": "STARTSHIP_ACME_EMAIL",
	"acme_directory": "STARTSHIP_ACME_DIRECTORY", "socket_path": "STARTSHIP_SOCKET",
	"trusted_proxies": "STARTSHIP_TRUSTED_PROXIES", "geoip_database": "STARTSHIP_GEOIP_DB",
	"network_firewall": "STARTSHIP_NETWORK_FIREWALL",
}

func (d *Daemon) bools() map[string]*bool {
	return map[string]*bool{"network_firewall": &d.NetworkFirewall}
}

func (d *Daemon) strings() map[string]*string {
	return map[string]*string{"data_dir": &d.DataDir, "dashboard_domain": &d.DashboardDomain, "acme_email": &d.ACMEEmail, "acme_directory": &d.ACMEDirectory, "socket_path": &d.SocketPath, "geoip_database": &d.GeoIPDatabase}
}
func (d *Daemon) lists() map[string]*[]string {
	return map[string]*[]string{"dashboard_listen": &d.DashboardListen, "http_listen": &d.HTTPListen, "https_listen": &d.HTTPSListen, "trusted_proxies": &d.TrustedProxies}
}
func Defaults() Daemon {
	return Daemon{DashboardListen: []string{":8080"}, HTTPListen: []string{":80"}, HTTPSListen: []string{":443"}, DataDir: DataDirectory(), ACMEDirectory: "https://acme-v02.api.letsencrypt.org/directory"}
}
func Path(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if p := os.Getenv("STARTSHIP_CONFIG"); p != "" {
		return p
	}
	return DefaultPath
}
func Read(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}
func Parse(b []byte, d *Daemon) error {
	meta, err := toml.Decode(string(b), d)
	if err != nil {
		return err
	}
	if unknown := meta.Undecoded(); len(unknown) > 0 {
		return fmt.Errorf("unknown daemon setting: %s", unknown[0])
	}
	return nil
}

type listenFlag []string

func (v *listenFlag) String() string     { return strings.Join(*v, ",") }
func (v *listenFlag) Set(s string) error { *v = append(*v, strings.Split(s, ",")...); return nil }

func Load(args []string, out io.Writer) (Daemon, error) {
	var values Daemon
	var file string
	f := flag.NewFlagSet("startshipd", flag.ContinueOnError)
	f.SetOutput(out)
	f.StringVar(&file, "config", "", "daemon TOML file (STARTSHIP_CONFIG, else /etc/startship/startship.toml if present)")
	for key, p := range values.bools() {
		f.BoolVar(p, strings.ReplaceAll(key, "_", "-"), false, "override "+key+" ("+environment[key]+")")
	}
	for key, p := range values.strings() {
		f.StringVar(p, strings.ReplaceAll(key, "_", "-"), "", "override "+key+" ("+environment[key]+")")
	}
	for key, p := range values.lists() {
		f.Var((*listenFlag)(p), strings.ReplaceAll(key, "_", "-"), "listen host:port or interface:port; comma-separated/repeatable ("+environment[key]+")")
	}
	f.Usage = func() {
		fmt.Fprintln(out, "Usage: startshipd [flags]\nPrecedence: flags > environment > TOML file > defaults.\nDefaults: dashboard :8080; HTTP :80; HTTPS :443; data ./_data (legacy ./data if present).\n--version prints the build version.")
		f.PrintDefaults()
	}
	if err := f.Parse(args); err != nil {
		return Daemon{}, err
	}
	if f.NArg() != 0 {
		return Daemon{}, fmt.Errorf("unexpected arguments: %s", strings.Join(f.Args(), " "))
	}
	d := Defaults()
	path := Path(file)
	b, err := os.ReadFile(path)
	if err != nil && !(errors.Is(err, os.ErrNotExist) && file == "" && os.Getenv("STARTSHIP_CONFIG") == "") {
		return d, fmt.Errorf("config %s: %w", path, err)
	}
	if err = Parse(b, &d); err != nil {
		return d, fmt.Errorf("config %s: %w", path, err)
	}
	for key, p := range d.strings() {
		if s := os.Getenv(environment[key]); s != "" {
			*p = s
		}
	}
	for key, p := range d.lists() {
		if s := os.Getenv(environment[key]); s != "" {
			*p = strings.Split(s, ",")
		}
	}
	for key, p := range d.bools() {
		if s := os.Getenv(environment[key]); s != "" {
			value, err := strconv.ParseBool(s)
			if err != nil {
				return d, fmt.Errorf("%s: %w", environment[key], err)
			}
			*p = value
		}
	}
	f.Visit(func(v *flag.Flag) {
		key := strings.ReplaceAll(v.Name, "-", "_")
		if p, ok := d.bools()[key]; ok {
			*p = *values.bools()[key]
		}
		if p, ok := d.strings()[key]; ok {
			*p = *values.strings()[key]
		}
		if p, ok := d.lists()[key]; ok {
			*p = *values.lists()[key]
		}
	})
	return d, d.Validate()
}

func (d *Daemon) Validate() error {
	for key, p := range d.lists() {
		if key == "trusted_proxies" {
			for i, s := range *p {
				s = strings.TrimSpace(s)
				(*p)[i] = s
				if _, err := netip.ParsePrefix(s); err != nil {
					return fmt.Errorf("trusted_proxies[%d]: invalid CIDR %q", i, s)
				}
			}
			continue
		}
		if len(*p) == 0 {
			return fmt.Errorf("%s needs at least one listen address", key)
		}
		for i, s := range *p {
			s = strings.TrimSpace(s)
			(*p)[i] = s
			if err := ValidateListen(s); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	if strings.TrimSpace(d.DataDir) == "" {
		return fmt.Errorf("data_dir cannot be empty")
	}
	if d.ACMEEmail != "" {
		if _, err := mail.ParseAddress(d.ACMEEmail); err != nil {
			return fmt.Errorf("invalid acme_email: %w", err)
		}
	}
	if d.ACMEDirectory != "" {
		u, err := url.Parse(d.ACMEDirectory)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("acme_directory must be an HTTPS URL")
		}
	}
	if strings.ContainsAny(d.DashboardDomain, "/ \t\n") {
		return fmt.Errorf("dashboard_domain must be a hostname")
	}
	return nil
}
func ValidateListen(s string) error {
	_, port, err := net.SplitHostPort(s)
	if err != nil {
		return fmt.Errorf("invalid listen entry %q: use host:port, :port or interface:port: %w", s, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("invalid listen port in %q", s)
	}
	return nil
}

func Resolve(entries []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, entry := range entries {
		if err := ValidateListen(entry); err != nil {
			return nil, err
		}
		host, port, _ := net.SplitHostPort(entry)
		ipHost := strings.Split(host, "%")[0]
		if host == "" || net.ParseIP(ipHost) != nil {
			add(entry)
			continue
		}
		iface, err := net.InterfaceByName(host)
		if err == nil {
			addresses, err := iface.Addrs()
			if err != nil {
				return nil, fmt.Errorf("interface %s: %w", host, err)
			}
			if len(addresses) == 0 {
				return nil, fmt.Errorf("interface %s has no addresses", host)
			}
			for _, address := range addresses {
				ip, _, err := net.ParseCIDR(address.String())
				if err != nil {
					return nil, err
				}
				h := ip.String()
				if ip.IsLinkLocalUnicast() && ip.To4() == nil {
					h += "%" + iface.Name
				}
				add(net.JoinHostPort(h, port))
			}
			continue
		}
		addresses, lookupErr := net.LookupHost(host)
		if lookupErr != nil {
			return nil, fmt.Errorf("listen %q: interface %q does not exist and hostname cannot be resolved: %w", entry, host, lookupErr)
		}
		for _, address := range addresses {
			add(net.JoinHostPort(address, port))
		}
	}
	return out, nil
}

// Export keeps URL generation, socket defaults and legacy integrations in sync.
func (d *Daemon) Export() error {
	for key, p := range d.bools() {
		if err := os.Setenv(environment[key], strconv.FormatBool(*p)); err != nil {
			return err
		}
	}
	for key, p := range d.strings() {
		if err := os.Setenv(environment[key], *p); err != nil {
			return err
		}
	}
	for key, p := range d.lists() {
		value := ""
		if key == "trusted_proxies" {
			value = strings.Join(*p, ",")
		} else if len(*p) > 0 {
			value = (*p)[0]
		}
		if err := os.Setenv(environment[key], value); err != nil {
			return err
		}
	}
	return nil
}
