# MRC-721 索引与聚合设计(man-p2p 全量接管)

> 状态:规划稿(v1,2026-11-01)
> 目标:在 man-p2p 内原生实现 MRC-721(基于 MetaID PIN 的 NFT)的索引、聚合与查询 API,同时覆盖 BTC 与 MVC;应用端(IDBots/MetaBot)基于本层 API 实现发行、铸造、转移、交易。

## 1. 背景与决策

- 生产上现有的 MRC-721 聚合在旧 MetaSo(metaso)`basicprotocols/mrc721`:轮询 v1 MAN indexer 的 Mongo `pins` 视图 + `pin_transfer_history`,建 `mrc721collection`/`mrc721item` 两个 Mongo 集合并暴露 `/api/mrc721/*`。它是**二次聚合**,不解析链上数据。
- 决策:**man-p2p 全量接管** MRC-721 的索引与聚合;metaso 实现仅作语义参考。前期不拆独立服务,性能或复杂聚合需求出现时再评估拆分。
- man-p2p 已具备全部底座原语:
  - 多链同时索引:`-chain btc,mvc`(man.go:89 `strings.Split(chainType, ",")`),`IndexerAdapter`/`ChainAdapter` 按 chain 注册,`IndexerRun`/`ZmqRun` 均按链循环。
  - PIN 语义无跨链概念:不同链视为一张大 UTXO 网络;`chainName` 只在 pin 记录(输出层)标注来源链,`p2p_wiring.go:37` 同步 pin 亦携带 `ChainName`。
  - `/nft` 为合规根路径(pin/validator.go),pin 主存储保留 `Path/Operation/ContentBody`。
  - 转移检测:`handleTransfer`(man/indexer_pebble.go)→ `PinTransferHistory` 已入库。
  - MRC20 已示范同样的"pin 入库 → 解析 content → 协议索引"模式。

## 2. 协议语义(对官方文档的实现裁决)

官方文档:https://docs.metaid.io/ft-and-nft-protocols/mrc-721 (滞后于实现,标注"testnet 测试中")。

### 2.1 路径

| 对象 | 路径 | 说明 |
|---|---|---|
| NFT 条目 | `/nft/mrc721/{collection_name}/{anything}` | 任意 create pin 即铸造 |
| 专辑描述 | `/nft/mrc721/{collection_name}/collection_desc` | 专辑注册;`collectionid` = 该 pin 的 pinid;**首个生效,后续同名忽略** |
| 条目描述 | `/nft/mrc721/{collection_name}/item_desc` | 可选、可多条;对同一 itemPinId **首条生效**;引用不存在条目自然忽略 |

约束:仅接受 `operation=create`;`modify/revoke/hide` 在 `/nft/mrc721` 下不生效。collection_name 建议**不含 `/`**(路径按 `/` 切分)。

### 2.2 collection_desc JSON

```json
{
  "name": "collection full name",
  "totalSupply": -1,
  "royaltyRate": 0,
  "desc": "...",
  "website": "https://...",
  "cover": "metafile://<pinid>",
  "metadata": "..."
}
```

- `totalSupply`:[-1, 1e12],-1 = 不限(缺省 -1);超限的 mint 不收录。
- `royaltyRate`:[0, 20] 百分比。
- **裁决(与 metaso 实现的差异)**:缺省 `royaltyRate` 对齐官方文档取 **0**(metaso 取 5,属实现偏差);校验 JSON 时**只对 key 做大小写不敏感匹配,值保留原始大小写**(metaso 将整个 content `ToLower` 导致 name/desc/website/metadata 值大小写丢失,属缺陷,不沿用)。回填的 metaso 存量数据保留其已落库的值,不重算。

### 2.3 item_desc JSON

```json
{ "items": [ { "pinid": "...", "name": "...", "desc": "...", "cover": "metafile://...", "metadata": "..." } ] }
```

### 2.4 命名空间与跨链语义

- collectionName **全局唯一**(跨 BTC/MVC 同一命名空间,首个 collection_desc 按"先确认先注册"生效)。这是"一张大 UTXO 网络"语义的自然结果:抢注冲突等同于单链上的时间顺序冲突。
- 每条 collection/item 记录携带 `chainName`(genesis pin 来源链)、`genesisHeight`、`genesisTx` 等**展示性字段**;唯一性 key 不含链。
- 同名风险:他人可向已存在的 collection 路径 mint(item 是开放铸造,协议本身无铸造权限概念),但**无法**接管 collection(collection_desc 首个生效)或覆盖首个 item_desc。

### 2.5 真伪/可信语义(本设计新增,metaso 未提供)

- `creatorMinted bool`:item.createAddress == collection.createAddress。开放铸造下,此项是"官方铸造"的事实标志,展示层据此打标,索引层不做拒绝。
- 真伪判定事实集:是否被收录(路径合法 + 专辑已注册 + 未超 supply)、genesis 信息(链/高度/tx)、collectionid 归属、是否 creatorMinted。**不做**白名单/链下认证。

## 3. 数据模型(Pebble)

新增专用 DB(挂到 `pebblestore.Database`,命名对齐现有 `Mrc20Db` 风格):

| DB | Key | Value | 用途 |
|---|---|---|---|
| `Mrc721CollectionDb` | `collectionName` | `Mrc721Collection` JSON | 专辑主表 |
| `Mrc721CollectionByPinIdDb` | `collectionDescPinId` | `collectionName` | 按 collectionid 反查 |
| `Mrc721ItemDb` | `itemPinId` | `Mrc721Item` JSON | NFT 主表(含当前 owner、当前 outpoint) |
| `Mrc721CollItemListDb` | `collectionName & seq & itemPinId` | 空(seq=收录序号,定长补零) | 专辑内条目分页(正序遍历) |
| `Mrc721OwnerItemDb` | `ownerAddress & collectionName & itemPinId` | 空 | 按持有人查询;转移时删旧 key 写新 key |
| `Mrc721ItemTransferDb` | `itemPinId & height & txid` | `{from,to,chainName,time}` | 转移路径/溯源 |
| `CountDB`(复用) | `mrc721_coll_{name}_total` 等 | 计数 | totalnum、holders |

`Mrc721Collection`:`collectionName, name, totalSupply, royaltyRate, desc, website, cover, metadata, pinid(collectionid), createAddress, createMetaId, createTime, genesisChainName, genesisHeight, genesisTx, totalnum`。
`Mrc721Item`:`itemPinId, collectionName, collectionPinId, number(收录序号), name/desc/cover/metadata(item_desc 回填), ownerAddress, ownerMetaId, creatorMinted, contentType, contentSummary, genesisChainName, genesisHeight, genesisTx, genesisFee, output(当前 outpoint), outputValue, createTime, transferCount`。

注:当前 outpoint 直接取 pin 主记录(转移检测 `UpdateTransferPin` 会随转移刷新 pin 的 Output/OutputValue),与 metaso 的 `getPinOutput` 回填等价,无需额外管道。

## 4. 聚合流程(ingest 钩子,替代轮询)

### 4.1 Mint 钩子

在 pin 入库主流程(MRC20 解析所在层)后挂 `Mrc721PinHook(pins []*pin.PinInscription)`:

1. 过滤:`strings.ToLower(path)` 前缀 == `/nft/mrc721` 且 `operation == "create"`。
2. 按路径切分:第 3 段 = collectionName;第 4 段:
   - `collection_desc` → 校验(JSON 合法、totalSupply/royaltyRate 范围、**collectionName 未注册**)→ 写 CollectionDb + ByPinIdDb。已存在则忽略(首个生效)。
   - `item_desc` → 校验所属 collection 已注册 → 按 `items[].pinid` 回填 ItemDb 的 name/desc/cover/metadata(仅当该字段为空,首条生效)。
   - 其余 → mint:要求所属 collection 已注册;`totalSupply != -1` 时校验 `totalnum < totalSupply`(原子自增,单进程 ingest 串行无竞态,优于 metaso 的批间近似计数)→ 写 ItemDb + CollItemListDb + OwnerItemDb + 计数;`creatorMinted` 按创建地址比对。
3. **乱序自愈**(修复 metaso 丢条目缺陷):mint 时若 collection 未注册,条目记入 `Mrc721PendingDb`(`collectionName & itemPinId`);`collection_desc` 注册成功时回放 pending 列表,回放完删除。
4. item_desc 同理:引用的 itemPinId 尚未收录时暂存 pending,条目收录后回放。

### 4.2 Transfer 钩子

在 `handleTransfer` 产出 `PinTransferHistory` 处(meltdown 场景按现状排除)挂 `Mrc721TransferHook`:

- 对命中的 itemPinId:更新 ItemDb.owner/ownerMetaId/output、OwnerItemDb 换 key、`transferCount+1`、追加 `Mrc721ItemTransferDb` 记录。
- 不需要 metaso 的 `SyncAddress` 兜底管道(那是修 Mongo join 缺失的;本设计 ingest 时直接写 owner)。

### 4.3 熔化/撤销

`revoke`/`hide`/`modify` 在 `/nft/mrc721` 下按协议不生效;meltdown 转移(烧毁)在钩子中标记 `burned=true` 并从持有人视图排除,保留记录可溯源。(`mrc721` 语义下暂无 burn 需求,仅留字段。)

## 5. API 设计

挂载条件:`chainSourceEnabled && common.ModuleExist("mrc721")`(配置 `module` 已含 `mrc721`,无需改配置),并仿 `Mrc20RuntimeEnabled` 增加运行时开关。替换 `api/webapi.go:692-744` 的注释残骸;HTML 页面(mrc721.html/mrc721item.html)本期不动。

兼容 metaso 的 6 端点(路径/参数/响应结构保持一致,便于应用端与历史调用方平滑切换):

| 端点 | 说明 |
|---|---|
| `GET /api/mrc721/collection/pageList?cursor=&size=` | 专辑列表(totalnum 降序可选) |
| `GET /api/mrc721/collection/info?name=|pinId=` | 专辑详情 |
| `GET /api/mrc721/collection/items/pageList?pinid=&cursor=&size=` | 专辑条目分页 |
| `GET /api/mrc721/address/collection?address=` | 地址持有的专辑 |
| `GET /api/mrc721/address/item?address=&pinId=` | 地址持有的 NFT(可按专辑过滤) |
| `GET /api/mrc721/item/info?pinId=` | NFT 详情 |

新增 2 端点(IDBots 场景必需):

| 端点 | 说明 |
|---|---|
| `GET /api/mrc721/item/transfers?pinId=&cursor=&size=` | 转移路径(溯源) |
| `GET /api/mrc721/item/verify?pinId=` | 真伪事实集:收录状态、collectionid、genesis(chainName/height/tx)、creatorMinted、transferCount |

响应统一沿用 `api/respond` 现有 `ApiSuccess/Err*` 封装;列表项**裁剪 ContentBody**(遵循现有列表裁剪惯例);地址字段同时返回 `address` 与 `globalMetaId`(跨链统一身份)。

## 6. 数据回填(BTC 存量,方案 A:离线快照重放,2026-11-01 定稿)

硬约束:**不得在生产节点上执行全量扫描类回填**——历史经验表明会拖垮或打挂线上索引服务。

生产节点 1(chain-enabled btc,mvc)的 Pebble 已含全部历史 pin,回填只需重放其中少量 `/nft/mrc721` pin,因此采用**离线快照重放**:

1. 按新节点 SOP 的种子复制方式取得数据目录快照(在线预复制 + 短停机 final delta,停机窗口秒级);
2. 在离线机器上对快照执行 `tools/mrc721_backfill -data-dir <快照> [-chain btc] [-dry-run]`:
   - 扫描全部 pins 分片(含 "rc721" 粗筛预过滤),收集 `create` 且路径前缀 `/nft/mrc721` 的 pin 主记录(即最新记录,含当前持有人);
   - **铸造重放**:`(genesisHeight, txIndex, Id)` 升序、Address 取 `InitialOwner`(创世持有人语义),走与实时 ingest **同一套** `handleMrc721` 聚合;
   - **转移时间线重放**:快照 `TransferDb`(PinId 与 itemPinId 同为 `txidiN` 格式)按高度升序经 `Mrc721BackfillRunTransfers` 重放;
   - **归属/outpoint 校正**:`Mrc721BackfillReconcileOwner` 按 pin 主记录的当前 Address/Output 兜底 TransferDb 缺口;`Status==PinStatusMeltdown` 标记 burned;
   - 全表 upsert by pinId,**幂等可重跑**(重跑计数不变)。
3. 产物为快照内新增的 `mrc721/db`(体积极小),整目录拷回生产节点对应路径即可;或让该离线节点作为 p2p follower 直接对外服务。

事实澄清(存档):metaso 的聚合结果在 MongoDB,无法直接搬库;若快照中缺 BTC 历史 pin(快照不含全量 BTC 史时),备选从旧生产 Mongo `pins` 视图导出后走同一套入口重放。

## 7. 分阶段实施

| 阶段 | 内容 | 验证 |
|---|---|---|
| P0 | Pebble 存储层 + `Mrc721PinHook`(collection_desc/item_desc/mint/乱序 pending)+ 校验 | 单测:注册/首条生效/supply 上限/乱序自愈/大小写保留 |
| P1 | `Mrc721TransferHook` + 转移历史 + owner 换 key | 单测:转移后归属/历史/持有人视图;与 `make test` 全绿 |
| P2 | 8 个 JSON API + 运行时开关 + 替换 webapi 残骸 | `make alpha-test`;curl 冒烟(regtest mint→transfer→查询) |
| P3 | BTC 存量重放回填脚本 | 回填前后计数对账(metaso Mongo vs man-p2p Pebble) |
| P4 | IDBots 集成(见 docs/IDBOTS_MRC721_REQUIREMENTS.md) | IDBots electron:dev → 打包验收 |

工程约束沿用 AGENTS.md:decimal 不涉及(全为计数)、sonic 解析 JSON、`sync.Mutex` 保护聚合内缓存、`CGO_ENABLED=0` 构建与测试。

## 8. 风险与待确认

1. **collectionName 跨链抢注**:全局唯一命名空间下,MVC 开放后存在抢注/撞名。首注册生效是协议语义,但热门名字需引导尽早注册。(如需"同名单链共存",要引入链维度 key——与"无跨链概念"哲学冲突,默认不做。)
2. **开放铸造**:任何人可向 `/nft/mrc721/{name}` mint 至 supply 上限。展示层用 `creatorMinted` 区分;若业务要求"仅创作者可铸",是钱包/应用侧引导问题,索引层不做拒绝(协议如此)。
3. **royaltyRate 缺省 0 vs 存量 5**:回填数据保留原值;新数据按 0。应用端展示时对缺省专辑注意区分。
4. **全局"专辑列表"分页**:CollItemListDb 按专辑内分页;全站专辑列表用 CollectionDb 全量迭代(Pebble 前缀迭代,量小可行),量大后再加排序索引。
5. **P2P 传播**:MRC-721 是派生数据,不经 gossip 独立传播;各节点从 pin 重放聚合,一致性依赖 pin 同步完成度(与 metaname/alias 同性质)。
