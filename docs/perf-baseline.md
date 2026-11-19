# man-p2p 性能基准报告

- 基准时间：2026-11-20
- 基准代码版本：`5d655d9`（main，与两台生产节点当前 release 一致）
- 目的：在大规模推广前的性能优化迭代开始前，固化基础业务接口的延迟 / 吞吐 / 资源占用基准，用于优化后对比复测。
- 配套文档：[perf-optimization-plan.md](./perf-optimization-plan.md)（问题清单与迭代建议）

---

## 1. 方法与工具

| 项 | 内容 |
|---|---|
| 负载工具 | `benchget`（本次编写的 Go 单二进制，固定连接池、定时采样、输出 p50/p90/p99/max/错误数）|
| 工具位置 | `/Users/tusm/Documents/MetaID_Projects/servers/outputs/perf-bench/`（源码 `main.go`、脚本 `run-baseline-node2.sh`、原始结果 `perf-baseline-node2.jsonl`）|
| 负载测试目标 | 节点 2 `47.239.239.128` 的 `127.0.0.1:7778`（p2p-only follower，不经 nginx、不对公网服务，压测不影响公网 API）|
| 公网采样 | `https://manapi.metaid.io`（nginx → 节点 1 `8.217.14.206:7777`），串行小样本（≤8 次/端点，间隔 0.3s），不构成负载 |
| 测量时长 | 每端点每并发档：2s 预热 + 10s 测量；并发档 c=1 / c=4 / c=8 |
| 病态用例 | 单发 curl（`--max-time` 30~90s），不复压 |

基准参数样本（节点 2 真实数据）：

- pin id：`4e8f735aaf1b142a100b0da593bc68bcbe3c762d26b8a3f751f67dacd7ce461bi0`（text/plain，含 content body）
- address：`15xNw6NDBuZn2hvVgauXgX498dbfZk1fqa`
- metaid：`876c1bd3bb909c1eab77e1b6b5ea780e758da7c1a384880c1c492c70ebc45c45`
- path：`/protocols/metabot-heartbeat`（约 2.6 万 pins 的协议池路径）

## 2. 环境快照

### 节点 2（负载测试机）

| 项 | 值 |
|---|---|
| 规格 | 2 vCPU / 14 GB 内存（同机还有其它 MVC/MRC20 业务）|
| 压测前负载 | load average 0.03（空闲）|
| man-p2p RSS | **12.4 GB**（无 GOMEMLIMIT / MemoryMax，见计划文档基础设施节）|
| 数据规模 | pin=10,443,111 / block=5,053,892 / metaId=35,288 |
| Pebble 总量 | ~40 GB，其中 **mempool 子库 19 GB**（本应为临时数据，见发现 1）|
| 运行模式 | `p2p_sync_mode=full`，chain source 关闭（`/api/mrc20/*` 与 `/api/tx/status` 在该模式不注册路由，返回 404 属设计行为）|

### 节点 1（公网采样机）

- 4 vCPU（估）/ GOMEMLIMIT=5GiB / MemoryMax=8G，chain-enabled（btc,mvc,doge,opcat），Pebble ~42 GB。

## 3. 负载测试结果（节点 2 直连 127.0.0.1:7778，10s 采样）

单位：RPS = 吞吐；延迟为毫秒。errors=0 除注明外。

| 端点 | c | RPS | p50 | p90 | p99 | max | 备注 |
|---|---|---:|---:|---:|---:|---:|---|
| GET /health（对照） | 1 | 6949 | 0.1 | 0.2 | 0.4 | 8.1 | |
| GET /health（对照） | 8 | 13029 | 0.4 | 1.2 | 3.7 | 21.3 | 2 核裸吞吐上限 ~13k RPS |
| GET /api/pin/:id（全量含正文） | 1 | 3987 | 0.2 | 0.3 | 0.6 | 11.7 | 点查 O(1) |
| 同上 | 4 | 5789 | 0.5 | 1.2 | 4.3 | 37.7 | |
| GET /api/pin/:id?meta=1 | 1 | 4493 | 0.2 | 0.3 | 0.5 | 12.9 | 轻量模式更省带宽 |
| 同上 | 4 | 7202 | 0.4 | 1.0 | 3.7 | 22.3 | |
| GET /api/pin/list?page=1&size=20 | 1 | 270 | 3.0 | 5.4 | 9.8 | 18.9 | 单次 3ms（PinSort 倒序 offset 扫描）|
| 同上 | 4 | 309 | 9.9 | 22.8 | 66.9 | 235.1 | c4 即饱和（~310 RPS 封顶）|
| 同上 | 8 | 310 | 16.1 | 54.6 | 155.6 | 328.6 | 加并发不加吞吐 = CPU 瓶颈在每请求重扫+排序 |
| GET /api/metaid/list?page=1&size=20 | 1 | 2701 | 0.3 | 0.5 | 0.8 | 9.8 | 首页快（offset 从头数）|
| 同上 | 4 | 3640 | 0.7 | 2.0 | 7.2 | 48.0 | |
| GET /api/pin/path/list?size=200 | 1 | 17.7 | 48.2 | 60.4 | 88.8 | 778.1 | 200 行协议池回放 ~48ms/请求 |
| 同上 | 4 | 22.2 | 165.0 | 261.9 | 371.6 | 516.1 | **封顶 ~22 RPS**（IDBots MetaTask 主路径）|
| GET /api/pin/path/list?size=20 | 1 | 534 | 1.6 | 2.5 | 4.3 | 16.6 | size 小则快 |
| GET /api/address/pin/list/:a | 1 | 315 | 2.7 | 4.5 | 8.2 | 21.9 | |
| 同上 | 4 | 354 | 9.3 | 20.9 | 38.8 | 90.7 | |
| GET /api/info/address/:a | 1 | 5669 | 0.2 | 0.2 | 0.4 | 11.0 | 点查 + 最多 6 次 size=1 回填 |
| 同上 | 4 | 9625 | 0.3 | 0.7 | 2.9 | 23.6 | |
| GET /api/info/metaid/:m | 1 | 5952 | 0.1 | 0.2 | 0.4 | 9.5 | |
| GET /api/notifcation/list | 1 | 4770 | 0.2 | 0.2 | 0.5 | 13.3 | 节点 2 通知库近乎空；全库扫描问题见计划文档 |
| GET /api/mempool/list?page=1&size=20 | 1 | 0.2 | 8005 | 8005 | 8005 | 8006 | **触发 8s 客户端超时（2/2 errors）** |
| GET /debug/count | 1 | 6601 | 0.1 | 0.2 | 0.4 | 7.6 | |
| GET /api/p2p/status | 1 | 6342 | 0.1 | 0.2 | 0.4 | 12.8 | |
| GET /api/protocol/list | 1 | 6876 | 0.1 | 0.2 | 0.4 | 10.7 | 命中 SWR 快照缓存 |
| GET /（HTML 首页） | 1 | 115 | 7.3 | 12.7 | 20.8 | 28.7 | |
| GET /pin/:id（HTML） | 1 | 2809 | 0.3 | 0.4 | 0.8 | 7.7 | |
| GET /content/:id | 1 | 5243 | 0.2 | 0.2 | 0.4 | 10.0 | |

## 4. 公网轻采样（nginx → 节点 1，串行小样本，单位秒）

| 端点 | 样本序列（time_total） |
|---|---|
| /health ×5 | 0.107 0.050 0.052 0.077 0.084 |
| /api/pin/:id ×8 | 0.137 0.045 0.049 0.074 0.150 0.446 0.057 0.066 |
| /api/pin/:id?meta=1 ×8 | 0.138 0.136 0.057 0.054 0.054 0.141 0.147 0.050 |
| /api/pin/list p1 ×8 | 0.076 0.086 0.085 0.149 0.166 0.069 0.068 0.077 |
| /api/pin/path/list size=200 ×8 | 0.342 0.182 0.202 0.207 0.189 0.197 0.203 0.325 |
| /api/info/address ×8 | 0.119 0.134 0.066 0.047 0.063 0.071 0.090 0.062 |
| /api/mrc20/address/balance/:a（单发） | 0.133（当前 UTXO 规模下尚快；全库扫描读径，增长后劣化）|
| /api/mrc20/tick/AddressBalance（单发） | 0.055 |
| /mrc20/holders/1/1（单发） | 0.073（响应 9.6KB）|
| /api/mrc20/tick/all?size=50（单发） | 0.065 |
| /api/tx/status（单发） | 0.520 |

## 5. 病态用例（单发）

| 用例 | 节点 2 | 公网（节点 1 + nginx）|
|---|---|---|
| `/api/mempool/list?page=1&size=20` | **>90s 仍未返回**（curl 90s 超时）| **60s 后 nginx 504**（后端仍在跑，占满一个 worker）|
| `/api/pin/list?page=1000&size=20` | 0.24s | — |
| `/api/pin/list?page=10000&size=20` | **5.63s** | **7.54s** |
| `/api/pin/list?page=1&size=500`（无钳制） | 0.03s / 404KB 响应 | — |
| `/api/metaid/list?page=300&size=100` | 0.03s | — |
| `/api/notifcation/list`（不存在的地址） | 0.0004s | — |

## 6. 基准结论（按严重度）

1. **P0｜mempool/list 不可用**：mempool 子库膨胀至 19 GB（临时数据未清理），读路径全表扫描 + 逐条跨库回查，单请求 >90s，公网必 504；并发几条即可拖死节点。属性能+资源+DoS 三重问题。
2. **P0｜pin/list 深分页**：offset 逐条回退扫描，page=10000 单请求 5.6~7.5s；响应里的 `lastId` 参数后端根本没实现（传了也无效）。`size` 无上限钳制。
3. **P1｜path/list 200 行协议池回放**：~48ms/请求、22 RPS 封顶（节点 2），是 IDBots MetaTask 高频路径；公网叠加 nginx 后 180~340ms。
4. **P1｜列表端点并发上限低**：pin/list 每请求做"全页扫描 + sort.Slice 重排序"，c4 即饱和（310 RPS @2 核）；公网大规模推广后这将是首个到达的容量墙。
5. **P1｜MRC20 全库扫描族**：balance / holders / tick-address 当前数据量下尚快（0.05~0.13s），但读路径是 O(全库 UTXO)~O(持有者×全库 UTXO)，MRC20 增长后必然劣化（代码证据见计划文档）。
6. **良好面**：点查类（pin by id、info、content、p2p status、protocol list 缓存命中）在 0.1~0.3ms、数千 RPS，结构健康；`?meta=1` 轻量模式有效。
7. **资源面**：节点 2 RSS 12.4GB / 14GB 无上限护栏；mempool 库 19GB 无生命周期治理；两节点均未配置连接级限流（nginx 层也无限流配置记录）。

## 7. 复测指引（优化后按同口径对比）

```bash
# 1) 工具与脚本（servers/outputs/perf-bench/）
scp benchget-linux run-baseline-node2.sh root@47.239.239.128:/tmp/
ssh root@47.239.239.128 '/tmp/run-baseline-node2.sh'   # 结果写 /tmp/perf-baseline-node2.jsonl

# 2) 病态用例单发（同第 5 节命令）
# 3) 公网轻采样（同第 4 节 sample 函数）
```

对比口径：同节点、同样本参数、同并发档；重点看 mempool/list 恢复到 <100ms、pin/list p9999 深页 <100ms、path/list 200 行 p50 <10ms、列表端点 c4 吞吐提升倍数。
