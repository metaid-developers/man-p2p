# BTC JSON API 接口文档

---

## 1. 获取 MetaId 列表

**GET** `/api/metaid/list`

**参数：**
- `page` (int64, 必填)：页码，从 1 开始
- `size` (int64, 必填)：每页数量

**返回：**
```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "list": [
      "metaid1",
      "metaid2"
    ],
    "count": 12345
  }
}
```

---

## 2. 获取 Pin 列表

**GET** `/api/pin/list`

**参数：**
- `page` (int, 必填)：页码，从 1 开始
- `size` (int, 必填)：每页数量
- `lastId` (string, 可选)：上次分页最后一个 Pin 的 Id

**返回：**
```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "Pins": [
      {
        "Content": "内容摘要",
        "Number": 123,
        "Operation": "create",
        "Id": "pinid",
        "Type": "text",
        "Path": "/path",
        "MetaId": "metaid",
        "Pop": 0,
        "ChainName": "btc",
        "InitialOwner": "address",
        "Address": "address",
        "CreateAddress": "address",
        "Timestamp": 1690000000
      }
    ],
    "Count": {
      "MetaId": 12345,
      "Pin": 67890
    },
    "Active": "index",
    "LastId": "pinid"
  }
}
```

---

## 3. 获取 Mempool Pin 列表

**GET** `/api/mempool/list`

**参数：**
- `page` (int64, 必填)：页码，从 1 开始
- `size` (int64, 必填)：每页数量

**返回：**
```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "Pins": [
      {
        "Content": "内容摘要",
        "Number": 123,
        "Operation": "create",
        "Id": "pinid",
        "Type": "text",
        "Path": "/path",
        "MetaId": "metaid"
      }
    ],
    "Count": {
      "MetaId": 12345,
      "Pin": 67890
    },
    "Active": "mempool"
  }
}
```

---

## 4. 获取 Pin 详情

**GET** `/api/pin/:numberOrId`

**参数：**
- `numberOrId` (string, 路径参数)：Pin 编号或 Id

**返回：**
```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "ContentSummary": "内容摘要",
    "Number": 123,
    "Operation": "create",
    "Id": "pinid",
    "Type": "text",
    "Path": "/path",
    "MetaId": "metaid",
    "Pop": 0,
    "ChainName": "btc",
    "InitialOwner": "address",
    "Address": "address",
    "CreateAddress": "address",
    "Timestamp": 1690000000,
    "PopLv": 1,
    "Preview": "http://host/pin/pinid",
    "Content": "http://host/content/pinid"
  }
}
```

---

## 5. 获取指定地址和 path 下 Pin（倒序分页）

**GET** `/api/address/pin/list/:address`

**参数：**
- `address` (string, 路径参数)：地址
- `cursor` (int64, 必填)：跳过条数
- `size` (int64, 必填)：返回条数；`<=0` 回退 20，`>200` 按 200 截断
- `path` (string, 必填)：路径

**返回：**
```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "list": [
      {
        "ContentSummary": "内容摘要",
        "Id": "pinid",
        "Path": "/path",
        "MetaId": "metaid",
        "ChainName": "btc",
        "Address": "address",
        "Timestamp": 1690000000,
        "Preview": "http://host/pin/pinid",
        "Content": "http://host/content/pinid",
        "PopLv": 1
      }
    ],
    "total": 100
  }
}
```

---

## 6. 获取指定 metaid 和 path 下 Pin（倒序分页）

**GET** `/api/metaid/pin/list/:metaid`

**参数：**
- `metaid` (string, 路径参数)：legacy metaid；也兼容合法的 GlobalMetaID（`id...`），服务端会按 BTC mainnet legacy 地址别名转换后复用 legacy metaid 查询
- `cursor` (int64, 必填)：跳过条数
- `size` (int64, 必填)：返回条数；`<=0` 回退 20，`>200` 按 200 截断
- `path` (string, 必填)：路径

**返回：**
```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "list": [
      {
        "ContentSummary": "内容摘要",
        "Id": "pinid",
        "Path": "/path",
        "MetaId": "metaid",
        "ChainName": "btc",
        "Address": "address",
        "Timestamp": 1690000000,
        "Preview": "http://host/pin/pinid",
        "Content": "http://host/content/pinid",
        "PopLv": 1
      }
    ],
    "total": 100
  }
}
```

---

## 7. 获取指定 path 下所有 Pin（分页）

**GET** `/api/pin/path/list`

**参数：**
- `cursor` (string, 可选)：倒序分页游标，首次为空；取上一页返回的 `nextCursor`
- `size` (int64, 必填)：返回条数；`<=0` 回退 20，`>200` 按 200 截断（响应回显生效的 `size`）
- `path` (string, 必填)：路径
- `since` (string, 可选)：增量游标（排他下界），传入后按正序（最旧优先）返回排序键大于 `since` 的 Pin。首次传 `since=0` 从头补齐历史；之后把返回的 `nextCursor` 作为下一次的 `since` 轮询增量，直到返回空列表。游标含 `&` 分隔符，必须 URL 编码后传递（`cursor` 同理）

**返回：**
```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "list": [
      {
        "ContentSummary": "内容摘要（见下方内容契约）",
        "Content": "http://host/content/pinid",
        "ContentLength": 1024,
        "Id": "pinid",
        "Path": "/path",
        "MetaId": "metaid",
        "ChainName": "btc",
        "Address": "address",
        "Timestamp": 1690000000,
        "Preview": "http://host/pin/pinid",
        "PopLv": 1
      }
    ],
    "total": 100,
    "nextCursor": "游标",
    "size": 20
  }
}
```

**内容契约（协议池遍历专用）：**
- `content`：始终为稳定的内容下载地址 `{host}/content/{pinId}`，列表行永远携带
- `contentSummary`：本地持有内容体时为其前 4096 字节；`contentLength > len(contentSummary)` 表示被截断
- `contentSummary` 为空且 `contentLength > 0`：本地节点只存了元数据（例如超大 pin 仅通过 gossip 元数据入库），完整内容需等同步补齐后经 `content` 地址获取
- `contentBody`：列表行恒为空

---

## 8. 获取地址 MetaId 信息

**GET** `/api/info/address/:address`

**参数：**
- `address` (string, 路径参数)：地址

**返回：**
```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "MetaIdInfo": {
      "MetaId": "metaid",
      "Address": "address",
      "Status": 1,
      ...
    },
    "unconfirmed": "",
    "blocked": false
  }
}
```

---

## 9. 获取 MetaId 信息

**GET** `/api/info/metaid/:metaId`

**参数：**
- `metaId` (string, 路径参数)：metaid

**返回：**
```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "MetaIdInfo": {
      "MetaId": "metaid",
      "Address": "address",
      "Status": 1,
      ...
    },
    "unconfirmed": "",
    "blocked": false
  }
}
```

---

## 10. 获取通知列表

**GET** `/api/notifcation/list`

**参数：**
- `address` (string, 必填)：地址
- `lastId` (int64, 可选)：上次分页最后一个通知的 Id

**返回：**
```json
{
  "code": 200,
  "message": "ok",
  "data": [
    {
      "NotifcationId": 123,
      "FromPinId": "pinid",
      "Type": "type",
      "Content": "内容"
    }
  ],
  "total": 10
}
```

---

## 11. 区块分片文件下载

**GET** `/api/block/file`

**参数：**
- `height` (int64, 必填)：区块高度
- `chain` (string, 必填)：链名
- `part` (int, 可选)：分片索引

**返回：**
- 文件流（二进制下载）

---

## 12. 查询区块分片文件数量

**GET** `/api/block/file/partCount`

**参数：**
- `height` (int64, 必填)：区块高度
- `chain` (string, 必填)：链名

**返回：**
```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "partCount": 5,
    "btcMin": 100,
    "btcMax": 200,
    "mvcMin": 300,
    "mvcMax": 400
  }
}
```

---

## 13. 批量创建区块分片文件

**GET** `/api/block/file/create`

**参数：**
- `token` (string, 必填)：管理员 token
- `chain` (string, 必填)：链名
- `from` (int64, 必填)：起始高度
- `to` (int64, 必填)：结束高度

**返回：**
- 字符串："block file create finish"

---

## 14. 获取区块 id 列表

**GET** `/api/block/id/list`

**参数：**
- `token` (string, 必填)：管理员 token
- `chain` (string, 必填)：链名
- `height` (int64, 必填)：区块高度

**返回：**
```json
{
  "code": 1,
  "message": "ok",
  "data": {
    "data": ["id1", "id2"]
  }
}
```

---

## 15. 批量设置区块 PinId 列表

**GET** `/api/block/id/create`

**参数：**
- `token` (string, 必填)：管理员 token
- `chain` (string, 必填)：链名
- `from` (int64, 必填)：起始高度
- `to` (int64, 必填)：结束高度

**返回：**
- 字符串："block file pin id list create finish"

---
