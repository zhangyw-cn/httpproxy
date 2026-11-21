package clients

import (
	"fmt"
	"testing"
	"time"
)

func TestTrackerCounts(t *testing.T) {
	now := time.Unix(1000, 0)
	tr := NewWithClock(func() time.Time { return now })
	tr.AddConn("127.0.0.1")
	now = now.Add(time.Second)
	tr.AddConn("::1")
	now = now.Add(time.Second)
	tr.Request("127.0.0.1", "api.example.com")
	now = now.Add(time.Second)
	tr.Tunnel("::1", "v6.example.com")
	tr.DropConn("127.0.0.1")
	got := tr.Snapshot()
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].IP != "::1" || got[0].Tunnels != 1 || got[0].ActiveConns != 1 {
		t.Fatalf("%+v", got[0])
	}
	if got[1].IP != "127.0.0.1" || got[1].Requests != 1 || got[1].ActiveConns != 0 {
		t.Fatalf("%+v", got[1])
	}
	if len(got[1].Hosts) != 1 || got[1].Hosts[0] != "api.example.com" {
		t.Fatalf("%+v", got[1])
	}
}

func TestTrackerHostsAndPrune(t *testing.T) {
	now := time.Unix(1000, 0)
	tr := NewWithClock(func() time.Time { return now })
	for i := 0; i < 10; i++ {
		tr.Request("1.2.3.4", fmt.Sprintf("h%d.example.com", i))
	}
	first := tr.Snapshot()[0]
	if len(first.Hosts) != 8 || first.Hosts[0] != "h9.example.com" {
		t.Fatalf("%+v", first.Hosts)
	}
	if first.Hosts[7] != "h2.example.com" {
		t.Fatalf("oldest kept host = %q", first.Hosts[7])
	}
	now = now.Add(4 * time.Minute)
	tr.Request("5.6.7.8", "fresh.example.com")
	now = now.Add(2 * time.Minute)
	got := tr.Snapshot()
	if len(got) != 1 || got[0].IP != "5.6.7.8" {
		t.Fatalf("%+v", got)
	}
}

func TestTrackerSnapshotCapsAt200(t *testing.T) {
	now := time.Unix(1000, 0)
	tr := NewWithClock(func() time.Time { return now })
	for i := 0; i < 205; i++ {
		tr.Request(fmt.Sprintf("10.0.0.%d", i), "cap.example.com")
		now = now.Add(time.Second)
	}
	got := tr.Snapshot()
	if len(got) != 200 {
		t.Fatalf("snapshot has %d clients", len(got))
	}
	if got[0].IP != "10.0.0.204" {
		t.Fatalf("newest client = %q", got[0].IP)
	}
	if got[len(got)-1].IP != "10.0.0.5" {
		t.Fatalf("oldest kept client = %q", got[len(got)-1].IP)
	}
	for _, c := range got {
		if c.IP == "10.0.0.0" {
			t.Fatalf("oldest client was not dropped: %+v", c)
		}
	}
}

func TestTrackerSnapshotTiebreakAndDeepCopy(t *testing.T) {
	now := time.Unix(1000, 0)
	tr := NewWithClock(func() time.Time { return now })
	tr.Request("10.0.0.2", "b.example.com")
	tr.Request("10.0.0.1", "a.example.com")
	got := tr.Snapshot()
	if len(got) != 2 || got[0].IP != "10.0.0.1" || got[1].IP != "10.0.0.2" {
		t.Fatalf("%+v", got)
	}
	got[0].Hosts[0] = "mutated.example.com"
	again := tr.Snapshot()
	if again[0].Hosts[0] != "a.example.com" {
		t.Fatalf("snapshot aliases tracker state: %+v", again[0].Hosts)
	}
}

func TestTrackerDropConnDoesNotLeak(t *testing.T) {
	now := time.Unix(1000, 0)
	tr := NewWithClock(func() time.Time { return now })
	tr.DropConn("10.9.9.9")
	if got := tr.Snapshot(); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
	tr.AddConn("10.9.9.9")
	tr.DropConn("10.9.9.9")
	tr.DropConn("10.9.9.9")
	got := tr.Snapshot()
	if len(got) != 1 || got[0].ActiveConns != 0 {
		t.Fatalf("%+v", got)
	}
}
