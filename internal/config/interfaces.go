package config

import (
	"net"
	"slices"
	"strings"
)

type NetworkInterface struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	Up        bool     `json:"up"`
	Loopback  bool     `json:"loopback"`
}

func NetworkInterfaces() ([]NetworkInterface, error) {
	raw, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := []NetworkInterface{}
	for _, iface := range raw {
		if strings.HasPrefix(iface.Name, "veth") {
			continue
		}
		rawAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		var v4, v6, linkLocal []string
		seen := map[string]bool{}
		for _, a := range rawAddrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil || ip.IsMulticast() || ip.IsUnspecified() {
				continue
			}
			s := ip.String()
			if seen[s] {
				continue
			}
			seen[s] = true
			switch {
			case ip.To4() != nil:
				v4 = append(v4, s)
			case ip.IsLinkLocalUnicast():
				linkLocal = append(linkLocal, s)
			default:
				v6 = append(v6, s)
			}
		}
		addrs := append(append(v4, v6...), linkLocal...)
		if len(addrs) == 0 {
			continue
		}
		out = append(out, NetworkInterface{
			Name:      iface.Name,
			Addresses: addrs,
			Up:        iface.Flags&net.FlagUp != 0,
			Loopback:  iface.Flags&net.FlagLoopback != 0,
		})
	}
	slices.SortFunc(out, func(a, b NetworkInterface) int {
		if a.Up != b.Up {
			if a.Up {
				return -1
			}
			return 1
		}
		if a.Loopback != b.Loopback {
			if !a.Loopback {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}
