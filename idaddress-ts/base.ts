/**
 * IDAddress TypeScript Implementation
 * 跨链地址统一标识系统
 */

/**
 * 地址版本类型
 */
export enum AddressVersion {
  P2PKH = 0,   // Pay-to-PubKey-Hash
  P2SH = 1,    // Pay-to-Script-Hash
  P2WPKH = 2,  // Pay-to-Witness-PubKey-Hash
  P2WSH = 3,   // Pay-to-Witness-Script-Hash
  P2MS = 4,    // Pay-to-Multisig
  P2TR = 5,    // Pay-to-Taproot
}

/**
 * 地址类型名称映射
 */
const ADDRESS_TYPE_NAMES: Record<AddressVersion, string> = {
  [AddressVersion.P2PKH]: 'Pay-to-PubKey-Hash',
  [AddressVersion.P2SH]: 'Pay-to-Script-Hash',
  [AddressVersion.P2WPKH]: 'Pay-to-Witness-PubKey-Hash',
  [AddressVersion.P2WSH]: 'Pay-to-Witness-Script-Hash',
  [AddressVersion.P2MS]: 'Pay-to-Multisig',
  [AddressVersion.P2TR]: 'Pay-to-Taproot',
};

/**
 * IDAddress 信息
 */
export interface IDAddressInfo {
  version: AddressVersion;
  data: Uint8Array;
}

/**
 * Bech32 编码类型
 */
export enum Bech32Encoding {
  Bech32 = 1,   // SegWit v0
  Bech32m = 2,  // SegWit v1+ (Taproot)
}

/**
 * 获取地址类型名称
 */
export function getAddressType(version: AddressVersion): string {
  return ADDRESS_TYPE_NAMES[version] || 'Unknown';
}

import * as crypto from 'crypto';

/**
 * SHA256 哈希
 */
function sha256(data: Uint8Array): Uint8Array {
  return new Uint8Array(crypto.createHash('sha256').update(data).digest());
}

/**
 * 双重 SHA256 哈希 (用于 Base58Check)
 */
function doubleSHA256(data: Uint8Array): Uint8Array {
  const first = sha256(data);
  return sha256(first);
}

/**
 * Base58 字符集
 */
const BASE58_ALPHABET = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';

/**
 * Base58 编码
 */
export function base58Encode(data: Uint8Array): string {
  if (data.length === 0) return '';

  // 转换为大整数
  let num = BigInt(0);
  for (const byte of data) {
    num = num * BigInt(256) + BigInt(byte);
  }

  // 转换为 Base58
  let result = '';
  while (num > 0) {
    const remainder = Number(num % BigInt(58));
    result = BASE58_ALPHABET[remainder] + result;
    num = num / BigInt(58);
  }

  // 添加前导零
  for (const byte of data) {
    if (byte === 0) {
      result = '1' + result;
    } else {
      break;
    }
  }

  return result;
}

/**
 * Base58 解码
 */
export function base58Decode(str: string): Uint8Array {
  if (str.length === 0) return new Uint8Array(0);

  // 转换为大整数
  let num = BigInt(0);
  for (const char of str) {
    const index = BASE58_ALPHABET.indexOf(char);
    if (index === -1) {
      throw new Error(`Invalid Base58 character: ${char}`);
    }
    num = num * BigInt(58) + BigInt(index);
  }

  // 转换为字节数组
  const bytes: number[] = [];
  while (num > 0) {
    bytes.unshift(Number(num % BigInt(256)));
    num = num / BigInt(256);
  }

  // 添加前导零
  for (const char of str) {
    if (char === '1') {
      bytes.unshift(0);
    } else {
      break;
    }
  }

  return new Uint8Array(bytes);
}

/**
 * Base58Check 编码
 */
export function base58CheckEncode(version: number, payload: Uint8Array): string {
  // 组合版本和载荷
  const data = new Uint8Array(1 + payload.length);
  data[0] = version;
  data.set(payload, 1);

  // 计算校验和
  const checksum = doubleSHA256(data);

  // 添加校验和
  const result = new Uint8Array(data.length + 4);
  result.set(data);
  result.set(checksum.slice(0, 4), data.length);

  return base58Encode(result);
}

/**
 * Base58Check 解码
 */
export function base58CheckDecode(str: string): { version: number; payload: Uint8Array } {
  const decoded = base58Decode(str);

  if (decoded.length < 5) {
    throw new Error('Decoded data too short');
  }

  // 提取数据和校验和
  const data = decoded.slice(0, -4);
  const checksum = decoded.slice(-4);

  // 验证校验和
  const expectedChecksum = doubleSHA256(data);
  for (let i = 0; i < 4; i++) {
    if (checksum[i] !== expectedChecksum[i]) {
      throw new Error('Checksum mismatch');
    }
  }

  return {
    version: data[0],
    payload: data.slice(1),
  };
}

/**
 * Bech32 字符集
 */
const BECH32_CHARSET = 'qpzry9x8gf2tvdw0s3jn54khce6mua7l';

/**
 * Bech32 字符映射
 */
const BECH32_CHARSET_MAP: Record<string, number> = {};
for (let i = 0; i < BECH32_CHARSET.length; i++) {
  BECH32_CHARSET_MAP[BECH32_CHARSET[i]] = i;
}

/**
 * Bech32 Polymod 算法
 */
function bech32Polymod(values: number[]): number {
  const gen = [0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3];
  
  let chk = 1;
  for (const v of values) {
    const b = chk >> 25;
    chk = ((chk & 0x1ffffff) << 5) ^ v;
    for (let i = 0; i < 5; i++) {
      if ((b >> i) & 1) {
        chk ^= gen[i];
      }
    }
  }
  return chk;
}

/**
 * 扩展 HRP
 */
function bech32HrpExpand(hrp: string): number[] {
  const result: number[] = [];
  for (let i = 0; i < hrp.length; i++) {
    result.push(hrp.charCodeAt(i) >> 5);
  }
  result.push(0);
  for (let i = 0; i < hrp.length; i++) {
    result.push(hrp.charCodeAt(i) & 31);
  }
  return result;
}

/**
 * 验证 Bech32 校验和
 */
function bech32VerifyChecksum(hrp: string, data: number[], encoding: Bech32Encoding): boolean {
  const values = [...bech32HrpExpand(hrp), ...data];
  const polymod = bech32Polymod(values);
  
  const bech32Const = 1;
  const bech32mConst = 0x2bc830a3;
  
  return encoding === Bech32Encoding.Bech32 
    ? polymod === bech32Const 
    : polymod === bech32mConst;
}

/**
 * 转换位宽 (用于 Bech32)
 */
function convertBits(data: Uint8Array, fromBits: number, toBits: number, pad: boolean): Uint8Array {
  let acc = 0;
  let bits = 0;
  const result: number[] = [];
  const maxv = (1 << toBits) - 1;

  for (const value of data) {
    acc = (acc << fromBits) | value;
    bits += fromBits;
    while (bits >= toBits) {
      bits -= toBits;
      result.push((acc >> bits) & maxv);
    }
  }

  if (pad) {
    if (bits > 0) {
      result.push((acc << (toBits - bits)) & maxv);
    }
  } else if (bits >= fromBits || ((acc << (toBits - bits)) & maxv) !== 0) {
    throw new Error('Invalid padding');
  }

  return new Uint8Array(result);
}

/**
 * Bech32 解码
 */
export function bech32Decode(addr: string): {
  hrp: string;
  version: number;
  program: Uint8Array;
  encoding: Bech32Encoding;
} {
  // 转换为小写
  addr = addr.toLowerCase();

  // 查找分隔符
  const pos = addr.lastIndexOf('1');
  if (pos < 1 || pos + 7 > addr.length || addr.length > 90) {
    throw new Error('Invalid bech32 address format');
  }

  // 分离 HRP 和数据部分
  const hrp = addr.slice(0, pos);
  const data = addr.slice(pos + 1);

  // 解码数据
  const decoded: number[] = [];
  for (const char of data) {
    const val = BECH32_CHARSET_MAP[char];
    if (val === undefined) {
      throw new Error(`Invalid bech32 character: ${char}`);
    }
    decoded.push(val);
  }

  // 尝试验证校验和 (先尝试 Bech32m，再尝试 Bech32)
  let encoding = Bech32Encoding.Bech32m;
  if (!bech32VerifyChecksum(hrp, decoded, Bech32Encoding.Bech32m)) {
    encoding = Bech32Encoding.Bech32;
    if (!bech32VerifyChecksum(hrp, decoded, Bech32Encoding.Bech32)) {
      throw new Error('Invalid bech32 checksum');
    }
  }

  // 移除校验和（最后6个字符）
  const dataWithoutChecksum = decoded.slice(0, -6);

  if (dataWithoutChecksum.length < 1) {
    throw new Error('Invalid bech32 data length');
  }

  // 第一个字节是见证版本
  const version = dataWithoutChecksum[0];

  // 转换剩余数据从 5 位到 8 位
  const program = convertBits(
    new Uint8Array(dataWithoutChecksum.slice(1)),
    5,
    8,
    false
  );

  // 验证程序长度
  if (program.length < 2 || program.length > 40) {
    throw new Error('Invalid witness program length');
  }

  // 验证版本和编码的匹配
  if (version === 0 && encoding !== Bech32Encoding.Bech32) {
    throw new Error('Witness version 0 must use bech32');
  }
  if (version !== 0 && encoding !== Bech32Encoding.Bech32m) {
    throw new Error('Witness version 1+ must use bech32m');
  }

  return { hrp, version, program, encoding };
}

/**
 * Bech32 编码
 */
export function bech32Encode(hrp: string, version: number, program: Uint8Array): string {
  // 选择编码类型
  const encoding = version === 0 ? Bech32Encoding.Bech32 : Bech32Encoding.Bech32m;

  // 验证程序长度
  if (program.length < 2 || program.length > 40) {
    throw new Error('Invalid witness program length');
  }

  // 转换程序从 8 位到 5 位
  const converted = convertBits(program, 8, 5, true);

  // 添加见证版本
  const data = [version, ...Array.from(converted)];

  // 计算校验和
  const values = [...bech32HrpExpand(hrp), ...data, 0, 0, 0, 0, 0, 0];

  const bech32Const = 1;
  const bech32mConst = 0x2bc830a3;

  let polymod = bech32Polymod(values);
  if (encoding === Bech32Encoding.Bech32) {
    polymod ^= bech32Const;
  } else {
    polymod ^= bech32mConst;
  }

  // 提取校验和
  const checksum: number[] = [];
  for (let i = 0; i < 6; i++) {
    checksum.push((polymod >> (5 * (5 - i))) & 31);
  }

  // 组合最终地址
  data.push(...checksum);
  let result = hrp + '1';
  for (const d of data) {
    result += BECH32_CHARSET[d];
  }

  return result;
}

// IDAddress 相关常量和函数将在下一个文件中继续...
