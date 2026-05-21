package main

import (
	"net/http"
	"testing"
	"time"
)

func TestListenExposureWarning(t *testing.T) {
	for _, tc := range []struct {
		name     string
		addr     string
		wantWarn bool
	}{
		{name: "ipv4 loopback", addr: "127.0.0.1:17680", wantWarn: false},
		{name: "localhost", addr: "localhost:17680", wantWarn: false},
		{name: "ipv6 loopback", addr: "[::1]:17680", wantWarn: false},
		{name: "all interfaces", addr: "0.0.0.0:17680", wantWarn: true},
		{name: "implicit all interfaces", addr: ":17680", wantWarn: true},
		{name: "lan address", addr: "192.168.0.2:17680", wantWarn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := listenExposureWarning(tc.addr)
			gotWarn := got != ""
			if gotWarn != tc.wantWarn {
				t.Fatalf("listenExposureWarning(%q) warning = %v, want %v; message = %q", tc.addr, gotWarn, tc.wantWarn, got)
			}
		})
	}
}

func TestDefaultListenAddressUsesReleasePort(t *testing.T) {
	if defaultListenAddress != "127.0.0.1:17680" {
		t.Fatalf("defaultListenAddress = %q, want 127.0.0.1:17680", defaultListenAddress)
	}
}

func TestNewHTTPServerSetsConnectionTimeouts(t *testing.T) {
	server := newHTTPServer("127.0.0.1:0", http.NewServeMux())

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want 5s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 10*time.Minute {
		t.Fatalf("ReadTimeout = %s, want 10m", server.ReadTimeout)
	}
	if server.WriteTimeout != 10*time.Minute {
		t.Fatalf("WriteTimeout = %s, want 10m", server.WriteTimeout)
	}
	if server.IdleTimeout != time.Minute {
		t.Fatalf("IdleTimeout = %s, want 1m", server.IdleTimeout)
	}
}
