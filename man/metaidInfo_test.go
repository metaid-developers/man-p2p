package man

import (
	"man-p2p/pin"
	"testing"
)

// TestChatPubKeyParsed verifies that a PIN with path /info/chatpubkey
// is correctly parsed into MetaIdInfo.ChatPubKey.
// We pre-seed the metaIdData map with the address key so that
// metaIdInfoParse does not attempt a PebbleStore DB lookup.
func TestChatPubKeyParsed(t *testing.T) {
	pinNode := &pin.PinInscription{
		Id:          "chatpubkey-pin-id",
		MetaId:      "test-metaid",
		Address:     "1GrqX7K9jdnUor8hAoAfDx99uFH2tT75Za",
		Path:        "/info/chatpubkey",
		ContentBody: []byte("02abc123...publickey"),
	}

	metaIdData := make(map[string]*pin.MetaIdInfo)
	// Pre-seed with address key so the PebbleStore lookup is bypassed.
	metaIdData[pinNode.Address] = &pin.MetaIdInfo{
		MetaId:  "test-metaid",
		Address: "1TestAddr",
	}

	metaIdInfoParse(pinNode, "", &metaIdData)

	info, ok := metaIdData[pinNode.Address]
	if !ok {
		t.Fatalf("metaIdData has no entry for address %q after parse", pinNode.Address)
	}
	if info.ChatPubKey != "02abc123...publickey" {
		t.Errorf("chatpubkey not parsed correctly: got %q, want %q", info.ChatPubKey, "02abc123...publickey")
	}
	if info.ChatPubKeyId != "chatpubkey-pin-id" {
		t.Errorf("chatpubkeyId not parsed correctly: got %q, want %q", info.ChatPubKeyId, "chatpubkey-pin-id")
	}
	if info.GlobalMetaId == "" {
		t.Errorf("expected globalMetaId to be populated for a valid address")
	}
}

// TestChatPublicKeyPathVariant verifies the lowercase /info/chatpublickey
// path variant.  The existing switch statement does NOT handle this variant —
// only /info/chatpubkey is handled.  This test documents the current
// (expected) behavior: chatpublickey path is silently ignored.
// If support for the variant is added in the future, update this test.
func TestChatPublicKeyPathVariantNotHandled(t *testing.T) {
	pinNode := &pin.PinInscription{
		MetaId:      "test-metaid-2",
		Address:     "1TestAddr2",
		Path:        "/info/chatpublickey",
		ContentBody: []byte("02abc123...publickey"),
	}

	metaIdData := make(map[string]*pin.MetaIdInfo)
	metaIdData[pinNode.Address] = &pin.MetaIdInfo{
		MetaId:  "test-metaid-2",
		Address: "1TestAddr2",
	}

	metaIdInfoParse(pinNode, "", &metaIdData)

	info, ok := metaIdData[pinNode.Address]
	if !ok {
		t.Fatalf("metaIdData has no entry for address %q after parse", pinNode.Address)
	}
	// /info/chatpublickey is not in the switch; ChatPubKey stays empty.
	if info.ChatPubKey != "" {
		t.Logf("NOTE: /info/chatpublickey is now handled (ChatPubKey=%q); update this test", info.ChatPubKey)
	}
}
