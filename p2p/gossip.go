package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

const TopicName = "metaid-pins"

// R3 流水线常量：接收与处理解耦——旧实现单 goroutine 串行「拉取+写库」，
// 一条慢请求可挂死全部 pin 同步，pubsub 缓冲溢出后静默丢消息（ROB-1）。
const (
	gossipWorkers      = 4
	gossipJobQueueSize = 64
	gossipSeenTTL      = 5 * time.Minute
	gossipSeenMax      = 10000
	gossipMaxRetries   = 2 // 首次处理 + 最多 2 次退避重试
)

type PinAnnouncement struct {
	PinId         string `json:"pinId"`
	Path          string `json:"path"`
	Address       string `json:"address"`
	MetaId        string `json:"metaId"`
	ChainName     string `json:"chainName"`
	Timestamp     int64  `json:"timestamp"`
	GenesisHeight int64  `json:"genesisHeight"`
	Confirmed     bool   `json:"confirmed"`
	SizeBytes     int64  `json:"sizeBytes"`
	PeerID        string `json:"peerId"`
}

var (
	PS    *pubsub.PubSub
	topic *pubsub.Topic
	sub   *pubsub.Subscription

	// 处理流水线：receiveLoop 只做解析+去重+入队，worker 池消费。
	gossipJobs   = make(chan gossipJob, gossipJobQueueSize)
	gossipSeenMu sync.Mutex
	gossipSeen   = map[string]time.Time{}
)

type gossipJob struct {
	ann     PinAnnouncement
	attempt int // 0 = 首次
}

func InitGossip(ctx context.Context) error {
	var err error
	PS, err = pubsub.NewGossipSub(ctx, Node)
	if err != nil {
		return err
	}
	topic, err = PS.Join(TopicName)
	if err != nil {
		return err
	}
	sub, err = topic.Subscribe()
	if err != nil {
		return err
	}
	go receiveLoop(ctx)
	for i := 0; i < gossipWorkers; i++ {
		go func() {
			defer recoverPanic("gossip.worker")
			gossipWorker(ctx)
		}()
	}
	return nil
}

func PublishPin(ctx context.Context, ann PinAnnouncement) error {
	if Node == nil || topic == nil {
		return fmt.Errorf("p2p gossip not initialized")
	}
	ann.PeerID = Node.ID().String()
	data, err := json.Marshal(ann)
	if err != nil {
		return err
	}
	return topic.Publish(ctx, data)
}

func receiveLoop(ctx context.Context) {
	// R3：回调 panic 不再杀死进程。recover 在循环体外：panic 后本循环退出
	//（pubsub 订阅随之失效），进程存活。
	defer recoverPanic("gossip.receiveLoop")
	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			return
		}
		if msg.ReceivedFrom == Node.ID() {
			continue
		}
		var ann PinAnnouncement
		if err := json.Unmarshal(msg.Data, &ann); err != nil {
			logBadGossipMessage(msg.ReceivedFrom.String(), err)
			continue
		}
		// 去重：同 pinId 在 TTL 窗口内只处理一次（pubsub 会向同一节点重复
		// 投递同一条公告，旧实现每次都重新拉取+写库）。重试走独立通道，
		// 不经过去重门（见 gossipWorker）。
		if gossipSeenMark(ann.PinId) {
			gossipEnqueue(gossipJob{ann: ann})
		}
	}
}

func gossipWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-gossipJobs:
			if err := HandleIncomingAnnouncement(ctx, job.ann); err != nil {
				log.Printf("gossip: process %s failed (attempt %d): %v", job.ann.PinId, job.attempt+1, err)
				if job.attempt < gossipMaxRetries {
					// 有界退避重试：2s/8s，队列满则放弃（下次公告或 TTL 过期后再来）
					delay := time.Duration(1<<job.attempt) * 2 * time.Second
					time.AfterFunc(delay, func() {
						gossipEnqueue(gossipJob{ann: job.ann, attempt: job.attempt + 1})
					})
				}
			}
		}
	}
}

// gossipEnqueue 非阻塞入队；队列满说明处理速度低于到达速度，丢弃并告警
// （背压语义：宁可丢弃单条也不阻塞接收循环）。重试（attempt>0）绕过
// gossipSeen 门直达队列。
func gossipEnqueue(job gossipJob) {
	select {
	case gossipJobs <- job:
	default:
		log.Printf("gossip: job queue full, dropping %s (attempt %d)", job.ann.PinId, job.attempt)
	}
}

// gossipSeenMark 标记 pinId 已入队；返回 false 表示 TTL 窗口内已见过（去重）。
func gossipSeenMark(pinId string) bool {
	gossipSeenMu.Lock()
	defer gossipSeenMu.Unlock()
	if _, exists := gossipSeen[pinId]; exists {
		return false
	}
	now := time.Now()
	gossipSeen[pinId] = now
	if len(gossipSeen) > gossipSeenMax {
		// 先按 TTL 清理，仍超限则粗暴裁半（极端洪峰下保内存上限）
		for k, t := range gossipSeen {
			if now.Sub(t) >= gossipSeenTTL {
				delete(gossipSeen, k)
			}
		}
		if len(gossipSeen) > gossipSeenMax {
			half := len(gossipSeen) / 2
			n := 0
			for k := range gossipSeen {
				delete(gossipSeen, k)
				n++
				if n >= half {
					break
				}
			}
		}
	}
	return true
}
