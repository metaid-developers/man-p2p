package p2p

import (
	"log"
	"runtime/debug"
)

// recoverPanic 统一的 panic 防护：长驻 goroutine 与 stream handler 内的
// 任何回调 panic（如 StorePinFn 内部错误）不应杀死整个进程。
// 用法：goroutine/handler 入口 defer recoverPanic("<where>")。
// 注意：对 receiveLoop 类循环，recover 放在循环体外——panic 后该循环退出，
// 由调用方语义决定是否重启（gossip/presence 循环退出即失能，见各自文档）。
func recoverPanic(where string) {
	if r := recover(); r != nil {
		log.Printf("[P2P][PANIC] recovered in %s: %v\n%s", where, r, debug.Stack())
	}
}
