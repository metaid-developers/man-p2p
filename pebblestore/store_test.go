package pebblestore

import (
	"fmt"
	"man-p2p/common"
	"man-p2p/pin"
	"strconv"
	"strings"
	"testing"

	"github.com/cockroachdb/pebble"
)

func TestBatchInsertPins(t *testing.T) {
	// 使用临时目录
	dir := "../data_test"
	idx, err := NewDataBase(dir, 4)
	if err != nil {
		t.Fatalf("NewDataBase err: %v", err)
	}
	defer idx.Close()

	pins := []pin.PinInscription{
		{Id: "txid1", ChainName: "chainA", ContentSummary: "111"},
		{Id: "txid2", ChainName: "chainA", ContentSummary: "222"},
		{Id: "txid3", ChainName: "chainB", ContentSummary: "333"},
	}
	err = idx.BatchInsertPins(pins)
	if err != nil {
		t.Fatalf("BatchInsertPins err: %v", err)
	}

	// 查询主键（自动分片）
	key := BuildPinKey("txid1", 0)
	val, err := idx.GetPinByKey(key)
	if err != nil {
		t.Fatalf("主键查询失败: %v", err)
	}
	t.Logf("主键%s查询结果: %+v", key, string(val))
	if string(val) != "111" {
		t.Fatalf("主键查询内容不符: %+v", string(val))
	}

	// 批量查询主键
	keys := []string{
		BuildPinKey("txid1", 0),
		BuildPinKey("txid2", 1),
		BuildPinKey("txid3", 0),
		BuildPinKey("notfound", 0), // 不存在的key
	}
	vals := idx.BatchGetPinByKeys(keys, false)
	for _, k := range keys {
		if v, ok := vals[k]; ok {
			t.Logf("批量主键查询: %s => %s", k, string(v))
		} else {
			t.Logf("批量主键查询: %s => not found", k)
		}
	}

	// 测试区块交易表写入和读取
	blockKeys := []string{"txid1:0", "txid2:1"}
	err = idx.InsertBlockTxs("100&200chainA", strings.Join(blockKeys, ","))
	if err != nil {
		t.Fatalf("InsertBlockTxs err: %v", err)
	}
	blockKey := common.ConcatBytesOptimized([]string{"chainA", "_block_", strconv.Itoa(1)}, "")
	val, closer, err := idx.BlocksDB.Get([]byte(blockKey))
	if err != nil {
		t.Fatalf("区块交易表查询失败: %v", err)
	}
	blockTxs := SplitBytesOptimized(string(val), "|")
	closer.Close()
	if len(blockTxs) != 2 || blockTxs[0] != "txid1:0" {
		t.Fatalf("区块交易表内容不符: %+v", blockTxs)
	}
	t.Logf("区块交易表内容: %+v", blockTxs)
}
func TestPebbleMerge(t *testing.T) {
	dir := "../data_test"
	idx, err := NewDataBase(dir, 4)
	if err != nil {
		t.Fatalf("NewDataBase err: %v", err)
	}
	defer idx.Close()
	data := make(map[string]string)
	data["a"] = "1"
	//idx.BatchMergeAddressData(data)
	data2 := make(map[string]string)
	data2["a"] = "2"
	//idx.BatchMergeAddressData(data2)
	v, closer, err := idx.AddressDB.Get([]byte("a"))
	defer closer.Close()
	fmt.Println(err, string(v))
}

func TestGetPinByKeyReturnsIndependentBytes(t *testing.T) {
	pinDB, err := pebble.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("pebble.Open err: %v", err)
	}
	t.Cleanup(func() {
		if err := pinDB.Close(); err != nil {
			t.Errorf("pinDB.Close err: %v", err)
		}
	})

	idx := &Database{PinsDBs: []*pebble.DB{pinDB}}
	key := "pin-independent-bytes"
	firstValue := []byte("first pin value")
	secondValue := []byte("second pin value with different bytes")

	if err := pinDB.Set([]byte(key), firstValue, nil); err != nil {
		t.Fatalf("pinDB.Set first err: %v", err)
	}
	firstRead, err := idx.GetPinByKey(key)
	if err != nil {
		t.Fatalf("GetPinByKey first err: %v", err)
	}
	if len(firstRead) == 0 {
		t.Fatal("GetPinByKey first read returned empty value")
	}
	firstRead[0] = 'X'
	reread, err := idx.GetPinByKey(key)
	if err != nil {
		t.Fatalf("GetPinByKey reread err: %v", err)
	}
	if string(reread) != string(firstValue) {
		t.Fatalf("reread after mutating returned slice = %q, want %q", string(reread), string(firstValue))
	}

	if err := pinDB.Set([]byte(key), secondValue, nil); err != nil {
		t.Fatalf("pinDB.Set second err: %v", err)
	}
	secondRead, err := idx.GetPinByKey(key)
	if err != nil {
		t.Fatalf("GetPinByKey second err: %v", err)
	}

	if string(secondRead) != string(secondValue) {
		t.Fatalf("second read = %q, want %q", string(secondRead), string(secondValue))
	}
	if string(firstRead) != "X"+string(firstValue[1:]) {
		t.Fatalf("mutated first read changed after overwrite: got %q, want %q", string(firstRead), "X"+string(firstValue[1:]))
	}
}

func TestGetMempoolReturnsIndependentBytes(t *testing.T) {
	mempoolDB, err := pebble.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("pebble.Open err: %v", err)
	}
	t.Cleanup(func() {
		if err := mempoolDB.Close(); err != nil {
			t.Errorf("mempoolDB.Close err: %v", err)
		}
	})

	idx := &Database{PinsMempoolDb: mempoolDB}
	key := "mempool-independent-bytes"
	firstValue := []byte("first mempool value")
	secondValue := []byte("second mempool value with different bytes")

	if err := mempoolDB.Set([]byte(key), firstValue, nil); err != nil {
		t.Fatalf("mempoolDB.Set first err: %v", err)
	}
	firstRead, err := idx.GetMempool(key)
	if err != nil {
		t.Fatalf("GetMempool first err: %v", err)
	}
	if len(firstRead) == 0 {
		t.Fatal("GetMempool first read returned empty value")
	}
	firstRead[0] = 'X'
	reread, err := idx.GetMempool(key)
	if err != nil {
		t.Fatalf("GetMempool reread err: %v", err)
	}
	if string(reread) != string(firstValue) {
		t.Fatalf("reread after mutating returned slice = %q, want %q", string(reread), string(firstValue))
	}

	if err := mempoolDB.Set([]byte(key), secondValue, nil); err != nil {
		t.Fatalf("mempoolDB.Set second err: %v", err)
	}
	secondRead, err := idx.GetMempool(key)
	if err != nil {
		t.Fatalf("GetMempool second err: %v", err)
	}

	if string(secondRead) != string(secondValue) {
		t.Fatalf("second read = %q, want %q", string(secondRead), string(secondValue))
	}
	if string(firstRead) != "X"+string(firstValue[1:]) {
		t.Fatalf("mutated first read changed after overwrite: got %q, want %q", string(firstRead), "X"+string(firstValue[1:]))
	}
}
