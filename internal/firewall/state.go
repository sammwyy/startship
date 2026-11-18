package firewall

import (
	"sync"
	"time"
)

type rateKey struct{ rule, ip, path string }
type rateWindow struct {
	times   []time.Time
	expires time.Time
}
type banEntry struct {
	until      time.Time
	rule, name string
	seconds    int
}

// State belongs to one workspace and must be reused across engine recompiles.
type State struct {
	mu         sync.Mutex
	windows    map[rateKey]rateWindow
	bans       map[string]banEntry
	concurrent map[string]int
	now        func() time.Time
}

func NewState() *State { return &State{now: time.Now} }

// ClearBans is used after publishing an authoritative persisted banned list.
// Rate windows and in-flight concurrency are retained.
func (s *State) ClearBans() { s.mu.Lock(); defer s.mu.Unlock(); s.bans = nil }
func (s *State) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}
func (s *State) over(key rateKey, requests, seconds int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	cut := now.Add(-time.Duration(seconds) * time.Second)
	if s.windows == nil {
		s.windows = map[rateKey]rateWindow{}
	}
	for k, window := range s.windows {
		if !now.Before(window.expires) {
			delete(s.windows, k)
		}
	}
	ts := s.windows[key].times
	i := 0
	for i < len(ts) && !ts[i].After(cut) {
		i++
	}
	ts = append(ts[i:], now)
	s.windows[key] = rateWindow{ts, now.Add(time.Duration(seconds) * time.Second)}
	return len(ts) > requests
}
func (s *State) ban(ip, rule, name string, seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bans == nil {
		s.bans = map[string]banEntry{}
	}
	var until time.Time
	if seconds > 0 {
		until = s.clock().Add(time.Duration(seconds) * time.Second)
	}
	s.bans[ip] = banEntry{until, rule, name, seconds}
}
func (s *State) banned(ip string) (banEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.bans[ip]
	if ok && !b.until.IsZero() && !s.clock().Before(b.until) {
		delete(s.bans, ip)
		ok = false
	}
	return b, ok
}

// Acquire records an in-flight request only if the limit permits it. A zero limit is unlimited.
func (s *State) Acquire(ip string, limit int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.concurrent == nil {
		s.concurrent = map[string]int{}
	}
	if limit > 0 && s.concurrent[ip] >= limit {
		return false
	}
	s.concurrent[ip]++
	return true
}
func (s *State) Release(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.concurrent[ip] <= 1 {
		delete(s.concurrent, ip)
	} else {
		s.concurrent[ip]--
	}
}
