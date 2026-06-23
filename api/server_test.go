package api

import (
	"net/http"
	"testing"
	"time"
)

func TestRunHTTPServerConfiguresSafetyTimeouts(t *testing.T) {
	handler := http.NewServeMux()
	server := newHTTPServer(":7777", handler)

	if server.Addr != ":7777" {
		t.Fatalf("expected addr :7777, got %q", server.Addr)
	}
	if server.Handler != handler {
		t.Fatalf("expected handler to be assigned")
	}
	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("expected ReadHeaderTimeout 5s, got %s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 15*time.Second {
		t.Fatalf("expected ReadTimeout 15s, got %s", server.ReadTimeout)
	}
	if server.WriteTimeout != 60*time.Second {
		t.Fatalf("expected WriteTimeout 60s, got %s", server.WriteTimeout)
	}
	if server.IdleTimeout != 90*time.Second {
		t.Fatalf("expected IdleTimeout 90s, got %s", server.IdleTimeout)
	}
}
