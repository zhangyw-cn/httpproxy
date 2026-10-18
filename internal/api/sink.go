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
	s.Store.Add(f)
	summary := f.Summary()
	s.Hub.Broadcast(Event{Type: "flow.created", Flow: &summary})
}

func (s *StoreSink) OnUpdated(f flow.Flow) {
	s.Store.Update(f)
	summary := f.Summary()
	s.Hub.Broadcast(Event{Type: "flow.updated", Flow: &summary})
}
