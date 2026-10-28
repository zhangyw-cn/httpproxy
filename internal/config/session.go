package config

import "sync"

type Overrides struct {
	Proxy    bool
	UI       bool
	MaxFlows bool
	MaxBody  bool
}

type Applied struct {
	File        Values
	Runtime     Values
	SetMaxFlows bool
	MaxFlows    int
	SetMaxBody  bool
	MaxBody     int64
}

type Session struct {
	mu      sync.Mutex
	dir     *Dir
	file    Values
	runtime Values
	ov      Overrides
}

func Effective(file Values, proxy string, ui string, maxFlows int, maxBody int64, ov Overrides) Values {
	out := file
	if ov.Proxy {
		out.Proxy = proxy
	}
	if ov.UI {
		out.UI = ui
	}
	if ov.MaxFlows {
		out.MaxFlows = maxFlows
	}
	if ov.MaxBody {
		out.MaxBody = maxBody
	}
	return out
}

func NewSession(dir *Dir, file Values, ov Overrides, proxy string, ui string, maxFlows int, maxBody int64) *Session {
	return &Session{
		dir:     dir,
		file:    file,
		runtime: Effective(file, proxy, ui, maxFlows, maxBody, ov),
		ov:      ov,
	}
}

func (s *Session) Snapshot() (file Values, runtime Values) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file, s.runtime
}

func (s *Session) Save(next Values) (Applied, error) {
	return s.SaveAndApply(next, nil)
}

// SaveAndApply writes next, updates memory, then runs apply while still
// holding the session lock. A second save cannot observe the new runtime
// until apply has finished updating the ring and the body limit.
func (s *Session) SaveAndApply(next Values, apply func(Applied)) (Applied, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.dir.Save(next); err != nil {
		return Applied{}, err
	}

	s.file = next
	applied := Applied{File: s.file}
	if !s.ov.MaxFlows {
		s.runtime.MaxFlows = next.MaxFlows
		applied.SetMaxFlows = true
		applied.MaxFlows = next.MaxFlows
	}
	if !s.ov.MaxBody {
		s.runtime.MaxBody = next.MaxBody
		applied.SetMaxBody = true
		applied.MaxBody = next.MaxBody
	}
	s.runtime.Theme = next.Theme
	s.runtime.Capture = next.Capture
	s.runtime.CaptureEmpty = next.CaptureEmpty
	s.runtime.CaptureHTTPS = next.CaptureHTTPS
	hosts := append([]string{}, next.CaptureHosts...)
	if len(hosts) == 0 {
		hosts = []string{}
	}
	s.runtime.CaptureHosts = hosts
	applied.Runtime = s.runtime
	if apply != nil {
		apply(applied)
	}
	return applied, nil
}
