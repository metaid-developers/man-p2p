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
- `size` (int64, 必填)：返回条数
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
- `metaid` (string, 路径参数)：metaid
- `cursor` (int64, 必填)：跳过条数
- `size` (int64, 必填)：返回条数
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
- `cursor` (int64, 必填)：跳过条数
- `size` (int64, 必填)：返回条数
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
        "Timestamp": 1690000000
      }
    ],
    "total": 100
  }
}
```

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
