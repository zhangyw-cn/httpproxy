package store

import (
	"strconv"
	"strings"
	"sync"

	"httpproxy/internal/flow"
)

type Filter struct {
	Host   string
	Path   string
	Status string
	Limit  int
}

type Store struct {
	mu    sync.RWMutex
	max   int
	order []string // oldest -> newest IDs
	byID  map[string]flow.Flow
}

func New(max int) *Store {
	if max < 1 {
		max = 1
	}
	return &Store{max: max, byID: make(map[string]flow.Flow)}
}

func (s *Store) Add(f flow.Flow) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byID[f.ID]; ok {
		delete(s.byID, f.ID)
		s.removeFromOrder(f.ID)
	}

	s.byID[f.ID] = f
	s.order = append(s.order, f.ID)

	for len(s.order) > s.max {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.byID, oldest)
	}
}

func (s *Store) Update(f flow.Flow) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byID[f.ID]; !ok {
		return false
	}
	s.byID[f.ID] = f
	return true
}

func (s *Store) Get(id string) (flow.Flow, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.byID[id]
	return f, ok
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = nil
	s.byID = make(map[string]flow.Flow)
}

func (s *Store) List(filter Filter) []flow.FlowSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []flow.FlowSummary
	for i := len(s.order) - 1; i >= 0; i-- {
		id := s.order[i]
		f, ok := s.byID[id]
		if !ok {
			continue
		}
		if filter.Host != "" && !strings.Contains(f.Host, filter.Host) {
			continue
		}
		if filter.Path != "" && !strings.Contains(f.Path, filter.Path) {
			continue
		}
		if !matchStatus(f.StatusCode, filter.Status) {
			continue
		}
		out = append(out, f.Summary())
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out
}

func (s *Store) removeFromOrder(id string) {
	for i, oid := range s.order {
		if oid == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}

func matchStatus(code int, filter string) bool {
	if filter == "" {
		return true
	}
	allDigits := true
	for _, r := range filter {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if !allDigits {
		return false
	}
	switch len(filter) {
	case 3:
		want, err := strconv.Atoi(filter)
		if err != nil {
			return false
		}
		return code == want
	case 1:
		d, err := strconv.Atoi(filter)
		if err != nil {
			return false
		}
		return code/100 == d
	case 2:
		want, err := strconv.Atoi(filter)
		if err != nil {
			return false
		}
		return code/10 == want
	default:
		return false
	}
}
