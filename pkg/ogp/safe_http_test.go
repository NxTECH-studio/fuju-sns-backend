package ogp

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, // AWS metadata
		{"0.0.0.0", true},
		{"255.255.255.255", true},
		{"224.0.0.1", true},
		{"::1", true},
		{"fe80::1", true},
		{"fc00::1", true},
		{"ff02::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2606:4700:4700::1111", false}, // Cloudflare IPv6
	}
	for _, tc := range tests {
		t.Run(tc.ip, func(t *testing.T) {
			got := IsBlockedIP(net.ParseIP(tc.ip))
			if got != tc.blocked {
				t.Errorf("IsBlockedIP(%s) = %v, want %v", tc.ip, got, tc.blocked)
			}
		})
	}
}

func TestCheckURL_BlocksLiteralPrivateIP(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/",
		"http://10.0.0.1/x",
		"https://169.254.169.254/latest/meta-data",
		"http://[::1]/",
	} {
		u, _ := url.Parse(raw)
		err := checkURL(u)
		if err == nil || !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("checkURL(%s) should be blocked, got %v", raw, err)
		}
	}
}

func TestCheckURL_BlocksBadScheme(t *testing.T) {
	u, _ := url.Parse("file:///etc/passwd")
	if err := checkURL(u); !errors.Is(err, ErrBlockedScheme) {
		t.Errorf("expected ErrBlockedScheme, got %v", err)
	}
}

func TestNewSafeClient_BlocksPrivateDial(t *testing.T) {
	client := NewSafeClient(0)
	// Try to connect to a loopback address. The guard should refuse at
	// dial time before any syscalls reach the socket.
	_, err := client.Get("http://127.0.0.1:9/")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected blocked error, got %v", err)
	}
}
