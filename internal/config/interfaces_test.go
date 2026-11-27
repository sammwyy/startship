package config

import (
	"slices"
	"strings"
	"testing"
)

func TestNetworkInterfaces(t *testing.T) {
	ifaces, err := NetworkInterfaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(ifaces) == 0 {
		t.Fatal("expected at least one network interface")
	}
	foundLoopback := false
	for _, iface := range ifaces {
		if strings.HasPrefix(iface.Name, "veth") {
			t.Fatalf("unexpected veth interface %q", iface.Name)
		}
		if len(iface.Addresses) == 0 {
			t.Fatalf("interface %q has no addresses", iface.Name)
		}
		if iface.Loopback && slices.Contains(iface.Addresses, "127.0.0.1") {
			foundLoopback = true
		}
	}
	if !foundLoopback {
		t.Fatalf("expected loopback interface with 127.0.0.1 in %+v", ifaces)
	}
}
