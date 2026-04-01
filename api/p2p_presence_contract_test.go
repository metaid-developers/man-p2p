package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"man-p2p/api/respond"
	"man-p2p/p2p"
)

func TestP2PPresenceEndpointReturnsHealthySnapshot(t *testing.T) {
	p2p.SetPresenceStatusForTests(p2p.PresenceStatus{
		Healthy:   true,
		PeerCount: 2,
		NowSec:    1760000000,
		OnlineBots: map[string]p2p.PresenceBotState{
			" IDQ1BotA ": {
				PeerIDs:      []string{"peer-a"},
				LastSeenSec:  1759999988,
				ExpiresAtSec: 1760000043,
			},
		},
	})
	defer p2p.ResetPresenceStatusForTests()

	r := setupP2PTestRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/p2p/presence", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var res respond.ApiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("expected valid json response: %v", err)
	}
	if res.Code != 1 {
		t.Fatalf("expected success envelope code 1, got %d", res.Code)
	}

	data, ok := res.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data payload, got %#v", res.Data)
	}
	if got, ok := data["healthy"].(bool); !ok || !got {
		t.Fatalf("expected healthy=true, got %#v", data["healthy"])
	}
	if got, ok := data["peerCount"].(float64); !ok || got != 2 {
		t.Fatalf("expected peerCount=2, got %#v", data["peerCount"])
	}
	if got, ok := data["nowSec"].(float64); !ok || got != 1760000000 {
		t.Fatalf("expected nowSec=1760000000, got %#v", data["nowSec"])
	}

	onlineBots, ok := data["onlineBots"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected onlineBots object, got %#v", data["onlineBots"])
	}
	bot, ok := onlineBots["idq1bota"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected canonical online bot key, got %#v", onlineBots)
	}
	peerIDs, ok := bot["peerIds"].([]interface{})
	if !ok || len(peerIDs) != 1 || peerIDs[0] != "peer-a" {
		t.Fatalf("expected peerIds [peer-a], got %#v", bot["peerIds"])
	}
}

func TestP2PPresenceEndpointReportsNoActivePeersAsUnhealthy(t *testing.T) {
	p2p.SetPresenceStatusForTests(p2p.PresenceStatus{
		Healthy:         false,
		PeerCount:       0,
		UnhealthyReason: "no_active_peers",
		OnlineBots:      map[string]p2p.PresenceBotState{},
	})
	defer p2p.ResetPresenceStatusForTests()

	r := setupP2PTestRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/p2p/presence", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var res respond.ApiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("expected valid json response: %v", err)
	}
	if res.Code != 1 {
		t.Fatalf("expected success envelope code 1, got %d", res.Code)
	}

	data, ok := res.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data payload, got %#v", res.Data)
	}
	if got, ok := data["healthy"].(bool); !ok || got {
		t.Fatalf("expected healthy=false, got %#v", data["healthy"])
	}
	if got, ok := data["peerCount"].(float64); !ok || got != 0 {
		t.Fatalf("expected peerCount=0, got %#v", data["peerCount"])
	}
	if got := data["unhealthyReason"]; got != "no_active_peers" {
		t.Fatalf("expected unhealthyReason no_active_peers, got %#v", got)
	}
	if onlineBots, ok := data["onlineBots"].(map[string]interface{}); !ok || len(onlineBots) != 0 {
		t.Fatalf("expected empty onlineBots object, got %#v", data["onlineBots"])
	}
}
