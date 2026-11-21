package p2p

import (
	"log"
	"sync/atomic"
)

// R3/ROB-5：发布失败可观测性。发布失败 = 全网收不到该 pin，此前调用方
// 完全吞错无任何告警。首个失败 + 每 100 次失败打一条 WARN，附累计计数。
var publishFailures atomic.Int64

// LogPublishError 调用方在 PublishPin 返回错误时调用；采样告警不刷日志。
func LogPublishError(pinId string, err error) {
	total := publishFailures.Add(1)
	if total == 1 || total%100 == 0 {
		log.Printf("[P2P] PublishPin failures total=%d (sample pinId=%s): %v", total, pinId, err)
	}
}
