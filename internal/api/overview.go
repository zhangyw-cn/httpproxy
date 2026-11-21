package api

import (
	"errors"
	"net/http"
	"time"

	"httpproxy/internal/clients"
)

// overviewJSON is the home pane snapshot. Clients is always a slice so the
// JSON shape stays [] when nobody is connected.
type overviewJSON struct {
	Proxy         string           `json:"proxy"`
	UI            string           `json:"ui"`
	StartedAt     time.Time        `json:"started_at"`
	UptimeSeconds int64            `json:"uptime_seconds"`
	Capture       bool             `json:"capture"`
	FlowCount     int              `json:"flow_count"`
	HostsProfile  string           `json:"hosts_profile"`
	HostsRules    int              `json:"hosts_rules"`
	HostsError    string           `json:"hosts_error"`
	Clients       []clients.Client `json:"clients"`
}

// SetClients installs the client tracker. It is called once at startup,
// before the listeners accept requests.
func (s *Server) SetClients(t *clients.Tracker) {
	s.clients = t
}

// overview reports the running state for the 2s home poll. It refreshes the
// active hosts file first, exactly like GET /api/hosts, so an external edit is
// visible without a separate request.
func (s *Server) overview(w http.ResponseWriter, _ *http.Request) {
	if s.session == nil {
		writeConfigError(w, errors.New("nil session"))
		return
	}
	s.refreshActiveHosts()
	_, runtime := s.session.Snapshot()
	active, rules, hostsErr := s.hostsState()
	snap := s.clients.Snapshot()
	if snap == nil {
		snap = []clients.Client{}
	}
	writeJSON(w, http.StatusOK, overviewJSON{
		Proxy:         runtime.Proxy,
		UI:            runtime.UI,
		StartedAt:     s.startedAt,
		UptimeSeconds: int64(time.Since(s.startedAt) / time.Second),
		Capture:       runtime.Capture,
		FlowCount:     s.store.Count(),
		HostsProfile:  active,
		HostsRules:    rules,
		HostsError:    hostsErr,
		Clients:       snap,
	})
}
