package api

import (
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
)

func startLocalDebugServerFromEnv() {
	addr := strings.TrimSpace(os.Getenv("MAN_P2P_PPROF_ADDR"))
	if addr == "" {
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Printf("[WARN] pprof disabled: invalid MAN_P2P_PPROF_ADDR=%q: %v", addr, err)
		return
	}
	if !isAllowedPprofHost(host) {
		log.Printf("[WARN] pprof disabled: MAN_P2P_PPROF_ADDR must be localhost, got %q", addr)
		return
	}
	go func() {
		log.Printf("[INFO] pprof listening on %s", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Printf("[WARN] pprof server stopped: %v", err)
		}
	}()
}

func isLocalDebugAddr(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	return isAllowedPprofHost(host)
}

func isAllowedPprofHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}
