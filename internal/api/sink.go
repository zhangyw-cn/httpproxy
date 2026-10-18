package api

import (
	"httpproxy/internal/flow"
	"httpproxy/internal/store"
)

type StoreSink struct {
	Store *store.Store
	Hub   *Hub
}

func (s *StoreSink) OnCreated(f flow.Flow) {
	evicted := s.Store.Add(f)
	summary := f.Summary()
	s.Hub.Broadcast(Event{Type: "flow.created", Flow: &summary})
	for _, id := range evicted {
		s.Hub.Broadcast(Event{Type: "flow.evicted", FlowID: id})
	}
}

func (s *StoreSink) OnUpdated(f flow.Flow) {
	if !s.Store.Update(f) {
		return
	}
	summary := f.Summary()
	s.Hub.Broadcast(Event{Type: "flow.updated", Flow: &summary})
}
