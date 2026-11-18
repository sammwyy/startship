package netfw

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeExecutor struct {
	chains  map[string]map[string][]string
	sets    map[string]bool
	scripts []string
	calls   []string
	fail    string
	missing bool
}

func newFake() *fakeExecutor {
	return &fakeExecutor{chains: map[string]map[string][]string{"iptables": {"INPUT": {}, "DOCKER-USER": {}}, "ip6tables": {"INPUT": {}}}, sets: map[string]bool{}}
}
func (f *fakeExecutor) LookPath(s string) (string, error) {
	if f.missing {
		return "", errors.New("missing binary")
	}
	return s, nil
}
func (f *fakeExecutor) Run(input, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if f.fail != "" && strings.Contains(call, f.fail) {
		return nil, errors.New("injected failure")
	}
	if name == "ipset" {
		switch args[0] {
		case "restore":
			f.scripts = append(f.scripts, input)
			for _, line := range strings.Split(strings.TrimSpace(input), "\n") {
				p := strings.Fields(line)
				if p[0] == "create" {
					f.sets[p[1]] = true
				}
				if p[0] == "destroy" {
					delete(f.sets, p[1])
				}
			}
		case "list":
			if args[1] != "-n" && !f.sets[args[1]] {
				return nil, errors.New("absent")
			}
		case "destroy":
			delete(f.sets, args[1])
		}
		return nil, nil
	}
	args = args[2:]
	op, chain := args[0], args[1]
	chains := f.chains[name]
	rules, exists := chains[chain]
	if op == "-N" {
		if exists {
			return nil, errors.New("exists")
		}
		chains[chain] = []string{}
		return nil, nil
	}
	if !exists {
		return nil, errors.New("absent")
	}
	rule := strings.Join(args[2:], " ")
	switch op {
	case "-S":
		lines := []string{"-N " + chain}
		for _, r := range rules {
			lines = append(lines, "-A "+chain+" "+r)
		}
		return []byte(strings.Join(lines, "\n")), nil
	case "-C":
		if !slices.Contains(rules, rule) {
			return nil, errors.New("absent")
		}
	case "-A":
		chains[chain] = append(rules, rule)
	case "-I":
		chains[chain] = append([]string{strings.Join(args[3:], " ")}, rules...)
	case "-D":
		index := slices.Index(rules, rule)
		if index < 0 {
			return nil, errors.New("absent")
		}
		chains[chain] = slices.Delete(rules, index, index+1)
	case "-F":
		chains[chain] = []string{}
	case "-X":
		delete(chains, chain)
	default:
		return nil, fmt.Errorf("unexpected command %s", call)
	}
	return nil, nil
}

func TestReconcileAndTeardown(t *testing.T) {
	f := newFake()
	m := Manager{Executor: f, Ports: []string{"443", "80", "80"}, Country: func(s string) ([]string, error) {
		if s != "US" {
			t.Fatal(s)
		}
		return []string{"203.0.113.0/24", "2001:db8::/32"}, nil
	}}
	now := time.Unix(1000, 0)
	expiry := now.Add(30 * time.Second)
	past := now.Add(-time.Second)
	rules := []Rule{{Kind: "ip", Value: "192.0.2.1", ExpiresAt: &expiry}, {Kind: "cidr", Value: "192.0.2.0/24", Ports: "router"}, {Kind: "country", Value: "us"}, {Kind: "ip", Value: "198.51.100.1", ExpiresAt: &past}, {Kind: "ip", Value: "192.0.2.1", ExpiresAt: &expiry}}
	if err := m.Reconcile(rules, now); err != nil {
		t.Fatal(err)
	}
	script := f.scripts[0]
	for _, want := range []string{"create startship-all-v4 hash:net family inet timeout 0 maxelem 1048576 -exist", "create startship-router-v6-tmp hash:net family inet6 timeout 0", "add startship-all-v4-tmp 192.0.2.1/32 timeout 30", "add startship-router-v4-tmp 192.0.2.0/24 timeout 0", "add startship-all-v6-tmp 2001:db8::/32 timeout 0", "swap startship-all-v4-tmp startship-all-v4\ndestroy startship-all-v4-tmp"} {
		if !strings.Contains(script, want) {
			t.Fatalf("missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "198.51.100.1") || strings.Count(script, "add startship-all-v4-tmp 192.0.2.1/") != 1 {
		t.Fatal(script)
	}
	if strings.LastIndex(script, "add ") > strings.Index(script, "swap ") {
		t.Fatal("publishing before all sets populated")
	}
	state := fmt.Sprint(f.chains)
	count := len(f.calls)
	if err := m.Reconcile(rules, now); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.chains) != state {
		t.Fatal("non-idempotent chains", f.chains)
	}
	for _, call := range f.calls[count:] {
		if strings.Contains(call, " -I ") || strings.Contains(call, " -A ") {
			t.Fatal("duplicate rule", call)
		}
	}
	if len(f.chains["iptables"]["INPUT"]) != 1 || len(f.chains["iptables"]["DOCKER-USER"]) != 1 || len(f.chains["ip6tables"]["INPUT"]) != 1 {
		t.Fatal(f.chains)
	}
	routerRule := f.chains["iptables"][chain][1]
	if !strings.Contains(routerRule, "-p tcp -m multiport --dports 443,80 -m set --match-set startship-router-v4 src -j DROP") {
		t.Fatal(routerRule)
	}
	m.Ports = []string{"8080"}
	if err := m.Reconcile(nil, now); err != nil {
		t.Fatal(err)
	}
	if len(f.chains["iptables"][chain]) != 2 || strings.Contains(fmt.Sprint(f.chains), "443,80") {
		t.Fatal(f.chains)
	}
	f.sets["startship-all-v4-tmp"] = true
	if err := m.Teardown(); err != nil {
		t.Fatal(err)
	}
	if len(f.sets) != 0 || len(f.chains["iptables"]["INPUT"]) != 0 {
		t.Fatal(f)
	}
	if _, ok := f.chains["ip6tables"][chain]; ok {
		t.Fatal("chain not removed")
	}
	if err := m.Teardown(); err != nil {
		t.Fatal(err)
	}
}

func TestErrorsAndValidation(t *testing.T) {
	for _, r := range []Rule{{Kind: "ip", Value: "bad"}, {Kind: "ip", Value: "fe80::1%eth0"}, {Kind: "cidr", Value: "1.2.3.4"}, {Kind: "country", Value: "USA"}, {Kind: "country", Value: "1A"}, {Kind: "x", Value: "US"}, {Kind: "ip", Value: "127.0.0.1", Ports: "x"}} {
		if Normalize(&r) == nil {
			t.Fatal(r)
		}
	}
	f := newFake()
	m := Manager{Executor: f, Ports: []string{"80"}}
	if err := m.Reconcile([]Rule{{Kind: "country", Value: "US"}}, time.Now()); err == nil {
		t.Fatal("country without geoip")
	}
	if len(f.scripts) != 0 {
		t.Fatal("mutated kernel for invalid input")
	}
	f.missing = true
	if m.Available() == nil {
		t.Fatal("missing binaries")
	}
	f.missing = false
	for _, fail := range []string{"ipset list -n", "ipset restore", "iptables -w 5 -N STARTSHIP-NET", "iptables -w 5 -A STARTSHIP-NET"} {
		f := newFake()
		f.fail = fail
		m.Executor = f
		if err := m.Reconcile(nil, time.Now()); err == nil {
			t.Fatal(fail)
		}
	}
	m.Executor = newFake()
	m.Ports = []string{"0"}
	if m.Reconcile(nil, time.Now()) == nil {
		t.Fatal("invalid ports")
	}
	if _, err := ExpandCountry("", "US"); err == nil {
		t.Fatal("missing db")
	}
	if _, err := ExpandCountry("/nonexistent.mmdb", "US"); err == nil {
		t.Fatal("bad db")
	}
}

func TestJumpAtTopAndPermanentOverlap(t *testing.T) {
	f := newFake()
	f.chains["iptables"]["INPUT"] = []string{"-j ACCEPT", "-j STARTSHIP-NET", "-j STARTSHIP-NET"}
	m := Manager{Executor: f, Ports: []string{"80"}}
	expires := time.Now().Add(time.Hour)
	if err := m.Reconcile([]Rule{{Kind: "ip", Value: "127.0.0.2", ExpiresAt: &expires}, {Kind: "ip", Value: "127.0.0.2"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.chains["iptables"]["INPUT"], []string{"-j STARTSHIP-NET", "-j ACCEPT"}) {
		t.Fatal(f.chains)
	}
	if !strings.Contains(f.scripts[0], "add startship-all-v4-tmp 127.0.0.2/32 timeout 0") {
		t.Fatal(f.scripts)
	}
	f.fail = "iptables -w 5 -D INPUT"
	if m.Teardown() == nil {
		t.Fatal("teardown error swallowed")
	}
}

func TestRootNetworksAndPortChunks(t *testing.T) {
	f := newFake()
	ports := []string{}
	for port := 8000; port < 8016; port++ {
		ports = append(ports, fmt.Sprint(port))
	}
	m := Manager{Executor: f, Ports: ports}
	now := time.Now()
	expires := now.Add(365 * 24 * time.Hour)
	if err := m.Reconcile([]Rule{{Kind: "cidr", Value: "0.0.0.0/0", ExpiresAt: &expires}, {Kind: "cidr", Value: "::/0"}}, now); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"0.0.0.0/1 timeout 2147483", "128.0.0.0/1 timeout 2147483", "::/1 timeout 0", "8000::/1 timeout 0"} {
		if !strings.Contains(f.scripts[0], want) {
			t.Fatal("missing", want)
		}
	}
	if len(f.chains["iptables"][chain]) != 3 || !strings.Contains(f.chains["iptables"][chain][2], "--dports 8015") {
		t.Fatal(f.chains)
	}
	m.Country = func(string) ([]string, error) { return nil, errors.New("bad country database") }
	if err := m.Reconcile([]Rule{{Kind: "country", Value: "US"}}, now); err == nil || !strings.Contains(err.Error(), "bad country database") {
		t.Fatal(err)
	}
}
