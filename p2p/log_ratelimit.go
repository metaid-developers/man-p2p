package p2p

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// 坏消息日志限流（R3/ROB-6）：gossip/presence topic 公开，任何节点可持续
// 发垃圾消息刷日志。按来源聚合计数，每来源每窗口只打一条汇总。
const (
	badMsgWindow      = time.Minute
	badMsgLogInterval = 10 * time.Second
)

type badMsgCounter struct {
	counted  int64
	firstAt  time.Time
	lastWarn time.Time
}

var badMsgMu sync.Mutex
var badMsgStats = map[string]*badMsgCounter{}

// logBadGossipMessage 记录一次坏消息；同来源在 logInterval 内只打一条汇总日志。
func logBadGossipMessage(from interface{}, err error) {
	badMsgMu.Lock()
	defer badMsgMu.Unlock()

	key, ok := from.(string)
	if !ok {
		key = fmt.Sprintf("%v", from)
	}
	now := time.Now()
	c := badMsgStats[key]
	if c == nil {
		c = &badMsgCounter{}
		badMsgStats[key] = c
	}
	c.counted++

	if c.lastWarn.IsZero() || now.Sub(c.lastWarn) >= badMsgLogInterval {
		log.Printf("gossip: bad messages from %s: total=%d (sample: %v)", key, c.counted, err)
		c.lastWarn = now
		c.counted = 0
	}

	// 窗口清理：超过窗口未活跃的来源移除，防 map 无限增长
	if len(badMsgStats) > 1024 {
		for k, v := range badMsgStats {
			if now.Sub(v.lastWarn) > badMsgWindow {
				delete(badMsgStats, k)
			}
		}
	}
}
