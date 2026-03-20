# IDAddress TypeScript Library

跨链地址统一标识系统 - TypeScript 实现

## 功能特性

- ✅ 支持 Bitcoin P2PKH (Legacy)
- ✅ 支持 Bitcoin P2SH
- ✅ 支持 Bitcoin P2WPKH (SegWit v0)
- ✅ 支持 Bitcoin P2WSH (SegWit v0)
- ✅ 支持 Bitcoin P2TR (Taproot)
- ✅ 支持 Dogecoin P2PKH
- ✅ 支持 MVC P2PKH
- ✅ Base58Check 编解码
- ✅ Bech32/Bech32m 编解码
- ✅ 双向地址转换
- ✅ 跨链身份统一

## 安装

```bash
npm install idaddress
```

## 使用示例

### 基础使用

```typescript
import { convertToGlobalMetaId, convertToBitcoin } from 'idaddress';

// Bitcoin Legacy 地址 -> GlobalMetaId
const globalId = await convertToGlobalMetaId('1KAZD8sxTDkNzcjtCpKJ9ynPGsB8oryoFk');
console.log(globalId); 
// 输出: idq1caqw2z0gn5gx3c79ach7s0p8h7ra3ggetcxu2a

// GlobalMetaId -> Bitcoin 地址
const btcAddr = await convertToBitcoin(globalId, 'mainnet');
console.log(btcAddr);
// 输出: 1KAZD8sxTDkNzcjtCpKJ9ynPGsB8oryoFk
```

### SegWit 地址

```typescript
import { convertToGlobalMetaId, convertToBitcoin } from 'idaddress';

// SegWit 地址 -> GlobalMetaId
const segwitAddr = 'bc1qfxd3xp3q65ulewmzrfx50pxw45qjfvgdsfq4ah';
const globalId = await convertToGlobalMetaId(segwitAddr);
console.log(globalId);
// 输出: idz1fxd3xp3q65ulewmzrfx50pxw45qjfvgd0j24uz

// GlobalMetaId -> SegWit 地址
const converted = await convertToBitcoin(globalId, 'mainnet');
console.log(converted === segwitAddr); // true
```

### Taproot 地址

```typescript
import { convertToGlobalMetaId, convertToBitcoin } from 'idaddress';

// Taproot 地址 -> GlobalMetaId
const taprootAddr = 'bc1pj93y5u8q8uszm5mktzu0hxs5keras3kzzt06n7e0ky0qenwdt0yqawfyy0';
const globalId = await convertToGlobalMetaId(taprootAddr);
console.log(globalId);
// 输出: idt1j93y5u8q8uszm5mktzu0hxs5keras3kzzt06n7e0ky0qenwdt0yqqk89qy
```

### Dogecoin 地址

```typescript
import { convertToGlobalMetaId, convertToDogecoin } from 'idaddress';

// Dogecoin 地址 -> GlobalMetaId
const dogeAddr = 'DFo712BpLysLsoF6kSjTN6pPmZXxibtWcG';
const globalId = await convertToGlobalMetaId(dogeAddr);
console.log(globalId);
// 输出: idq1wnshx9kvz379ssjz38ku4q7vdwz36lg5hlr2gy

// GlobalMetaId -> Dogecoin 地址
const converted = await convertToDogecoin(globalId);
console.log(converted === dogeAddr); // true
```

### 跨链身份统一

```typescript
import { convertToGlobalMetaId } from 'idaddress';

// 同一个私钥在不同链上的地址
const mvcAddr = '195gtuVbW9DsKPnSZLrt9kdJrQmvrAt7e3';
const dogeAddr = 'DDDnSASEoZ89rPy3HvrShWnujYWEABGhUB';

const mvcGlobal = await convertToGlobalMetaId(mvcAddr);
const dogeGlobal = await convertToGlobalMetaId(dogeAddr);

console.log(mvcGlobal === dogeGlobal); // true
// 输出: idq1tz3ljq763lqsj2wp894h06vxn0ndhnsq3fllnj
```

### 使用地址转换器类

```typescript
import { AddressConverter } from 'idaddress';

const converter = new AddressConverter('mainnet');

// 转换为 IDAddress
const idAddr = await converter.toID('1KAZD8sxTDkNzcjtCpKJ9ynPGsB8oryoFk');

// 转换回 Bitcoin
const btcAddr = await converter.fromID(idAddr, 'bitcoin');

// 转换为 Dogecoin
const dogeAddr = await converter.fromID(idAddr, 'dogecoin');

// 批量转换
const addresses = [
  '1KAZD8sxTDkNzcjtCpKJ9ynPGsB8oryoFk',
  'DFo712BpLysLsoF6kSjTN6pPmZXxibtWcG',
  'bc1qfxd3xp3q65ulewmzrfx50pxw45qjfvgdsfq4ah',
];

const results = await converter.batch(addresses);
results.forEach((r, i) => {
  if (r.result) {
    console.log(`${addresses[i]} -> ${r.result}`);
  } else {
    console.error(`${addresses[i]} 转换失败: ${r.error?.message}`);
  }
});
```

### 验证地址

```typescript
import { validateIDAddress, validateGlobalMetaId } from 'idaddress';

const isValid = validateIDAddress('idq1caqw2z0gn5gx3c79ach7s0p8h7ra3ggetcxu2a');
console.log(isValid); // true

const isValidGlobal = validateGlobalMetaId('invalid-address');
console.log(isValidGlobal); // false
```

## API 文档

### 地址转换

#### `convertToGlobalMetaId(address: string): Promise<string>`
将任意支持的区块链地址转换为 GlobalMetaId (IDAddress 格式)

#### `convertFromBitcoin(address: string): Promise<string>`
从 Bitcoin 地址转换为 IDAddress (等同于 convertToGlobalMetaId)

#### `convertToBitcoin(idAddr: string, network: 'mainnet' | 'testnet'): Promise<string>`
从 IDAddress 转换为 Bitcoin 地址

#### `convertToDogecoin(idAddr: string): Promise<string>`
从 IDAddress 转换为 Dogecoin 地址

#### `convertToMVC(idAddr: string): Promise<string>`
从 IDAddress 转换为 MVC 地址

### 验证

#### `validateIDAddress(addr: string): boolean`
验证 IDAddress 格式是否正确

#### `validateGlobalMetaId(globalMetaId: string): boolean`
验证 GlobalMetaId 格式是否正确 (等同于 validateIDAddress)

### 编解码

#### `encodeIDAddress(version: AddressVersion, data: Uint8Array): string`
编码 IDAddress

#### `decodeIDAddress(addr: string): IDAddressInfo`
解码 IDAddress

#### `base58CheckEncode(version: number, payload: Uint8Array): Promise<string>`
Base58Check 编码

#### `base58CheckDecode(str: string): Promise<{ version: number; payload: Uint8Array }>`
Base58Check 解码

#### `bech32Encode(hrp: string, version: number, program: Uint8Array): string`
Bech32/Bech32m 编码

#### `bech32Decode(addr: string): { hrp: string; version: number; program: Uint8Array; encoding: Bech32Encoding }`
Bech32/Bech32m 解码

## 支持的地址格式

| 格式 | 地址类型 | 示例 | 编码 |
|-----|---------|------|------|
| Bitcoin Legacy | P2PKH | `1...` | Base58Check |
| Bitcoin Legacy | P2SH | `3...` | Base58Check |
| Bitcoin SegWit | P2WPKH | `bc1q...` | Bech32 |
| Bitcoin SegWit | P2WSH | `bc1q...` | Bech32 |
| Bitcoin Taproot | P2TR | `bc1p...` | Bech32m |
| Dogecoin | P2PKH | `D...` | Base58Check |
| MVC | P2PKH | `1...` | Base58Check |

## 地址版本

```typescript
enum AddressVersion {
  P2PKH = 0,   // Pay-to-PubKey-Hash
  P2SH = 1,    // Pay-to-Script-Hash
  P2WPKH = 2,  // Pay-to-Witness-PubKey-Hash
  P2WSH = 3,   // Pay-to-Witness-Script-Hash
  P2MS = 4,    // Pay-to-Multisig
  P2TR = 5,    // Pay-to-Taproot
}
```

## IDAddress 格式

```
id + 版本字符 + 分隔符(1) + 数据 + 校验和

示例:
idq1caqw2z0gn5gx3c79ach7s0p8h7ra3ggetcxu2a
│││ │                                      │
│││ └─ 分隔符                               └─ 6字符校验和
││└─ 版本字符 (q=P2PKH)
│└─ HRP
└─ 前缀
```

版本字符映射:
- `q` → P2PKH
- `p` → P2SH
- `z` → P2WPKH
- `r` → P2WSH
- `y` → P2MS
- `t` → P2TR

## 构建

```bash
npm install
npm run build
```

## 测试

```bash
npm test
```

## 许可证

MIT License

## 相关资源

- [BIP173: Bech32 地址格式](https://github.com/bitcoin/bips/blob/master/bip-0173.mediawiki)
- [BIP350: Bech32m 格式](https://github.com/bitcoin/bips/blob/master/bip-0350.mediawiki)
- [BIP341: Taproot](https://github.com/bitcoin/bips/blob/master/bip-0341.mediawiki)
