# IDBots MRC-721 需求文档(MetaBot 发行/铸造/转移/交易 NFT)

> 状态:需求稿(v1,2026-11-01)
> 上游设计:见仓库根目录 `MRC721_INDEXING_DESIGN.md`(man-p2p 侧索引与聚合)
> 交付边界:本文档是给 IDBots 侧的需求输入;man-p2p 侧的改动在 man-p2p 仓库实施。

## 1. 目标场景

用户在 IDBots 的 MetaBot 中完成 MRC-721(MetaID NFT)的全生命周期操作:

1. **发行**:创建 MRC-721 专辑(collection),成为该 collectionName 的注册者。
2. **铸造**:向自己的专辑 mint NFT 条目(支持设置名称/描述/封面/metadata)。
3. **转移**:把自己的 NFT 转给其他 MetaID 地址。
4. **交易**:通过 PSBT 构建的部分签名交易完成 NFT 买卖。
5. **浏览/查验**:专辑列表、我的 NFT、NFT 详情——**当前归属、转移路径、发行者、真伪**——统一经索引器读取(现有 `fetchFromLocalOrFallback` 路径:本地缓存 → 自建 override → manapi.metaid.io,详见 §2/§4)。

链范围:BTC 与 MVC(同一套协议语义,man-p2p 以 `-chain btc,mvc` 同索引;应用层按 pin 的 chainName 展示来源)。

## 2. 依赖的 man-p2p API(由上游设计 P2 阶段提供)

| 能力 | 端点 | 用途 |
|---|---|---|
| 专辑列表 | `GET /api/mrc721/collection/pageList` | 市场广场页 |
| 专辑详情 | `GET /api/mrc721/collection/info?name=|pinId=` | 发行确认轮询、专辑页 |
| 专辑条目 | `GET /api/mrc721/collection/items/pageList?pinid=` | 专辑内 NFT 列表 |
| 地址的专辑 | `GET /api/mrc721/address/collection?address=` | "我发行的" |
| 地址的 NFT | `GET /api/mrc721/address/item?address=&pinId=` | "我的 NFT"(转移/交易前的持仓源) |
| NFT 详情 | `GET /api/mrc721/item/info?pinId=` | 详情页;**`output`/`outputValue` 为当前 UTXO,转移与交易构建直接使用** |
| 转移路径 | `GET /api/mrc721/item/transfers?pinId=` | 溯源时间线 |
| 真伪查验 | `GET /api/mrc721/item/verify?pinId=` | 详情页"已收录/官方铸造"标识 |

约定:`address` 返回字段同时携带链地址与 `globalMetaId`;列表响应不含 ContentBody;确认轮询以 `item/info` / `collection/info` 命中为准(索引延迟 = pin 确认延迟)。

读取路径(IDBots 现状,已移除内嵌索引器):`fetchFromLocalOrFallback` 模式——本地 sqlite 缓存(`metaid_pins` 表)→ 自建索引器 override(环境变量 `IDBOTS_MAN_P2P_LOCAL_BASE`,可选)→ `https://manapi.metaid.io` 兜底。`/api/mrc721/*` 部署至 manapi.metaid.io 后端后,以上端点沿同一模式自动可用,工具层无需区分本地/远程。

## 3. 功能需求

### R1 发行专辑(collection)

- 输入:collectionName(建议规则:`[a-z0-9-]`,不含 `/`;发行前用 `collection/info?name=` 预检是否已被注册)、name、totalSupply(-1 不限)、royaltyRate(0–20,缺省 0)、desc、website、cover(先上传 metafile 得 pinid)、metadata。
- 行为:构造 `collection_desc` pin(`operation=create`,path=`/nft/mrc721/{collectionName}/collection_desc`,content=上述 JSON),经当前会话链(BTC 或 MVC)广播。
- 确认:轮询 `collection/info` 直到命中;展示 collectionid(=collection_desc pinid)。
- 约束:首个生效;若已被抢注,广播前预检 + 广播后以索引结果为准,UI 明确提示"首个生效"。

### R2 铸造 NFT(item)

- 输入:目标专辑、可选 item 名/描述/封面/metadata(即 item_desc 的 items[] 内容,可后补)。
- 行为:构造 create pin,path=`/nft/mrc721/{collectionName}/{itemPath}`(itemPath 用户可自定义且不与 `collection_desc`/`item_desc` 重名);可选随后(或事后任意时刻)发 `item_desc` pin 补充展示信息。
- 确认:轮询 `item/info?pinId=`;若专辑 totalSupply 已满,铸造会被索引层拒绝收录——发行端预检 `collection/info` 的 `totalnum/totalSupply`。
- 标识:详情页依据 `item/verify` 的 `creatorMinted` 显示"官方铸造/第三方铸造"。

### R3 转移

- 语义:NFT 转移 = 花费 NFT pin 所在 output 的普通转账交易(无专用协议消息);索引层经转移检测自动改写归属并追加转移历史。
- 输入:NFT pinId → `item/info` 取当前 `output`/`outputValue`;收款地址(MetaID 地址,跨链同身份用 globalMetaId 展示)。
- 行为:钱包按该 outpoint 构建转移交易并广播;完成后轮询 `item/info` 的 owner 变化 + `item/transfers` 新增记录。
- 注意:同一 output 的找零/合并需避免把 NFT UTXO 当普通零钱花掉——钱包 UTXO 管理须将 NFT 所在 outpoint 标记为"资产 UTXO"(依赖 `item/info` 与 `address/item` 的持仓清单)。

### R4 交易(模型见 §5.1,建议 V1.1 走 A、V2 升级 C)

- 模式(对齐现有 MRC20/服务订单基建的形态,由 IDBots 侧定稿):
  1. 卖方挂单:指定 NFT(pinId)与要价;托管/撮合的订单态由 IDBots 侧存储(现有 sqliteStore/orderPayment 模式)。
  2. 成交构建:卖方以 NFT 当前 outpoint 为输入构建交易,输出含买方得 NFT、卖方收款(含可选 royalty 输出,比例取 `collection/info.royaltyRate`)。模型 A 下为卖家单方签名转移;模型 C 下为部分签名交易(PSBT/TxComposer 装配),买卖双方各签后广播。
  3. 签名流转:BTC 复用现有 `/api/idbots/wallet/btc/sign-psbt` RPC(`metaidRpcServer.ts` 已有,SIGN_PSBT 模式);MVC 侧模型 A 仅需普通转移(现有 `executeTransfer` + 强制 outpoint 参数),模型 C 需新增 TxComposer 部分签名端点(§5.1)。
  4. 广播与确认:广播后以 `item/info` owner 变化 + `item/transfers` 为最终交割依据(索引即权威)。
- man-p2p 在交易中的职责边界:**只读数据源**(outpoint、归属、历史、royaltyRate);订单、撮合、托管、纠纷不在索引层。

### R5 浏览与查验

- 广场页:专辑分页列表(名称/封面/totalnum/royaltyRate/发行者)→ `GET /api/mrc721/collection/pageList`。
- **钱包资产页(用户钱包里持有的专辑与 NFT)**——持有人视图是索引器派生数据,链上 UTXO 查询无法提供专辑聚合,必须读索引接口:
  - 持有的专辑列表 → `GET /api/mrc721/address/collection?address=`(索引层按该地址持有的条目去重专辑返回);
  - 某专辑下持有的条目(或全部持有)→ `GET /api/mrc721/address/item?address=&pinId=`。
- 详情页:NFT 当前归属、发行者、genesis(链/高度/tx)、转移时间线(`item/transfers`)、真伪事实(`item/verify`:已收录 + creatorMinted + collectionid)。
- 身份展示:优先展示 MetaID(globalMetaId),链地址为辅。

### R6 MetaBot 内置工具(IDBots 开发范围核心)

MetaBot 通过以下内置工具(agent tools)操作 MRC-721,注册模式沿用 `walletAgentTools.ts` 既有约定(名称 + 描述 + zod 参数 + 文本结果;批量 `metabot_ids` 单次调用;会话内写操作只动会话 bot 自己的钱包,私钥不出 host)。**命名采用 `mrc721_` 前缀,与既有 `wallet_nft_*`(合约系 NFT,genesis/codehash/tokenIndex 语义)严格区分**。

| 工具 | 类型 | 功能 | 依赖 |
|---|---|---|---|
| `mrc721_collection_create` | 写 | 发行专辑:collectionName 预检(`collection/info`)→ 构造 collection_desc pin → 广播 → 轮询确认 | 钱包 pin 构造器 + manapi |
| `mrc721_mint` | 写 | 铸造条目;可选 name/desc/cover/metadata(随发 item_desc) | 同上 |
| `mrc721_transfer` | 写 | 转移:`item/info` 取当前 output → 强制 outpoint 花费构建转移交易 → 广播 → 轮询 `item/transfers` | raw-tx `include_outpoints`(§4) |
| `mrc721_balance` | 读 | 我的持仓:`address/item`(可按专辑过滤) | manapi |
| `mrc721_collections` | 读 | 我的专辑 + 广场:`address/collection`、`collection/pageList` | manapi |
| `mrc721_item_info` | 读 | 详情 + 真伪 + 溯源:`item/info`、`item/verify`、`item/transfers` | manapi |
| `mrc721_collection_info` | 读 | 专辑详情与条目分页 | manapi |
| `mrc721_sell` / `mrc721_buy` / `mrc721_sell_orders` | 写/读 | V1.1 模型 A 订单托管式交易(挂单/购买/查单) | 订单基建 + `mrc721_transfer` |

## 4. IDBots 侧技术落点(现状核查 2026-11-01 修订)

- **索引数据访问(重要修正)**:IDBots 已在 `refactor/remove-man-p2p`(80c8a718)移除内嵌 man-p2p 运行时与打包二进制,改为 **remote-first 读取**:`fetchFromLocalOrFallback` = 本地 sqlite 缓存(`metaid_pins` 表)→ 自建索引器 override(`IDBOTS_MAN_P2P_LOCAL_BASE`)→ `https://manapi.metaid.io` 兜底。本文档所有索引读取均指该路径,无本地内嵌依赖。
- 已具备(钱包/交易侧):
  - BTC PSBT 签名 RPC(`/api/idbots/wallet/btc/sign-psbt`);MRC20 订单/支付/观察者会话等交易基建(orderPayment、buyerOrderObserverSession、metabotTokenTransferService)。
  - MVC 转账执行与 raw-tx 构建链:`executeTransfer`、`BUILD_MVC_TRANSFER_RAW_TX`、`BUILD_MVC_FT_TRANSFER_RAW_TX`、`BUILD_MVC_RAW_TX_BUNDLE`(walletRawTxService + workers)。
  - agent tools 注册基建(walletAgentTools.ts:wallet_balance/wallet_transfer/wallet_nft_*/wallet_token_* 等既有工具)。
  - MVC **合约系 NFT 全栈**(`mvcNftService` + meta-contract `NftManager`)——**本期不动**,与 MRC-721 并存两套协议(§5.2)。
- **生产接口可用性(2026-11-01)**:

  | 接口 | 状态 |
  |---|---|
  | `/pin/:id`、余额/UTXO、BTC sign-psbt、MVC 转账/raw-tx 构建 | 已上线,可用 |
  | `/api/mrc721/*`(§2 全部 8 端点) | **未上线**——依赖 man-p2p P2 实现并部署至 manapi.metaid.io 后端节点;旧 MetaSo 服务器虽有同形接口但不在 IDBots 读取路径上,不作为依赖 |

- **开发时序结论**:IDBots **可以立即开工**——工具层(R6)、钱包侧(R1–R3)、UI 按 §2 API 契约开发,mock 数据先行;联调与生产验收依赖 man-p2p **P1(转移钩子)+ P2(JSON API)** 合入并部署 manapi.metaid.io。man-p2p 侧 P0(索引与聚合)已完成。

- 需新增(IDBots 侧):
  1. MRC-721 pin 构造器(collection_desc / item mint / item_desc)+ R6 工具集。
  2. **raw-tx 构建器的"强制花费指定 outpoint"能力**:现有构建器只支持 `exclude_outpoints`,无法指定输入;MRC-721 转移 = 花费 NFT pin 所在 output,必须支持 `include_outpoints`(或专用 `buildMvc721TransferRawTx`)。这是转移(R3)与交易(R4)共同的前置。
  3. 挂单/成交流程与 NFT UI(广场/详情/我的)。

## 5. 待确认(IDBots 侧拍板项)

1. **MRC-721 交易结算模型** —— **已定稿(2026-11-01):V1.1 采用 A 订单托管式**;C 原子交换式保留为 V2 升级候选;B 合约挂单式弃用:
   - A. 订单托管式(已选定):买家付款 → 卖家转移 NFT(outpoint 强制花费);信任由订单/托管逻辑承担(复用现有 orderPayment 模式)。改动最小,纯钱包既有能力 + 第 2 项新参数。
   - B. 合约挂单式(弃用):仿现有合约系 NFT 的 sell/buy,但需把 pin-NFT 适配进 NftManager 两段式合约流程,工作量大、收益不明显。
   - C. 原子交换式(V2 候选,真 PSBT 语义):一笔交易同时含买家付款输入与卖家 NFT 输入,双方签名后广播。BTC 已有 sign-psbt 可做;MVC 的 meta-contract `TxComposer` 支持部分签名装配,但现有 RPC 未暴露——需新增"部分签名组装/外部签名回填"端点。MRC-721 转移是普通 UTXO 花费,单笔原子交换无需合约,技术上顺理成章。
2. **合约系 NFT 与 MRC-721 并存展示**(已明确 2026-11-01):本期专注 MRC-721,IDBots 已实现的 metacontract NFT 全栈(mvcNftService)**先不动**;钱包资产页并存展示方式由 UI 届时定。metacontract → MRC-721 格式转换列入后续规划(见 §7)。
3. 挂单/订单存储与撮合:本地 sqlite 是否够用;跨设备发现是否需要云端目录(不进 man-p2p 索引层)。
4. royalty 结算:V1 仅登记与展示 royaltyRate,分账输出是否进交易 PSBT/模板。
5. 双链资产展示:BTC 与 MVC 的 NFT 同列表混排(按 genesisChainName 过滤)还是分页签,由 UI 定。

## 6. 验收标准(V1)

1. MetaBot 内完成:发行专辑 → mint ≥2 个 NFT(其一后补 item_desc)→ 转移给另一地址,全程 MVC;BTC 侧以回填数据做只读验收。
2. 联调:以 `IDBOTS_MAN_P2P_LOCAL_BASE` 指向测试 man-p2p 节点,或按 §2 契约 mock;生产验收以 manapi.metaid.io 上线 `/api/mrc721/*` 后为准。
3. 详情页正确展示:当前归属、完整转移路径、发行者、真伪标识(含一个第三方铸造样例展示"第三方铸造"标识)。
4. 交易(若 V1.1 同期,模型 A):挂单 → 付款 → 卖家转移 NFT → `item/transfers` 确认交割,流程闭环。

## 7. 后续规划(非本期)

- **metacontract NFT → MRC-721 格式转换**:将 IDBots/metalet 侧已有的合约系 NFT 资产迁移/映射为 MRC-721 格式(协议差异:genesis/codehash/tokenIndex vs pinid 路径体系)。待 MRC-721 主流程(发行/铸造/转移/交易/查验)稳定后单独立项,本期不做。
