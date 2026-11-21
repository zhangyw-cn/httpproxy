package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"httpproxy/internal/cert"
	"httpproxy/internal/config"
	"httpproxy/internal/flow"
	"httpproxy/internal/hosts"
	"httpproxy/internal/proxy"
	"httpproxy/internal/store"
)

type Server struct {
	store   *store.Store
	ca      *cert.Manager
	hub     *Hub
	sink    *StoreSink
	mux     *http.ServeMux
	session *config.Session
	proxy   *proxy.Proxy

	hostsProfiles *hosts.Profiles
	hostsMu       sync.Mutex
	hostsActive   string
	hostsRules    int
	hostsErr      string
	hostsModTime  time.Time
	hostsSize     int64
}

func New(st *store.Store, ca *cert.Manager, session *config.Session, px *proxy.Proxy) *Server {
	hub := NewHub()
	s := &Server{
		store:   st,
		ca:      ca,
		hub:     hub,
		sink:    &StoreSink{Store: st, Hub: hub},
		mux:     http.NewServeMux(),
		session: session,
		proxy:   px,
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) Sink() proxy.FlowSink {
	return s.sink
}

// ClientCount returns the number of active WebSocket clients.
func (s *Server) ClientCount() int {
	return s.hub.ClientCount()
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/config", s.config)
	s.mux.HandleFunc("PUT /api/config", s.putConfig)
	s.mux.HandleFunc("PUT /api/capture", s.putCapture)
	s.mux.HandleFunc("GET /api/hosts", s.listHosts)
	s.mux.HandleFunc("POST /api/hosts", s.createHost)
	s.mux.HandleFunc("PUT /api/hosts", s.activateHosts)
	s.mux.HandleFunc("GET /api/hosts/{name}", s.getHost)
	s.mux.HandleFunc("PUT /api/hosts/{name}", s.putHost)
	s.mux.HandleFunc("POST /api/hosts/{name}/rename", s.renameHost)
	s.mux.HandleFunc("DELETE /api/hosts/{name}", s.deleteHost)
	s.mux.HandleFunc("GET /api/flows", s.listFlows)
	s.mux.HandleFunc("GET /api/flows/{id}", s.getFlow)
	s.mux.HandleFunc("DELETE /api/flows", s.clearFlows)
	s.mux.HandleFunc("GET /api/ca.crt", s.caCert)
	s.mux.HandleFunc("GET /ws/flows", s.webSocket)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{OK: true})
}

func (s *Server) listFlows(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
	}
	filter := store.Filter{
		Host:   r.URL.Query().Get("host"),
		Path:   r.URL.Query().Get("path"),
		Status: r.URL.Query().Get("status"),
		Limit:  limit,
	}
	flows := s.store.List(filter)
	if flows == nil {
		flows = []flow.FlowSummary{}
	}
	writeJSON(w, http.StatusOK, flows)
}

func (s *Server) getFlow(w http.ResponseWriter, r *http.Request) {
	f, ok := s.store.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) clearFlows(w http.ResponseWriter, _ *http.Request) {
	s.store.Clear()
	s.hub.Broadcast(Event{Type: "flows.cleared"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) caCert(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(s.ca.CACertPEM())
}

func (s *Server) webSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	client := s.hub.add(conn)
	defer func() {
		s.hub.remove(client)
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
