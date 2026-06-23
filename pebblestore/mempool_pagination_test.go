package pebblestore

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"man-p2p/pin"

	"github.com/bytedance/sonic"
	"github.com/cockroachdb/pebble"
)

func newMempoolTestDB(t *testing.T) *Database {
	t.Helper()

	db, err := NewDataBase(t.TempDir(), 1)
	if err != nil {
		t.Fatalf("NewDataBase error: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func insertMempoolPin(t *testing.T, db *Database, id string, seenTime int64, body string) {
	t.Helper()

	pinNode := &pin.PinInscription{
		Id:            id,
		ChainName:     "mvc",
		GenesisHeight: -1,
		Timestamp:     seenTime,
		SeenTime:      seenTime,
		ContentBody:   []byte(body),
	}
	if err := db.SetAllPins(-1, []*pin.PinInscription{pinNode}, 20000); err != nil {
		t.Fatalf("SetAllPins %s error: %v", id, err)
	}
	if err := db.SetMempool(pinNode); err != nil {
		t.Fatalf("SetMempool %s error: %v", id, err)
	}
}

func TestGetMempoolPageListTrimsContentBody(t *testing.T) {
	t.Parallel()

	db := newMempoolTestDB(t)
	insertMempoolPin(t, db, "pin-1", 1, "large-body")

	list, err := db.GetMempoolPageList(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("GetMempoolPageList error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 result, got %d", len(list))
	}
	if len(list[0].ContentBody) != 0 {
		t.Fatalf("expected ContentBody to be trimmed, got %q", string(list[0].ContentBody))
	}
	if list[0].ContentSummary != "large-body" {
		t.Fatalf("expected ContentSummary large-body, got %q", list[0].ContentSummary)
	}

	longDB := newMempoolTestDB(t)
	longBody := strings.Repeat("x", maxMempoolListSummaryBytes+1)
	insertMempoolPin(t, longDB, "pin-long", 1, longBody)

	longList, err := longDB.GetMempoolPageList(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("GetMempoolPageList long body error: %v", err)
	}
	if len(longList) != 1 {
		t.Fatalf("expected 1 long body result, got %d", len(longList))
	}
	if len(longList[0].ContentBody) != 0 {
		t.Fatalf("expected long ContentBody to be trimmed, got length %d", len(longList[0].ContentBody))
	}
	if len(longList[0].ContentSummary) != maxMempoolListSummaryBytes {
		t.Fatalf("expected long ContentSummary length %d, got %d", maxMempoolListSummaryBytes, len(longList[0].ContentSummary))
	}

	summaryDB := newMempoolTestDB(t)
	summaryPin := &pin.PinInscription{
		Id:             "pin-summary",
		ChainName:      "mvc",
		GenesisHeight:  -1,
		Timestamp:      1,
		SeenTime:       1,
		ContentSummary: strings.Repeat("s", maxMempoolListSummaryBytes+1),
	}
	if err := summaryDB.SetAllPins(-1, []*pin.PinInscription{summaryPin}, 20000); err != nil {
		t.Fatalf("SetAllPins summary pin error: %v", err)
	}
	if err := summaryDB.SetMempool(summaryPin); err != nil {
		t.Fatalf("SetMempool summary pin error: %v", err)
	}

	summaryList, err := summaryDB.GetMempoolPageList(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("GetMempoolPageList summary pin error: %v", err)
	}
	if len(summaryList) != 1 {
		t.Fatalf("expected 1 summary result, got %d", len(summaryList))
	}
	if len(summaryList[0].ContentBody) != 0 {
		t.Fatalf("expected summary ContentBody to be empty, got length %d", len(summaryList[0].ContentBody))
	}
	if len(summaryList[0].ContentSummary) != maxMempoolListSummaryBytes {
		t.Fatalf("expected existing ContentSummary length %d, got %d", maxMempoolListSummaryBytes, len(summaryList[0].ContentSummary))
	}
}

func TestGetMempoolPageListSkipsCorruptPinData(t *testing.T) {
	t.Parallel()

	db := newMempoolTestDB(t)
	insertMempoolPin(t, db, "pin-good", 10, "good-body")

	if err := db.getShard("pin-bad").Set([]byte("pin-bad"), []byte("{bad-json"), pebble.Sync); err != nil {
		t.Fatalf("set corrupt pin data error: %v", err)
	}
	if err := db.PinsMempoolDb.Set([]byte("pin-bad"), nil, pebble.Sync); err != nil {
		t.Fatalf("set corrupt mempool key error: %v", err)
	}

	list, err := db.GetMempoolPageList(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("GetMempoolPageList error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 result, got %d", len(list))
	}
	if list[0].Id != "pin-good" {
		t.Fatalf("expected only good pin, got %s", list[0].Id)
	}
}

func TestGetMempoolPageListKeepsOnlyRequestedWindow(t *testing.T) {
	t.Parallel()

	db := newMempoolTestDB(t)
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("pin-%04d", i)
		insertMempoolPin(t, db, id, int64(i), "")
	}

	list, err := db.GetMempoolPageList(context.Background(), 9, 25)
	if err != nil {
		t.Fatalf("GetMempoolPageList error: %v", err)
	}
	if len(list) != 25 {
		json, _ := sonic.MarshalString(list)
		t.Fatalf("expected 25 results, got %d: %s", len(list), json)
	}
	if list[0].Id != "pin-0774" {
		json, _ := sonic.MarshalString(list)
		t.Fatalf("expected first id pin-0774, got %s: %s", list[0].Id, json)
	}
}
