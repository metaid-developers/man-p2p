package api

import "testing"

func TestLocalDebugAddrAllowsOnlyLocalhostBinds(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1:6060",
		"localhost:6060",
		"[::1]:6060",
	} {
		if !isLocalDebugAddr(addr) {
			t.Fatalf("expected %q to be allowed", addr)
		}
	}
}

func TestLocalDebugAddrRejectsNonLocalOrInvalidBinds(t *testing.T) {
	for _, addr := range []string{
		"",
		"127.0.0.1",
		":6060",
		"0.0.0.0:6060",
		"192.168.1.10:6060",
		"[::]:6060",
		"::1:6060",
	} {
		if isLocalDebugAddr(addr) {
			t.Fatalf("expected %q to be rejected", addr)
		}
	}
}
