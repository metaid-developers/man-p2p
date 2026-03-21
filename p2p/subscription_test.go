package p2p

import "testing"

func TestBlocklistOverridesAllowlist(t *testing.T) {
	_ = LoadConfig(writeTempConfig(t, `{
        "p2p_sync_mode": "selective",
        "p2p_selective_addresses": ["1AllowedAddr"],
        "p2p_block_addresses": ["1AllowedAddr"]
    }`))
	ann := PinAnnouncement{PinId: "p1", Address: "1AllowedAddr", Path: "/info/name"}
	if ShouldSync(ann) {
		t.Error("blocked address should not sync even if in selective list")
	}
}

func TestSelectivePathMatch(t *testing.T) {
	_ = LoadConfig(writeTempConfig(t, `{
        "p2p_sync_mode": "selective",
        "p2p_selective_paths": ["/info/*"]
    }`))
	ann := PinAnnouncement{PinId: "p2", Address: "1Addr", Path: "/info/name", SizeBytes: 50}
	if !ShouldSync(ann) {
		t.Error("/info/name should match /info/* pattern")
	}
}

func TestOversizedPinStillPassesFilter(t *testing.T) {
	_ = LoadConfig(writeTempConfig(t, `{
        "p2p_sync_mode": "full",
        "p2p_max_content_size_kb": 100
    }`))
	ann := PinAnnouncement{PinId: "p3", Address: "1Addr", Path: "/info/name", SizeBytes: 200 * 1024}
	if !ShouldSync(ann) {
		t.Error("oversized PIN should pass filter in full mode")
	}
}

func TestSelfMode(t *testing.T) {
	_ = LoadConfig(writeTempConfig(t, `{
        "p2p_sync_mode": "self",
        "p2p_own_addresses": ["1MyAddr"]
    }`))
	ann := PinAnnouncement{PinId: "p4", Address: "1MyAddr", Path: "/info/name"}
	if !ShouldSync(ann) {
		t.Error("own address should sync in self mode")
	}
	ann2 := PinAnnouncement{PinId: "p5", Address: "1OtherAddr", Path: "/info/name"}
	if ShouldSync(ann2) {
		t.Error("other address should not sync in self mode")
	}
}

func TestLoadOwnAddressesForSelfMode(t *testing.T) {
	_ = LoadConfig(writeTempConfig(t, `{
        "p2p_sync_mode": "self",
        "p2p_own_addresses": ["1ConfigOwnAddr"]
    }`))
	ann := PinAnnouncement{PinId: "p-self-config", Address: "1ConfigOwnAddr", Path: "/info/name"}
	if !ShouldSync(ann) {
		t.Error("configured own address should sync in self mode")
	}
}

func TestSelfModeRequiresConfiguredOwnAddress(t *testing.T) {
	OwnAddresses = []string{"1LegacyOwnAddr"}
	t.Cleanup(func() {
		OwnAddresses = nil
	})

	_ = LoadConfig(writeTempConfig(t, `{
        "p2p_sync_mode": "self"
    }`))
	ann := PinAnnouncement{PinId: "p-self-legacy", Address: "1LegacyOwnAddr", Path: "/info/name"}
	if ShouldSync(ann) {
		t.Error("self mode should not rely on legacy global own addresses")
	}
}

func TestSelfModeBlockOverridesOwnAddress(t *testing.T) {
	_ = LoadConfig(writeTempConfig(t, `{
        "p2p_sync_mode": "self",
        "p2p_own_addresses": ["1BlockedOwnAddr"],
        "p2p_block_addresses": ["1BlockedOwnAddr"]
    }`))
	ann := PinAnnouncement{PinId: "p-self-blocked", Address: "1BlockedOwnAddr", Path: "/info/name"}
	if ShouldSync(ann) {
		t.Error("blocked own address should not sync in self mode")
	}
}

func TestBlockedPath(t *testing.T) {
	_ = LoadConfig(writeTempConfig(t, `{
        "p2p_sync_mode": "full",
        "p2p_block_paths": ["/files/*.mp4"]
    }`))
	ann := PinAnnouncement{PinId: "p6", Address: "1Addr", Path: "/files/video.mp4"}
	if ShouldSync(ann) {
		t.Error("/files/video.mp4 should be blocked by /files/*.mp4 pattern")
	}
	ann2 := PinAnnouncement{PinId: "p7", Address: "1Addr", Path: "/files/doc.pdf"}
	if !ShouldSync(ann2) {
		t.Error("/files/doc.pdf should not be blocked")
	}
}
