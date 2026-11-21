// Package clients tracks proxy clients per source IP independently of capture.
//
// A Tracker is in-memory only and lives for the process lifetime; restarting
// the proxy clears it, and clearing captured flows does not touch it. Entries
// aggregate by IP: no ports or process attribution are kept.
package clients

import (
	"sort"
	"sync"
	"time"
)

const (
	// maxHosts is how many recent hostnames one client keeps, newest first.
	maxHosts = 8
	// maxClients caps one Snapshot. The oldest entries above the cap are dropped.
	maxClients = 200
	// idleTTL is how long a client without active connections stays visible.
	idleTTL = 5 * time.Minute
)

// Client is one aggregated client, keyed by source IP.
type Client struct {
	IP          string    `json:"ip"`
	ActiveConns int       `json:"active_conns"`
	Requests    int       `json:"requests"`
	Tunnels     int       `json:"tunnels"`
	LastSeen    time.Time `json:"last_seen"`
	Hosts       []string  `json:"hosts"`
}

// Tracker aggregates proxy activity per client IP. It is safe for concurrent
// use. A nil *Tracker is not valid; proxy callers guard against nil instead.
type Tracker struct {
	mu   sync.Mutex
	now  func() time.Time
	byIP map[string]*Client
}

// New returns a Tracker driven by the wall clock.
func New() *Tracker {
	return NewWithClock(time.Now)
}

// NewWithClock returns a Tracker driven by now, which lets tests control
// pruning and ordering. A nil now falls back to time.Now.
func NewWithClock(now func() time.Time) *Tracker {
	if now == nil {
		now = time.Now
	}
	return &Tracker{now: now, byIP: make(map[string]*Client)}
}

// AddConn records a newly accepted connection from ip.
func (t *Tracker) AddConn(ip string) {
	if t == nil || ip == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.entry(ip)
	c.ActiveConns++
}

// DropConn records that one connection from ip closed. Closing does not count
// as activity: the entry ages out from its last request or tunnel.
func (t *Tracker) DropConn(ip string) {
	if t == nil || ip == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.byIP[ip]
	if !ok {
		return
	}
	if c.ActiveConns > 0 {
		c.ActiveConns--
	}
}

// Request counts one forwarded request from ip to host.
func (t *Tracker) Request(ip, host string) {
	if t == nil || ip == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.entry(ip)
	c.Requests++
	c.Hosts = addHost(c.Hosts, host)
}

// Tunnel counts one CONNECT tunnel from ip to host, decrypted or spliced.
func (t *Tracker) Tunnel(ip, host string) {
	if t == nil || ip == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.entry(ip)
	c.Tunnels++
	c.Hosts = addHost(c.Hosts, host)
}

// entry returns ip's entry, creating and stamping it as last seen now. The
// caller must hold t.mu.
func (t *Tracker) entry(ip string) *Client {
	c, ok := t.byIP[ip]
	if !ok {
		c = &Client{IP: ip, Hosts: []string{}}
		t.byIP[ip] = c
	}
	c.LastSeen = t.now()
	return c
}

// addHost returns the recent-host list with host first, deduped, and capped.
// An empty host leaves the list unchanged.
func addHost(hosts []string, host string) []string {
	if host == "" {
		return hosts
	}
	next := make([]string, 0, maxHosts)
	next = append(next, host)
	for _, h := range hosts {
		if h == host {
			continue
		}
		if len(next) == maxHosts {
			break
		}
		next = append(next, h)
	}
	return next
}

// Snapshot returns a copy of the tracked clients. Idle entries (no active
// connections and last seen more than five minutes ago) are pruned, the list is
// sorted by last seen descending with IP ascending as the tiebreak, and only
// the 200 most recent entries are returned.
func (t *Tracker) Snapshot() []Client {
	if t == nil {
		return []Client{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	out := make([]Client, 0, len(t.byIP))
	for ip, c := range t.byIP {
		if c.ActiveConns == 0 && now.Sub(c.LastSeen) > idleTTL {
			delete(t.byIP, ip)
			continue
		}
		copied := *c
		copied.Hosts = make([]string, len(c.Hosts))
		copy(copied.Hosts, c.Hosts)
		out = append(out, copied)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].IP < out[j].IP
	})
	if len(out) > maxClients {
		out = out[:maxClients]
	}
	return out
}
