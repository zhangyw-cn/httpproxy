package store_test

import (
	"testing"

	"httpproxy/internal/flow"
	"httpproxy/internal/store"
)

func TestRingEvictsOldest(t *testing.T) {
	s := store.New(2)
	s.Add(flow.Flow{ID: "1", Host: "a"})
	s.Add(flow.Flow{ID: "2", Host: "b"})
	s.Add(flow.Flow{ID: "3", Host: "c"})
	if _, ok := s.Get("1"); ok {
		t.Fatal("id 1 should be evicted")
	}
	list := s.List(store.Filter{})
	if len(list) != 2 || list[0].ID != "3" {
		t.Fatalf("want newest-first [3,2], got %+v", list)
	}
}

func TestFilterHostPathStatus(t *testing.T) {
	s := store.New(10)
	s.Add(flow.Flow{ID: "1", Host: "api.example.com", Path: "/v1/users", StatusCode: 200})
	s.Add(flow.Flow{ID: "2", Host: "cdn.example.com", Path: "/img.png", StatusCode: 404})
	got := s.List(store.Filter{Host: "api", Path: "users", Status: "2"})
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("filter failed: %+v", got)
	}
}

func TestClear(t *testing.T) {
	s := store.New(10)
	s.Add(flow.Flow{ID: "1"})
	s.Clear()
	if len(s.List(store.Filter{})) != 0 {
		t.Fatal("expected empty after clear")
	}
}

func TestUpdateMissingReturnsFalse(t *testing.T) {
	s := store.New(10)
	if s.Update(flow.Flow{ID: "nope"}) {
		t.Fatal("expected false")
	}
}
