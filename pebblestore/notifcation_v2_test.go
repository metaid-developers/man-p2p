package pebblestore

import (
	"testing"
)

func newNotifcationTestDB(t *testing.T) *Database {
	t.Helper()
	db, err := NewDataBase(t.TempDir(), 2)
	if err != nil {
		t.Fatalf("NewDataBase: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestGetNotifcationListV2AddressScoped 回归：迭代按地址前缀定界。
// 其它地址的 key 全部排在目标地址之前（aaa < zzz），读路径必须只扫目标前缀。
func TestGetNotifcationListV2AddressScoped(t *testing.T) {
	db := newNotifcationTestDB(t)

	otherPayload := []byte(`{"NotifcationId":1}`)
	for i := 0; i < 50; i++ {
		if err := db.SetNotifcationV2("aaa", int64(1000+i), "otherpin", otherPayload); err != nil {
			t.Fatalf("SetNotifcationV2 other: %v", err)
		}
	}
	minePayloads := make([][]byte, 5)
	for i := 0; i < 5; i++ {
		// 每条通知 fromPinId 不同（同 fromPinId 会被去重逻辑合并），value 内嵌同名字段
		minePayloads[i] = []byte(`{"NotifcationId":2,"fromPinId":"mypin` + string(rune('a'+i)) + `"}`)
		if err := db.SetNotifcationV2("zzz", int64(2000+i), "mypin"+string(rune('a'+i)), minePayloads[i]); err != nil {
			t.Fatalf("SetNotifcationV2 mine: %v", err)
		}
	}

	list, total, err := db.GetNotifcationListV2("zzz", 0, 20)
	if err != nil {
		t.Fatalf("GetNotifcationListV2: %v", err)
	}
	if len(list) != 5 {
		t.Fatalf("expected 5 notifications for zzz, got %d", len(list))
	}
	if total != 5 {
		t.Fatalf("expected total 5, got %d", total)
	}
	for _, n := range list {
		if len(n.FromPinId) != 6 || n.FromPinId[:5] != "mypin" {
			t.Fatalf("unexpected fromPinId leaked into result: %q", n.FromPinId)
		}
	}

	// lastId 过滤：只返回 id > lastId 的通知
	list, total, err = db.GetNotifcationListV2("zzz", 2002, 20)
	if err != nil {
		t.Fatalf("GetNotifcationListV2 lastId: %v", err)
	}
	if len(list) != 2 || total != 2 {
		t.Fatalf("expected 2 notifications newer than lastId 2002, got len=%d total=%d", len(list), total)
	}
}

// TestCleanUpNotifcationV2KeepsNewest 回归：清理按"最新优先保留"（迭代为时间倒序）。
func TestCleanUpNotifcationV2KeepsNewest(t *testing.T) {
	db := newNotifcationTestDB(t)

	payload := []byte(`{}`)
	// 4 条通知、4 个不同 fromPinId，id 递增=时间递增
	for i := 0; i < 4; i++ {
		if err := db.SetNotifcationV2("addr", int64(5000+i), "pin"+string(rune('a'+i)), payload); err != nil {
			t.Fatalf("SetNotifcationV2: %v", err)
		}
	}

	if err := db.CleanUpNotifcationV2("addr", 2); err != nil {
		t.Fatalf("CleanUpNotifcationV2: %v", err)
	}

	_, total, err := db.GetNotifcationListV2("addr", 0, 50)
	if err != nil {
		t.Fatalf("GetNotifcationListV2 after cleanup: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected newest 2 kept, got total=%d", total)
	}
}
