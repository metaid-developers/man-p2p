# IDBots MRC-721 V1.1 实施计划:原子交换交易(PSBT 语义)

> 状态:实施计划(v1,2026-11-02)
> 上游:`docs/IDBOTS_MRC721_REQUIREMENTS.md` §5.1(模型 C 定稿)/ R4(交易流程)/ R6(工具清单)
> 前置:V1 已合并 IDBots main(`10a2ac5c`);man-p2p 生产 `d7422de`(`/api/mrc721/*` 含 camelCase 别名)
> 本文是给 IDBots 开发 Agent 的实施输入,分支建议:`feat/mrc721-atomic-swap`(基于当时 main)

## 1. 目标与非目标

**目标**:MetaBot 会话内完成 MRC-721 的挂单、购买、查单——**一笔 MVC 交易同时完成买家付款与 NFT 过户**,无订单托管、无链下信任链。

**非目标(本期不做)**:云端撮合目录(挂单凭证链下传递);royalty 分账输出(仅展示);链上挂单过期(nLockTime);BTC 侧原子交换(真 PSBT,后续单独评估);卖家离线代售/平台托管场景(订单托管式仅在需要时另立项)。

## 2. 交易模型(核心)

一笔交易,五类构件:

```
输入0: NFT outpoint(卖家签名)          —— 铁律:必须输入 0
输入1+: 买家付款 UTXO(买家签名,买家钱包选择)
输出0: NFT → buyerAddress               —— 铁律:必须输出 0
输出1: priceSats → sellerAddress
输出2+: 买家找零(买家钱包生成)
手续费: 买家承担(从买家输入中扣)
```

- 铁律依据:man-p2p 索引器 `GetOWnerAddress` 按输入 0 → 输出 0 解析新归属(与 V1 `mrc721_transfer` 同一条已验证规则)。
- 原子性:交易要么整笔确认(钱/NFT 同时换手),要么整笔失败(买家可在确认前花掉付款输入,整笔作废,零损失)。
- 多挂单竞争:同一 NFT 的多张挂单,先广播先成交;其余因输入已被花而全部失效。
- 挂单失效:卖家挂单后自行转走 NFT → 买前校验(§4)拒绝,凭证自然作废。

## 3. 挂单凭证(listing tuple)V1 契约

```jsonc
{
  "version": 1,
  "kind": "mrc721-atomic-swap",
  "itemPinId": "<64hex>i<n>",          // 冗余展示用;真值以 partialTx 输入0 为准
  "collectionPinId": "...",
  "outpoint": { "txid": "<64hex>", "vout": 0, "satoshis": 546 },
  "sellerAddress": "...",
  "buyerAddress": "...",                // 挂单时指定买家;泛化"任意买家"留后续版本
  "priceSats": 22000,                   // ≥ 22000(对齐 NftManager 惯例下限)
  "partialTx": "<mvc partial tx hex>",  // TxComposer 序列化,仅输入0已解锁
  "feeRate": 2,                         // 记录卖方建议费率,实际以买家构建为准
  "createdAt": 1700000000,
  "expiresAt": 1700086400               // 链下约定,仅展示与本地过滤
}
```

传递方式:凭证即消息(会话里直接贴给对方 MetaBot / 点对点文本);卖家本地 sqlite 记录自己发出的挂单(`mrc721_sell_orders` 列出并逐张复核)。云端目录非本期。

## 4. 买前硬校验(必须全部通过才可签)

1. 解析 `partialTx`:输入 0 == listing.outpoint;输出 0 → buyerAddress;存在输出 == priceSats → sellerAddress;无多余对卖家有利的改动(全量输出审计)。
2. 输入 0 解锁脚本验签通过(卖家签名有效)。
3. 索引器 `item/info?pinId=`:当前 owner == sellerAddress 且 `output` == outpoint 且 `burned != true`。
4. 节点未花校验:outpoint 未被花费(复用 mvcSpend 的 UTXO/花费记录机制)。
5. 校验失败必须给教学性错误(沿用 V1 `transferMrc721Item` 的 "unindexed item" 提示模式),明确指出哪条不满足。

广播前**再做一轮** 3-4(防挂单校验与广播之间状态变化)。

## 5. 新增组件(IDBots 侧)

| 组件 | 位置 | 职责 |
|---|---|---|
| `buildMvc721SellPartialTxWorker` | `src/main/libs/`(新文件,pattern 同 `buildMvcNftTradeRawTxWorker`) | 卖家侧:`TxComposer` 组装(输入0=NFT outpoint;输出0/1 按 §2)→ 仅 `unlockP2PKHInput(0)` → 序列化 partialTx;**不得**触碰钱包其它 UTXO |
| `completeMvc721BuyTxWorker` | `src/main/libs/`(新文件) | 买家侧:解析 partialTx → 校验(§4 的 1-2)→ `pickUtxo` 选付款输入覆盖 price+fee → 加找零输出 → 签全部买家输入 → compose → 广播(wallet-api v3,同 V1 通道)→ 返回 txid + spentOutpoints(纳入 mvcSpend 会话记录) |
| `verifyMrc721Listing` | `mrc721Service.ts`(导出,供 buy 与 sell_orders 共用) | §4 全量校验;索引器读取走既有 `fetchMrc721*` |
| service 编排 | `mrc721Service.ts` | `sellMrc721Item({itemPinId, buyerAddress, priceSats, feeRate})`(内部先 `fetchMrc721ItemInfo` 取 outpoint 并校验归属/burned)、`buyMrc721Listing({listing})`、`recordMrc721Listing`/`listMrc721MySellOrders`(sqlite 存取 + 逐张复核) |
| agent tools ×3 | `mrc721AgentTools.ts` | `mrc721_sell`(参数 itemPinId/buyer/priceSats;返回凭证全文+提示"把凭证交给买家")、`mrc721_buy`(参数 listing 凭证 JSON;返回 txid+交割确认指引)、`mrc721_sell_orders`(无参列本地挂单并复核,标注有效/失效原因) |
| RPC 路由 ×3 | `metaidRpcServer.ts` | 命名对齐既有风格:`/api/idbots/wallet/mvc/build-mrc721-sell-partial-tx`、`.../complete-mrc721-buy-tx`、`.../verify-mrc721-listing`(POST,JSON body,鉴权同现有 wallet 路由) |
| sqlite | `serviceOrderStore` 旁新建轻量 store 或复用 kv | 仅存"我发出的挂单"(listing 全文 + 状态 new/失效原因/成交 txid) |

**不需要**:man-p2p 任何改动;`include_outpoints` 通道(V1 的强制花费走完整签名,与部分签名 worker 互不影响,保留给 `mrc721_transfer`)。

## 6. 边界情况

| 场景 | 行为 |
|---|---|
| 挂单后卖家把 NFT 转走了 | 买前校验第 3 条失败 → 拒绝并提示"NFT 已不在卖家手中" |
| 两张挂单并发成交 | 先广播者赢;后广播者节点拒绝(输入已花),钱包按既有 broadcast 错误处理 |
| 买家余额不足 / 找零小于 dust | worker 内预检,失败在签名前(沿 AC4 "no half-done state" 惯例) |
| 广播后未确认 | 买家可双花付款输入,整笔作废;不设超时机制(链下 expiresAt 仅过滤展示) |
| 卖家对同一 NFT 重复挂单 | 允许;都指向同一输入,先广播先成交 |
| 价格 < 22000 sat | 工具层拒绝(对齐 `MVC_NFT_MIN_SELL_PRICE_SATS`) |
| 买家把凭证转给第三方购买 | 凭证绑定 buyerAddress,第三方签名不匹配输出布局,无法受益;重开挂单即可 |

## 7. 测试计划(pattern 同 V1:service mock 索引器 / tools 注册与拒答 / worker 交易形状断言)

1. **worker 形状断言**:sell partial——输入恰 1 个且为 NFT outpoint、输出 0 → buyer、输出 1 == price → seller、仅输入 0 有解锁脚本;buy complete——注入买家输入后全签名、找零正确、费率生效。
2. **校验矩阵**:`verifyMrc721Listing` 对以下逐一拒绝——篡改价格输出、篡改买家输出、卖家签名无效、索引器 owner 不符、outpoint 已花、burned、价格低于下限。
3. **service 编排**:mock 索引器,_sell 先查归属再构建、_buy 校验失败不触碰钱包、成功路径返回 txid 且 spentOutpoints 入会话记录。
4. **tools**:三个工具注册、参数校验、教学性错误文案。
5. `tsc --project electron-tsconfig.json --noEmit` + 既有全套件回归(V1 65/65 不得回退)。

## 8. 验收标准(V1.1)

1. MVC 真机:卖方 MetaBot `mrc721_sell` 产出凭证 → 凭证文本交给买方 MetaBot → `mrc721_buy` 一笔交易成交 → 双方 `mrc721_balance`/`item/transfers` 反映换手。
2. 失败路径至少各验一次:篡改凭证被拒、挂单后 NFT 已转走被拒、买家余额不足被拒。
3. `mrc721_sell_orders` 正确列出本地挂单并标注一张已失效凭证的失效原因。
4. 全程不依赖云端服务(除既有 manapi 索引读取与 wallet-api 广播)。
