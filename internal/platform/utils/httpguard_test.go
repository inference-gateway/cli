package utils

import (
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"192.168.1.1", false},
		{"172.16.0.1", false},
		{"169.254.169.254", false},
		{"0.0.0.0", false},
		{"::1", false},
		{"fe80::1", false},
		{"::ffff:127.0.0.1", false},
		{"140.82.121.4", true},
		{"2606:2800:220:1:248:1893:25c8:1946", true},
	}
	for _, tt := range tests {
		ip := netip.MustParseAddr(tt.addr)
		if got := isPublicIP(ip); got != tt.want {
			t.Errorf("isPublicIP(%s) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}

func TestNewPublicOnlyHTTPClientBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(nil)
	defer srv.Close()

	client := NewPublicOnlyHTTPClient(5 * time.Second)
	if _, err := client.Get(srv.URL); err == nil {
		t.Fatal("expected loopback request to be refused")
	}
}
