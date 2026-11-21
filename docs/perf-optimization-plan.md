# man-p2p 性能优化与安全加固计划

- 时间：2026-11-20 ｜ 基准版本 `5d655d9`
- 配套基准数据：[perf-baseline.md](./perf-baseline.md)
- 来源：三路代码审计（读路径 / p2p 包 / 安全面）+ 生产实测。所有问题均有代码位置证据；行号为 `5d655d9` 时点。
- 排序原则：**SEC = 上线前必须修的安全项；PERF = 有实测或明确劣化曲线的性能项；ROB = 健壮性/资源；INFRA = 部署面护栏**。

## 执行状态（随批次更新）

- **R0 安全止血：已发布生产（2026-11-20，`79c7bb3`，双节点）。** SEC-1/2/4/10 + SEC-3 止血（export 限流+并发闸门+maxPins 5000，保留公开不破验收）全部落地；adminToken 已配置强随机值（节点本地 `.admin-token`）。SEC-5 依赖的 PERF-1/2/3 见 R1。
- **R1 性能止血：已发布生产（2026-11-21，双节点 `2a462b1`，含存量清理）。** PERF-1/3/4/5 全落地；mempool/list 8005ms→2.7ms、pin/list 深页游标化、path/list 3.8 倍；复测对比见 perf-baseline.md §8。遗留：节点 1 mempool 库 ~1955 万条非空坏数据（不碍读路径，可选清理）；SEC-5 的 nginx 限流仍未部署（INFRA-1）。
- **R2 MRC20 读路径定向索引：代码完成（分支 `perf/optimize-r2`），待发布。** 调查确认 `mrc20_in_{addr}_{tickId}_{txPoint}` 索引在全部写路径维护（仅 From 副本两处缺口），读路径切换无需回填：
  - 余额热路径逐条日志删除（a87dc03）
  - From 副本两处缺口修复（UpdateMrc20Utxo Spent 删 From / MarkUtxoAsSpent 刷 From）
  - 余额/holders/流水三条读路径从 `mrc20_utxo_` 全库扫描切到 `mrc20_in_` 定向前缀（holders 余额口径=Balance 字段跨链求和，等价复刻）
  - CheckOperationtx(All)：SaveMrc20Pin 写 `mrc20_op_tx_{txId}_{txPoint}` 前缀索引，读侧定向命中 + 存量无索引时回退全扫（语义与旧对硬删行不可见一致）
  - 调查中发现但**不在 R2 范围、仅上报**的存量 bug：`deleteBlockTransactions` 扫 `mrc20_tx_` 前缀而流水主键是 `tx_`（reindex.go:269，恒空）；`ReindexFromHeight` Step5 扫 `tx_{chain}_` 同样匹配不到（reindex.go:697）；teleport 硬删路径不写 block_spent_（不可回滚）。
- **R3 p2p 健壮性：代码完成（分支 `perf/optimize-r3`），待发布。**
  - ROB-2 panic 隔离：gossip/presence 接收循环、broadcast、storage monitor、bootstrap 循环、sync stream handler 全部 recover（4309329）
  - ROB-3 stream 硬化：sync handler 30s deadline + GetPinFn nil 守卫；FetchPin 30s 超时 + 32MB 解码上限；坏消息日志按来源限流（4309329）
  - ROB-1 gossip 流水线：接收与处理解耦——4 worker 池 + 64 有界队列（队列满丢弃告警）+ pinId 去重缓存（5min TTL/10k 上限）+ HandleIncomingAnnouncement 返回 error 驱动有界退避重试（2 次退避 2s/8s）（58414be）
  - ROB-4 bootstrap：尊重 libp2p 退避（不再每 2s 硬拨+清除退避）、去掉内层 15×2s 双层重试、失败日志每分钟每节点一条（97e0be7）
  - ROB-5 发布可观测：PublishPin 两处吞错点接入 LogPublishError（首败+每 100 次告警）（97e0be7）
  - 测试：去重缓存/错误返回驱动重试/重启对端重连（等待窗放宽到退避量级）回归；全量失败集与基线一致
- R4（结构优化）：未开始。

---

## 一、SEC —— 安全（大规模推广前必须处理）

### SEC-1｜P0｜admin 鉴权逻辑"配置为空即放行"，当前 `adminToken=""` → 11 个毁灭性管理端点对外完全开放
- 证据：`api/mrc20.go:414,592,647,729,793,922,961,1013,1047,1096,1147`（同一模式 `Config.AdminToken != "" && token != ...`）；`config.example.toml:8`、生产本地 `config.toml` 均 `adminToken = ""`。
- 后果：匿名可调用 `index-height/set`（回滚索引高度）、`reindex-from`（清 UTXO）、`snapshot/restore`、`fix-pending` 等。
- 修复：①鉴权改为"token 必须非空且匹配"；②启动时若挂载 admin 路由而 AdminToken 为空/弱则拒绝启动；③token 走 `X-Admin-Token` header + `crypto/subtle.ConstantTimeCompare`；④admin 路由默认仅绑定内网/独立管理端口。
- 验证：空 token 启动报错；`/api/mrc20/admin/*` 匿名全 401；带错 token 延迟恒定。

### SEC-2｜P0｜快照删除端点路径遍历 + `os.RemoveAll` → 一条匿名请求可删除整个数据目录
- 证据：`api/mrc20.go:1142-1173`（DELETE `/api/mrc20/admin/snapshot/:id`）→ `man/mrc20_snapshot.go:365-374` `DeleteSnapshot`：`filepath.Join(snapshotDir, snapshotID)` 后直接 `os.RemoveAll`，`snapshotID` 无校验；配合 SEC-1 即 `DELETE /api/mrc20/admin/snapshot/..` = 删库。
- 修复：snapshotID 白名单 `[0-9a-zA-Z_-]`；Join 后校验 `HasPrefix(clean, snapshotDir+sep)`；即使鉴权正确也保留该校验（纵深防御）。

### SEC-3｜P0｜`/api/export/user-data` 无鉴权：任何人可导出任意地址全量 pin 内容，并触发全库扫描
- 证据：`api/export_handler.go:17-20`（GET+POST 无 token）；`export/query.go:34-51`（按 globalMetaId 解析时全表扫 `MetaidInfoDB`）；`export/export.go:10` `maxPins=500000`。
- 后果：隐私泄露（他人地址全部内容可被拉走）+ DoS（SOP 发布后验收即依赖此端点公网可用）。
- 修复：加鉴权（地址签名 challenge 或限内网/token）；identity 解析走索引；`maxPins` 降级；限流。
- 验证：匿名导出返回 401/403；合法样例仍 `200 application/zip`（保住 SOP 验收口径）。

### SEC-4｜P0｜`POST /api/config/reload` 无鉴权
- 证据：`api/p2p_api.go:12,18-24` → `p2p/config.go:37-61` 热载 BootstrapNodes/ListenPort/EnableRelay。
- 后果：匿名反复触发 reload（DoS）；配合任意写文件可劫持节点 bootstrap（日蚀攻击面）。
- 修复：并入 SEC-1 的强制鉴权管理面。

### SEC-5｜P0｜公开 DoS 放大端点（与 PERF-2/3 同根因，安全视角单独列）
- 证据：`man/mrc20_balance_utxo.go:29-39,174-231,237-254`（余额=全库 UTXO 扫描×tick 数平方放大，热路径逐条打日志）；`man/mrc20_pebble.go:1052-1123`（holders=持有者数×全库扫描）；`man/mrc20_pebble.go:912-1043`（tick/address=全库扫描+内存全量排序）；`pebblestore/data.go:298-393`（mempool/list 全表扫描，实测 >90s）；`pebblestore/data.go:456-530`（notifcation 从库头扫到目标前缀）。
- 后果：当前即可用 `/api/mempool/list` 一条请求占死 worker 60s+（公网已实测 504）；并发数条 = 服务不可用。
- 修复：见 PERF-1/2/3；短期先上 nginx 限流 + 端点级并发闸门（见 INFRA-1）。

### SEC-6｜P1｜`/api/block/file` chain 参数路径遍历
- 证据：`api/btc_jsonapi.go:474-511`（`chain` 直接拼文件名）→ `man/blockfile.go:164-174`。`?chain=../../..` 可越出 blockFiles 目录读同构文件（后缀限制缩小了面，但遍历原语成立）。
- 修复：chain 白名单（btc/mvc/doge/opcat）；Join 后前缀校验。同函数 `from/to` 无上限（`api/btc_jsonapi.go:552-589`）需钳制。

### SEC-7｜P1｜CORS 危险组合 + token 走 URL query + 非常量时间比较
- 证据：`api/webapi.go:67-82`（`Allow-Origin: *` + `Allow-Credentials: true` 违反规范）、`:107-109`（全局 cors AllowOrigins=["*"] 覆盖 admin）；token 全部在 query（进入 nginx 日志/Referer）；15 处 `token != AdminToken` 非常量时间比较。
- 修复：去掉 credentials 或收紧 origin；token 改 header；subtle.ConstantTimeCompare。

### SEC-8｜P1｜无鉴权诊断端点泄露任意地址持仓/流水
- 证据：`api/mrc20.go:67-69,475-574`（debug/pending-in、debug/utxo-status）、`api/mrc20_admin_v2.go:14-30`（teleport/v2/list limit 无上限）、`:59-85`（verify/all）、`api/mrc20.go:851-907`（verify-balance 自述"不严格认证"）；另有 `/debug/count`、`/debug/sync`（`api/webapi.go:130-131`）、swagger（`:111`）、`/api/p2p/*`（`api/p2p_api.go`）暴露内部拓扑。
- 修复：全部并入强制鉴权管理面或限内网；`limit` 钳制；presence/p2p 状态去掉敏感细节。

### SEC-9｜P1｜Git 追踪文件含明文 RPC 凭据
- 证据：`config_dev_main.toml:25-26,52-53`、`config_regtest.toml:7,41-42`（adminToken="metaid" 字典词）、`config_doge.toml`；本地 config.toml 亦含真实凭据。
- 修复：轮换全部泄露凭据；配置改环境变量注入；评估是否需要历史清除（内部仓可只做轮换）。

### SEC-10｜P1｜无请求体大小限制
- 证据：全仓无 `http.MaxBytesReader`；`api/export_handler.go:47`、`api/mrc20.go:974` `ShouldBindJSON` 任意大小 body 进内存。`api/server.go` 已有 header/read/write timeout（良好），仅缺 body 上限。
- 修复：全局中间件包 `http.MaxBytesReader`（如 1MB，导出/快照端点单独放宽）。

### SEC-11｜P2
- GET 触发破坏性写操作（`api/mrc20.go:36-39` index-height/set、reindex-*）→ 改 POST。
- `api/webapi.go:55-58` `popStrShow` 短字符串切片 panic（模板 500）。
- `/content/:number`（text/plain 回传链上内容）补 `X-Content-Type-Options: nosniff`。
- 慢请求日志（`api/middleware.go:29`）无采样，配合慢端点可刷爆磁盘。
- pprof 已有 localhost 守卫（`api/debug_server.go:12-44`，实现良好）；上线 checklist 确保不导出 `MAN_P2P_PPROF_ADDR` 即可。

---

## 二、PERF —— 性能

### PERF-1｜P0｜mempool 子系统：读路径全表扫描 + 数据无限膨胀（实测最严重）
- 实测：mempool/list 单请求 >90s（节点 2）、公网 504；mempool Pebble 库 **19GB**（应为临时数据）。
- 读路径证据：`pebblestore/data.go:298-393` `GetMempoolPageList` 对 mempool 库 `NewIter(nil)` 全量迭代，每条再跨库 `db.GetPinByKey` + unmarshal，堆 + sort——与 page 大小无关，单请求 O(mempool 总量)。
- 膨胀证据：mempool pin 的清理路径只覆盖确认场景（`man/mempool.go` / DeleteMempoolData），p2p-only 节点长期累积；节点 2 无链源确认清理，19GB 即历史堆积。
- 修复：①`SetMempool` 已存完整 pin JSON（`pebblestore/data.go:225-234`），列表直接用 mempool 值，去掉二次跨库 Get；②建 seenTime 排序索引，limit 下推迭代器；③**mempool 生命周期治理**：TTL（如 72h）+ 确认/被替换清理 + 容量上限（LRU），节点 2 需一次性清洗存量（离线 compaction/重建）；④短期先在 nginx 拒绝或限流该路径。
- 验证：mempool/list p50 <100ms；mempool 库体积回落并稳定在 MB~低 GB 级。

### PERF-2｜P0｜MRC20 读路径族：全库 UTXO 扫描（数据增长后平方级劣化）
- 证据（均在 MrcDb 上无索引直扫，无缓存）：
  - `man/mrc20_balance_utxo.go:29-37` `CalculateBalanceFromUTXO` 全量扫 `mrc20_utxo_`，热路径逐条 log（:56,62,70,84）；
  - `:174-231` `GetAddressBalances` 全扫收集 tick，再对每 tick 重复全扫（O(T×U)）；`:237-254` 三链循环再乘 3；
  - `man/mrc20_pebble.go:1052-1123` `GetMrc20Holders`：全扫收集地址后对每地址再触发上述全链余额计算（O(H×chains×T×U)）；
  - `man/mrc20_pebble.go:912-1043` `GetMrc20AddressHistoryWithDirection` 全库扫 + 内存全量排序取一页；
  - `man/mrc20_pebble.go:572-606,632-670` `CheckOperationtx(All)` 索引 miss 时全库扫描（且在写路径被调用，拖慢出块）。
- 当前实测尚快（0.05~0.13s）是因为 UTXO 规模还小；MRC20 一旦推广，这 5 类端点（balance×2、holders、tick/address、tx/history 回退）最先挂。
- 修复：①建 `mrc20_in_{addr}_{tick}` 定向索引（`GetMrc20ByAddressAndTick` `man/mrc20_pebble.go:674-703` 已有正确范式，余额路径没用它）；②余额读已维护的 `balance_{chain}_{addr}_{tick}` 表（`man/mrc20_new_methods.go:27-52`）替代实时重算，UTXO 写路径同步更新；③holders 改预聚合表（仿 tick name 索引模式）；④tick/address 流水建 per-tick 前缀索引（仿 `tx_tick_`，`mrc20_new_methods.go:164`）；⑤去掉热路径逐条日志；⑥恢复并写入 `mrc20_op_tx_` 索引。
- 验证：单请求不再随全库 UTXO 线性变慢（用合成数据放大 U 后复测时延恒定）；基准中 balance/holders P95 <50ms。

### PERF-3｜P0｜pin/list 深分页：offset 逐条回退 + lastId 参数后端未实现 + size 无钳制
- 实测：page=1 3ms → page=10000 5.6s（节点 2）/ 7.5s（公网）。
- 证据：`pebblestore/store.go:431-462` `QueryPinPageList` `skip := q.Page*q.Size` 从 `it.Last()` 逐条 `Prev` 跳过；`q.LastId` 从未读取（响应却返回 NextId 诱导客户端使用）；`api/btc_jsonapi.go:129-141` size 无钳制（对照：mempool/path 端点已有 `normalize*PageSize` 先例）。
- 修复：①实现 lastId 游标（`SeekLT(lastId)`，正确范式见 `man/indexer_pebble.go:264`）；②size 钳制（沿用 maxPinListPageSize=200）；③保留 page 兼容但深度限幅（如 ≤5000 页）。
- 验证：page=10000 <100ms；size=10^9 返回 ≤200 行；IDBots 兼容性核对（NextId 语义不变）。

### PERF-4｜P0｜notifcation 读+写路径全库扫描
- 证据：`pebblestore/data.go:456-530` `GetNotifcationListV2` `NewIter(nil)` 从库头扫到目标地址前缀（未设 LowerBound），lastId==0 时二遍计数；写路径 `man/notifcation.go:92` 每条通知 spawn goroutine 执行 `CleanUpNotifcationV2`（`data.go:533-581`，同样从库头扫）。
- 修复：①`SeekGE(prefix)` / `IterOptions{LowerBound}`；②total 走 CountDB；③清理改定时批量任务，不再每条通知一个 goroutine。
- 验证：大通知库下不同地址的查询时延一致且 <20ms。

### PERF-5｜P0｜并发正确性：protocolList 对共享缓存切片原地排序（data race）
- 证据：`man/protocol_directory.go:248` 缓存直接 `return protocolDirCache.entries`（无拷贝）；`api/protocol_list.go:44,54` 在锁外对同一底层数组 `sort.SliceStable`（`api/sort.go:697-727`）。
- 后果：两个不同排序参数的并发请求 = data race（race detector 必报），且污染全局缓存顺序。
- 修复：ProtocolDirectory 返回 `slices.Clone`，或按 sortBy 缓存视图。
- 验证：`go test -race ./api ./man` 全绿。

### PERF-6｜P1｜path/list 200 行协议池回放 ~48ms、22 RPS 封顶（IDBots MetaTask 主路径）
- 现状证据：基准 §3；读路径本身是正确范式（`man/indexer_pebble.go:363-447` 游标迭代 + 4096 截断），48ms 主要耗在每行 `BatchGetPinListByKeys`（`pebblestore/store.go:583-637`，每请求百级 goroutine 风暴 + 200 条完整 unmarshal）与每请求重排序。
- 修复：①批量 Get 改单次 multi-get 批量（Pebble 单迭代器批量 read）或合并分片路由；②对 `path+cursor` 结果做短 TTL 进程内缓存（SWR）；③评估 `sort` 参数下推（排序键已有 seenTime）。
- 验证：size=200 p50 <10ms；c4 吞吐 ≥5 倍。

### PERF-7｜P1｜其余 offset 式分页与全量重算
- `pebblestore/store.go:310-331` MetaIdPageList（offset 线性 + 单条坏数据毁整页）→ SeekGE + 跳过坏行。
- `man/mrc20_pebble.go:174-216` GetMrc20TickList（cursor 逐条跳 + `Contains` 过滤混入 name 索引键）→ name 索引换前缀 + SeekGE。
- `man/mrc20_new_methods.go:291-447` 三个 history 分支每请求两遍全前缀扫描（一遍计数一遍数据）+ offset 逐条 Prev + 逐条回查主记录 → total 走计数器，SeekGE 定位。
- `pebblestore/store.go:464-502` GetBlockPageList（每块 split 全量 pinId 串 + ≤100 次批量 Get）→ limit 下推。
- `man/protocol_directory.go:357-399` 冷构建 46-60s 全分片扫描 → 已有 SWR+快照缓解；补"无快照时拒绝同步兜底、只回后台构建中"。

### PERF-8｜P2｜结构与配置卫生
- API 响应序列化走 gin 默认 encoding/json；大响应端点（pin/list、protocol/list、export）换 sonic（与仓内惯例一致）。
- `pebblestore/store.go:56-163`：28 个 Pebble 实例（16 分片+12 单库）、`NoCompression`（放大磁盘/缓存）、共享 128MB cache 引用未 Unref、MaxOpenFiles=64×28；评估合并低频单库、开压缩（zstd）、cache 配额整理。
- `pebblestore/store.go:583-637` BatchGetPinListByKeys 每请求 goroutine 风暴 → 改串行分批或单 goroutine/分片。
- `api/user_info.go:58-70` enrichMetaIdInfo 老账号最多 6 次 size=1 回填/请求 → 结果缓存。
- `man/mrc20_pebble.go:1300,1607-1727` N+1 回查模式（受 limit 限制，可接受，随索引重建一并处理）。

---

## 三、ROB —— p2p 健壮性与资源

### ROB-1｜P0｜gossip 接收循环无背压/无超时/无去重 → 一次对端挂起即可停摆全部 pin 同步，且静默丢 pin
- 证据：`p2p/gossip.go:63-79` 单 goroutine 串行：每条公告同步 `FetchPin`（`p2p/sync.go:50,97`，stream 无 per-request timeout、无 read deadline）+ 同步写库；pubsub 默认缓冲 32 条（`gossip.go:35` 零选项），溢出静默丢；`sync.go:99-104` fetch 失败仅打日志，无重试无补偿；重复公告无去重（重复 FetchPin+写库）。
- 修复：worker pool（有界并发）+ per-request `context.WithTimeout` + `io.LimitReader` 响应上限 + pinId LRU seen-cache + 失败退避重试队列。
- 验证：注入慢 peer（不回数据）时其他 pin 持续同步；同公告重复推送只拉一次。

### ROB-2｜P0｜长驻 goroutine 与 stream handler 无 recover → 单次回调 panic 即进程退出
- 证据：`p2p/gossip.go:63-79`、`p2p/sync.go:34-47`（handler）、`p2p/presence.go:208-257`、`p2p/host.go:179-207`、`p2p/storage.go:18-30`；另有 `p2p/sync.go:41` `GetPinFn` 未判 nil（同函数其余两个回调判了）。
- 修复：各 loop/handler 顶层 `defer recover()+log`；补 nil 判断。
- 验证：注入 panic 的回调，进程存活且记录日志。

### ROB-3｜P1｜FetchPin 响应无大小上限（OOM 面）+ sync handler 无并发上限/无写 deadline（goroutine 泄漏面）
- 证据：`p2p/sync.go:62`（解码无 LimitReader；大小过滤依据对端自报 `ann.SizeBytes`，`:83`）；`:35` 每入站 stream 一 goroutine 无上限；`:45` 写响应无 deadline。
- 修复：LimitReader + 实际大小校验；handler 信号量 + `SetWriteDeadline`。

### ROB-4｜P1｜bootstrap 重连绕过 libp2p backoff + 双层重试叠加 + 日志洪流
- 证据：`p2p/host.go:254,273-282`（每轮清 backoff 硬拨）、`:31,246-268`（内层 15×2s 与外层 2s ticker 叠加）、死节点每 90s 约 45 条失败日志；reload 触发全量重连（`p2p/config.go:57-59`）。
- 修复：去掉 clear-backoff；两层重试合一；失败日志降采样。

### ROB-5｜P1｜发布端吞错误
- 证据：`man/indexer_pebble.go:73`（返回值丢弃）、`man/mempool.go:31`（`_ =`）。发布失败 = 全网收不到该 pin 且无告警。
- 修复：失败计数 + 告警日志 + 重试队列。

### ROB-6｜P2
- 全局 `Node/sub/topic` 无锁读写竞态（`p2p/host.go:319-322` vs `p2p/gossip.go:65`；storage 直接读全局 Node `p2p/storage.go:65-72`，未用已有 `currentNode()`）。
- `p2p/host.go:70` InitHost 失败路径留半初始化 Node（host 泄漏 + 状态误报）。
- presence prune 仅由 Observe 触发（`p2p/presence.go:495`），加低频清扫 ticker；Observe 持写锁全表扫描（`:493-494`）。
- `p2p/gossip.go:33-49` InitGossip 重入泄漏旧 topic/subscription（对齐 presence 的替换语义）。
- 每条坏消息逐条日志（`p2p/gossip.go:74`、`p2p/presence.go:220`）→ 限流。
- app 层 ctx 永不取消、CloseHost 永不调用（`app.go:50-96`）：暂可接受，但信号处理（SIGTERM 优雅退出）值得补。

---

## 四、INFRA —— 部署面护栏（不改代码，发布前可先做）

1. **nginx**：对公网加 `limit_req`（按 IP 令牌桶）+ `client_max_body_size`（如 1m）+ 慢端点专项限流（mempool/list、export/user-data、mrc20 admin 路径直接 deny/内网 allow）；admin 路径在 nginx 层先封一道作为纵深（不能替代 SEC-1 代码修复）。
2. **节点 2 内存护栏**：补 `GOMEMLIMIT=6GiB` + systemd `MemoryHigh=8G`、`MemoryMax=10G`（当前 RSS 12.4G/14G 无护栏，同机还有其它业务；节点 1 已有此配置，照抄口径）。
3. **日志治理**：轮转策略核对（节点 1 写文件、节点 2 走 journald）；慢请求/坏消息日志限流后复查磁盘。
4. **告警**：mempool 库体积、Pebble 目录增长速率、RSS、公网 5xx/504 计数（当前 504 是发现问题的主要信号，太滞后）。

---

## 五、建议迭代批次

| 批次 | 内容 | 出口标准 |
|---|---|---|
| R0 安全止血（先行，可独立发布） | SEC-1/2/3/4 + SEC-10 body 限制 + INFRA-1 nginx 限流 | 匿名 admin/export/reload 全 401；nginx 限流生效；SOP 验收口径不破坏 |
| R1 性能止血（第一性能批） | PERF-1（mempool 读路径 + 存量清洗）+ PERF-3（pin/list 游标+钳制）+ PERF-5（race）+ PERF-4 | mempool/list p50<100ms；pin/list page=10000 <100ms；`-race` 全绿；基准复测提升达标 |
| R2 MRC20 索引重建 | PERF-2 全部 + PERF-7 history 分支 | 合成放大数据下时延恒定；holders/balance P95<50ms |
| R3 p2p 健壮性 | ROB-1/2/3/4/5 | 慢 peer 注入不影响同步；重复公告去重；panic 注入进程存活 |
| R4 结构优化 | PERF-6/7/8 + ROB-6 + INFRA-2/3/4 | 基准全项对比改善；资源护栏生效 |

## 六、验证清单（每批发布前）

1. `CGO_ENABLED=0 go test ./p2p ./ops ./export ./common` 全绿（SOP 口径）+ 全量 `go test ./...` 失败集与干净 main 基线一致（已知 3 个 mrc20_balance 基线失败）。
2. `go test -race ./api ./man ./pebblestore`（R1 起加入）。
3. 按 [perf-baseline.md](./perf-baseline.md) §7 复测，输出对比表归档本文档。
4. SOP 发布流程 + 公网验收（health/peers/export/tx-status 五件套不回归）。
5. 客户端兼容性：pin/list 的 NextId/lastId 语义、path/list 契约、export 行为变化需在 SOP 变更记录标注（参考 f23e8ec 先例）。
