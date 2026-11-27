package netfw

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/netip"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

type Rule struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Value     string     `json:"value"`
	Ports     string     `json:"ports,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Note      string     `json:"note"`
}

func Normalize(r *Rule) error {
	if r.Ports == "" {
		r.Ports = "all"
	}
	if r.Ports != "all" && r.Ports != "router" {
		return fmt.Errorf("ports: expected all or router")
	}
	switch r.Kind {
	case "ip":
		ip, err := netip.ParseAddr(r.Value)
		if err != nil || ip.Zone() != "" {
			return fmt.Errorf("value: invalid IP address")
		}
		r.Value = ip.Unmap().String()
	case "cidr":
		p, err := netip.ParsePrefix(r.Value)
		if err != nil || p.Addr().Is4In6() {
			return fmt.Errorf("value: invalid CIDR")
		}
		r.Value = p.Masked().String()
	case "country":
		r.Value = strings.ToUpper(r.Value)
		if len(r.Value) != 2 || r.Value[0] < 'A' || r.Value[0] > 'Z' || r.Value[1] < 'A' || r.Value[1] > 'Z' {
			return fmt.Errorf("value: expected ISO-2 country code")
		}
	default:
		return fmt.Errorf("kind: expected ip, cidr or country")
	}
	if len(r.Note) > 4096 {
		return fmt.Errorf("note: maximum length is 4096")
	}
	return nil
}

type Executor interface {
	LookPath(string) (string, error)
	Run(input, name string, args ...string) ([]byte, error)
}

type OSExecutor struct{}

func (OSExecutor) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (OSExecutor) Run(input, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

type Manager struct {
	Executor Executor
	Ports    []string
	Country  func(string) ([]string, error)
}

func (m *Manager) Available() error {
	for _, name := range []string{"ipset", "iptables", "ip6tables"} {
		if _, err := m.Executor.LookPath(name); err != nil {
			return err
		}
	}
	_, err := m.Executor.Run("", "ipset", "list", "-n")
	return err
}

var sets = []string{"startship-all-v4", "startship-router-v4", "startship-all-v6", "startship-router-v6"}

const chain = "STARTSHIP-NET"

func (m *Manager) Reconcile(rules []Rule, now time.Time) error {
	if err := m.Available(); err != nil {
		return err
	}
	ports := []string{}
	seen := map[string]bool{}
	for _, port := range m.Ports {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("invalid router port %q", port)
		}
		port = strconv.Itoa(n)
		if !seen[port] {
			ports = append(ports, port)
			seen[port] = true
		}
	}
	sort.Strings(ports)
	entries := map[string]map[string]int{}
	for _, set := range sets {
		entries[set] = map[string]int{}
	}
	for _, r := range rules {
		if err := Normalize(&r); err != nil {
			return err
		}
		timeout := 0
		if r.ExpiresAt != nil {
			seconds := r.ExpiresAt.Sub(now).Seconds()
			if seconds <= 0 {
				continue
			}
			timeout = int(math.Min(math.Ceil(seconds), 2147483))
		}
		values := []string{r.Value}
		if r.Kind == "country" {
			if m.Country == nil {
				return fmt.Errorf("country rules require geoip_database")
			}
			var err error
			values, err = m.Country(r.Value)
			if err != nil {
				return err
			}
		}
		for _, value := range values {
			addr, err := netip.ParseAddr(value)
			var prefix netip.Prefix
			if err == nil {
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			} else {
				prefix, err = netip.ParsePrefix(value)
			}
			if err != nil {
				return fmt.Errorf("invalid network %q", value)
			}
			family := "v6"
			if prefix.Addr().Is4() {
				family = "v4"
			}
			set := "startship-" + r.Ports + "-" + family
			prefixes := []netip.Prefix{prefix.Masked()}
			if prefix.Bits() == 0 {
				second := netip.AddrFrom16([16]byte{0x80})
				if prefix.Addr().Is4() {
					second = netip.AddrFrom4([4]byte{0x80})
				}
				prefixes = []netip.Prefix{netip.PrefixFrom(prefix.Addr(), 1), netip.PrefixFrom(second, 1)}
			}
			for _, p := range prefixes {
				value = p.String()
				old, exists := entries[set][value]
				if !exists || timeout == 0 || old != 0 && timeout > old {
					entries[set][value] = timeout
				}
			}
		}
	}
	var script strings.Builder
	for _, set := range sets {
		family := "inet"
		if strings.HasSuffix(set, "v6") {
			family = "inet6"
		}
		temp := set + "-tmp"
		for _, name := range []string{set, temp} {
			fmt.Fprintf(&script, "create %s hash:net family %s timeout 0 maxelem 1048576 -exist\n", name, family)
		}
		fmt.Fprintf(&script, "flush %s\n", temp)
		keys := []string{}
		for value := range entries[set] {
			keys = append(keys, value)
		}
		sort.Strings(keys)
		for _, value := range keys {
			fmt.Fprintf(&script, "add %s %s timeout %d\n", temp, value, entries[set][value])
		}
	}
	// Populate every staging set before publishing any of them.
	for _, set := range sets {
		fmt.Fprintf(&script, "swap %s-tmp %s\ndestroy %s-tmp\n", set, set, set)
	}
	if _, err := m.Executor.Run(script.String(), "ipset", "restore"); err != nil {
		return err
	}
	for i, binary := range []string{"iptables", "ip6tables"} {
		family := "v4"
		if i == 1 {
			family = "v6"
		}
		run := func(args ...string) error {
			_, err := m.Executor.Run("", binary, append([]string{"-w", "5"}, args...)...)
			return err
		}
		if err := run("-S", chain); err != nil {
			if err = run("-N", chain); err != nil {
				return err
			}
		}
		desired := [][]string{{"-m", "set", "--match-set", "startship-all-" + family, "src", "-j", "DROP"}}
		for start := 0; start < len(ports); start += 15 {
			end := start + 15
			if end > len(ports) {
				end = len(ports)
			}
			desired = append(desired, []string{"-p", "tcp", "-m", "multiport", "--dports", strings.Join(ports[start:end], ","), "-m", "set", "--match-set", "startship-router-" + family, "src", "-j", "DROP"})
		}
		// Add desired rules before deleting stale ones so reconciles never flush protection.
		for _, rule := range desired {
			if err := run(append([]string{"-C", chain}, rule...)...); err != nil {
				if err = run(append([]string{"-A", chain}, rule...)...); err != nil {
					return err
				}
			}
		}
		out, err := m.Executor.Run("", binary, "-w", "5", "-S", chain)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 || fields[0] != "-A" {
				continue
			}
			keep := false
			for _, rule := range desired {
				if strings.Join(fields[2:], " ") == strings.Join(rule, " ") {
					keep = true
				}
			}
			if !keep {
				fields[0] = "-D"
				if err := run(fields...); err != nil {
					return err
				}
			}
		}
		for _, parent := range []string{"INPUT", "DOCKER-USER"} {
			if parent == "DOCKER-USER" {
				if err := run("-S", parent); err != nil {
					continue
				}
			}
			out, err := m.Executor.Run("", binary, "-w", "5", "-S", parent)
			if err != nil {
				return err
			}
			jumps := 0
			first := ""
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "-A ") {
					if first == "" {
						first = line
					}
					if line == "-A "+parent+" -j "+chain {
						jumps++
					}
				}
			}
			if jumps == 1 && first == "-A "+parent+" -j "+chain {
				continue
			}
			for j := 0; j < jumps; j++ {
				if err := run("-D", parent, "-j", chain); err != nil {
					return err
				}
			}
			if err := run("-I", parent, "1", "-j", chain); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) Teardown() error {
	if err := m.Available(); err != nil {
		return err
	}
	for _, binary := range []string{"iptables", "ip6tables"} {
		run := func(args ...string) error {
			_, err := m.Executor.Run("", binary, append([]string{"-w", "5"}, args...)...)
			return err
		}
		for _, parent := range []string{"INPUT", "DOCKER-USER"} {
			if err := run("-S", parent); err != nil {
				if parent == "INPUT" {
					return err
				}
				continue
			}
			for run("-C", parent, "-j", chain) == nil {
				if err := run("-D", parent, "-j", chain); err != nil {
					return err
				}
			}
		}
		if run("-S", chain) == nil {
			if err := run("-F", chain); err != nil {
				return err
			}
			if err := run("-X", chain); err != nil {
				return err
			}
		}
	}
	for _, set := range sets {
		for _, name := range []string{set + "-tmp", set} {
			if _, err := m.Executor.Run("", "ipset", "list", name); err == nil {
				if _, err = m.Executor.Run("", "ipset", "destroy", name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func ExpandCountry(path, code string) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("country rules require geoip_database")
	}
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	networks := db.Networks(maxminddb.SkipAliasedNetworks)
	values := []string{}
	for networks.Next() {
		var record struct {
			Country struct {
				ISO string `maxminddb:"iso_code"`
			} `maxminddb:"country"`
		}
		var network *net.IPNet
		network, err = networks.Network(&record)
		if err != nil {
			return nil, err
		}
		if record.Country.ISO == code {
			values = append(values, network.String())
		}
	}
	return values, networks.Err()
}
