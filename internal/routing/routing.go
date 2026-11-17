package routing

import (
	"github.com/sammwyy/startship/internal/store"
	"net"
	"strings"
)

type Target struct {
	Route      store.Record
	Deployment store.Record
	Port       int
}

func NormalizeHost(h string) string {
	if host, _, e := net.SplitHostPort(h); e == nil {
		h = host
	}
	return strings.TrimSuffix(strings.ToLower(h), ".")
}
func NormalizePrefix(prefix string) string {
	prefix = strings.TrimSuffix(strings.TrimSpace(prefix), "*")
	return "/" + strings.TrimLeft(prefix, "/")
}
func matchesPrefix(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, strings.TrimSuffix(prefix, "/")+"/")
}
func MatchRoute(targets []Target, host, p string) *Target {
	host = NormalizeHost(host)
	var best *Target
	for i := range targets {
		t := &targets[i]
		if NormalizeHost(t.Route.Host) == host && matchesPrefix(p, t.Route.PathPrefix) && (best == nil || len(t.Route.PathPrefix) > len(best.Route.PathPrefix)) {
			best = t
		}
	}
	return best
}
func Stripped(p, prefix string) string {
	p = strings.TrimPrefix(p, prefix)
	return "/" + strings.TrimPrefix(p, "/")
}
