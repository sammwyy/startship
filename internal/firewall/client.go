package firewall

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/oschwald/maxminddb-golang"
)

type GeoIP interface{ Country(ip net.IP) string }
type MMDB struct{ db *maxminddb.Reader }

func OpenMMDB(path string) (*MMDB, error) {
	if path == "" {
		return &MMDB{}, nil
	}
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	return &MMDB{db}, nil
}
func (m *MMDB) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}
func (m *MMDB) Country(ip net.IP) string {
	if m == nil || m.db == nil || ip == nil {
		return ""
	}
	var record struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if m.db.Lookup(ip, &record) != nil {
		return ""
	}
	return record.Country.ISOCode
}
func ParseTrustedProxies(cidrs []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, s := range cidrs {
		p, err := ipPrefix(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
func trusted(ip netip.Addr, proxies []netip.Prefix) bool {
	for _, p := range proxies {
		if p.Contains(ip.Unmap()) {
			return true
		}
	}
	return false
}

// ResolveClient trusts forwarding headers only from a configured immediate peer.
// A malformed XFF chain fails closed to the peer instead of falling back to CF.
func ResolveClient(remoteAddr string, headers http.Header, proxies []netip.Prefix, geo GeoIP) (ip, country string) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "", ""
	}
	peer = peer.Unmap()
	client := peer
	isTrusted := trusted(peer, proxies)
	if isTrusted {
		if xff := headers.Get("X-Forwarded-For"); xff != "" {
			hops := strings.Split(xff, ",")
			valid := true
			parsed := make([]netip.Addr, len(hops))
			for i, h := range hops {
				a, e := netip.ParseAddr(strings.TrimSpace(h))
				if e != nil {
					valid = false
					break
				}
				parsed[i] = a.Unmap()
			}
			if valid {
				for i := len(parsed) - 1; i >= 0; i-- {
					client = parsed[i]
					if !trusted(client, proxies) {
						break
					}
				}
			}
		} else if cf, e := netip.ParseAddr(strings.TrimSpace(headers.Get("CF-Connecting-IP"))); e == nil {
			client = cf.Unmap()
		}
	}
	if geo != nil {
		country = geo.Country(net.IP(client.AsSlice()))
	}
	if isTrusted {
		if cf := strings.ToUpper(strings.TrimSpace(headers.Get("CF-IPCountry"))); validCountry(cf) {
			country = cf
		}
	}
	return client.String(), country
}
func validCountry(s string) bool {
	return len(s) == 2 && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) && ((s[1] >= 'A' && s[1] <= 'Z') || (s[1] >= 'a' && s[1] <= 'z'))
}
