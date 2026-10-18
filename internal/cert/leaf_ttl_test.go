package cert

import (
	"crypto/tls"
	"testing"
	"time"
)

func TestGetCertificateRefreshesLeafNearExpiry(t *testing.T) {
	manager, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	manager.now = func() time.Time { return base }
	hello := &tls.ClientHelloInfo{ServerName: "example.test"}

	first, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return base.Add(leafValidity - 2*leafRefreshBefore) }
	cached, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatal(err)
	}
	if cached != first {
		t.Fatal("leaf with ample validity left should be served from cache")
	}

	manager.now = func() time.Time { return base.Add(leafValidity - leafRefreshBefore/2) }
	refreshed, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed == first {
		t.Fatal("leaf within refresh window was not regenerated")
	}
	if !refreshed.Leaf.NotAfter.After(first.Leaf.NotAfter) {
		t.Fatalf("refreshed NotAfter %v not after original %v", refreshed.Leaf.NotAfter, first.Leaf.NotAfter)
	}
}
