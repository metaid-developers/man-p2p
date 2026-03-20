package man

import (
"testing"
)

func TestGetAllCount(t *testing.T) {
pd := &PebbleData{}
err := pd.Init(16)
if err != nil {
t.Fatalf("Failed to init database: %v", err)
}
defer pd.Database.Close()

result := pd.GetAllCount()

t.Logf("GetAllCount result: Pin=%d, Block=%d, MetaId=%d, App=%d", 
result.Pin, result.Block, result.MetaId, result.App)

if result.Pin == 0 {
t.Error("Expected Pin count > 0, got 0")
}
}

func TestPinPageList(t *testing.T) {
pd := &PebbleData{}
err := pd.Init(16)
if err != nil {
t.Fatalf("Failed to init database: %v", err)
}
defer pd.Database.Close()

list, nextId, err := pd.PinPageList(0, 100, "")
if err != nil {
t.Errorf("PinPageList error: %v", err)
}

t.Logf("PinPageList result: count=%d, nextId=%s", len(list), nextId)

if len(list) > 0 {
t.Logf("First pin: ID=%s, Operation=%s, Path=%s", 
list[0].Id, list[0].Operation, list[0].Path)
} else {
t.Error("Expected at least 1 pin, got 0")
}
}

func TestCountDBDirect(t *testing.T) {
pd := &PebbleData{}
err := pd.Init(16)
if err != nil {
t.Fatalf("Failed to init database: %v", err)
}
defer pd.Database.Close()

keys := []string{"pins", "blocks", "metaids", "pins_confirmed"}
for _, key := range keys {
val, closer, err := pd.Database.CountDB.Get([]byte(key))
if err != nil {
t.Logf("Key '%s': not found (%v)", key, err)
} else {
t.Logf("Key '%s': %s", key, string(val))
closer.Close()
}
}
}
