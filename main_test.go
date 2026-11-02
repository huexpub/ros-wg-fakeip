package main

import (
	"net"
	"testing"
)

func TestNewFakeDNSAndPoolExcludesNetworkAndBroadcast(t *testing.T) {
	f, err := newFakeDNS("198.18.0.0/30", "127.0.0.1:53")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.pool) != 2 || f.pool[0].String() != "198.18.0.1" || f.pool[1].String() != "198.18.0.2" {
		t.Fatalf("unexpected pool: %v", f.pool)
	}
}

func TestHostnameForIP(t *testing.T) {
	f, err := newFakeDNS("198.18.0.0/30", "127.0.0.1:53")
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.byIP["198.18.0.1"] = "example.com."
	f.mu.Unlock()

	name, ok := f.hostnameForIP(net.ParseIP("198.18.0.1"))
	if !ok || name != "example.com." {
		t.Fatalf("hostnameForIP() = %q, %v", name, ok)
	}
	if _, ok := f.hostnameForIP(net.ParseIP("203.0.113.1")); ok {
		t.Fatal("unexpected hostname for unknown IP")
	}
}

func TestNewFakeDNSRejectsLargePool(t *testing.T) {
	if _, err := newFakeDNS("10.0.0.0/8", "127.0.0.1:53"); err == nil {
		t.Fatal("expected oversized pool to be rejected")
	}
}
