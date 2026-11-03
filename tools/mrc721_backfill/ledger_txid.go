package main

// mrc721_backfill -fix-ledger-txid 模式:修复实时转移路径按 sha256d 口径写入的
// 当前 UTXO 指针(MVC 账本对 version≥10 新格式交易用重排 txid,sha256d 指针在链上
// 不可解析,下游按指针构建输入报 [-25] Missing inputs)。
//
// 流程(需 -mvc-rpc-*,服务须已停止,pebble 单写者):
//  1. 遍历全部 transferCount>0 且未熔化条目,对其 Output 的 txid 调节点
//     getrawtransaction:可解析即账本口径,通过;
//  2. 不可解析的,从该条目转移历史的高度逐块 getblock(verbosity=2),按
//     raw hex 的 sha256d 反查账本 txid,经 man.Mrc721BackfillRelocateOutpoint
//     改写条目/转移历史键/pin 主记录/别名;
//  3. 断言:全部转移过条目指针非空、值>0 且 txid 可被链解析,违例报错退出。

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/bitcoinsv/bsvd/rpcclient"

	"man-p2p/adapter/microvisionchain"
	"man-p2p/man"
	"man-p2p/pebblestore"
)

type mvcRpc struct {
	client *rpcclient.Client
}

func newMvcRpc(host, user, pass string) *mvcRpc {
	client, err := rpcclient.New(&rpcclient.ConnConfig{
		Host:                 host,
		User:                 user,
		Pass:                 pass,
		HTTPPostMode:         true,
		DisableTLS:           true,
		DisableAutoReconnect: true,
		DisableConnectOnNew:  true,
	}, nil)
	if err != nil {
		log.Fatalf("mvc rpc connect: %v", err)
	}
	return &mvcRpc{client: client}
}

// resolveTxid 账本口径可解析检测:getrawtransaction 对账本 txid 返回 raw,
// 对 sha256d 口径报 No such transaction。
func (r *mvcRpc) resolveTxid(txid string) bool {
	result, err := r.client.RawRequest("getrawtransaction", []json.RawMessage{
		json.RawMessage(`"` + txid + `"`),
		json.RawMessage(`false`),
	})
	return err == nil && string(result) != "null" && string(result) != `""`
}

// blockVerbose2 getblock verbosity=2:账本 txid + raw hex 成对返回。
type blockVerbose2 struct {
	Tx []struct {
		Txid string `json:"txid"`
		Hex  string `json:"hex"`
	} `json:"tx"`
}

// ledgerTxidBySha256d 在指定高度块内按 raw 的 sha256d 反查账本 txid;未命中返回空。
func (r *mvcRpc) ledgerTxidBySha256d(height int64, sha256dHex string) string {
	blockhash, err := r.client.GetBlockHash(height)
	if err != nil {
		log.Printf("getblockhash %d: %v", height, err)
		return ""
	}
	hashParam, err := json.Marshal(blockhash.String())
	if err != nil {
		return ""
	}
	result, err := r.client.RawRequest("getblock", []json.RawMessage{
		hashParam,
		json.RawMessage(`2`),
	})
	if err != nil {
		log.Printf("getblock %s: %v", blockhash, err)
		return ""
	}
	var block blockVerbose2
	if err := json.Unmarshal(result, &block); err != nil {
		log.Printf("parse getblock %s: %v", blockhash, err)
		return ""
	}
	for _, tx := range block.Tx {
		if microvisionchain.GetTxID(tx.Hex) == sha256dHex {
			return tx.Txid
		}
	}
	return ""
}

func (r *mvcRpc) transferHeights(db *pebblestore.Database, itemPinId string) []int64 {
	var heights []int64
	_ = db.IterateMrc721ItemTransfers(itemPinId, func(tr *pebblestore.Mrc721ItemTransfer) bool {
		heights = append(heights, tr.Height)
		return true
	})
	return heights
}

func fixLedgerTxidMode(db *pebblestore.Database, r *mvcRpc) {
	pd := &man.PebbleData{Database: db}

	// 1+2. 扫描与改写
	checked, relocated := 0, 0
	var offenders []string
	_ = db.IterateMrc721Items(func(item *pebblestore.Mrc721Item) bool {
		if item.TransferCount <= 0 || item.Burned || item.Output == "" {
			return true
		}
		checked++
		txid, idx, ok := strings.Cut(item.Output, ":")
		if !ok {
			offenders = append(offenders, fmt.Sprintf("%s (malformed output %q)", item.ItemPinId, item.Output))
			return true
		}
		if r.resolveTxid(txid) {
			return true
		}
		ledgerTxid := ""
		for _, h := range r.transferHeights(db, item.ItemPinId) {
			if cand := r.ledgerTxidBySha256d(h, txid); cand != "" {
				ledgerTxid = cand
				break
			}
		}
		if ledgerTxid == "" {
			offenders = append(offenders, fmt.Sprintf("%s (txid %s unresolvable, no ledger txid found in transfer heights)", item.ItemPinId, txid))
			return true
		}
		newOutpoint := ledgerTxid + ":" + idx
		if !pd.Mrc721BackfillRelocateOutpoint(item.ItemPinId, newOutpoint) {
			offenders = append(offenders, fmt.Sprintf("%s (relocate to %s failed)", item.ItemPinId, newOutpoint))
			return true
		}
		log.Printf("relocated %s: %s -> %s", item.ItemPinId, item.Output, newOutpoint)
		relocated++
		return true
	})

	// 3. 断言:非空 + 值>0(离线)+ txid 可被链解析(RPC),违例报错退出
	missing := pd.Mrc721BackfillAssertOutputs()
	var unresolvable []string
	_ = db.IterateMrc721Items(func(item *pebblestore.Mrc721Item) bool {
		if item.TransferCount <= 0 || item.Burned || item.Output == "" {
			return true
		}
		txid, _, _ := strings.Cut(item.Output, ":")
		if !r.resolveTxid(txid) {
			unresolvable = append(unresolvable, item.ItemPinId)
		}
		return true
	})
	for _, id := range missing {
		log.Printf("assert fail: item %s transferCount>0 but txOutput empty or outputValue=0", id)
	}
	for _, id := range unresolvable {
		log.Printf("assert fail: item %s pointer txid not resolvable by chain", id)
	}
	if len(missing) > 0 || len(unresolvable) > 0 || len(offenders) > 0 {
		for _, o := range offenders {
			log.Printf("offender: %s", o)
		}
		log.Fatalf("ledger txid assertion failed: missing=%d unresolvable=%d offenders=%d", len(missing), len(unresolvable), len(offenders))
	}
	fmt.Printf("checked=%d relocated=%d, ledger txid assertion passed\n", checked, relocated)
}
