/**
 * IDAddress 编解码实现
 */

import {
  AddressVersion,
  IDAddressInfo,
  getAddressType,
} from './base';

/**
 * IDAddress 字符集 (Bech32 字符集)
 */
const IDADDRESS_CHARSET = 'qpzry9x8gf2tvdw0s3jn54khce6mua7l';

/**
 * IDAddress 字符映射
 */
const IDADDRESS_CHARSET_MAP: Record<string, number> = {};
for (let i = 0; i < IDADDRESS_CHARSET.length; i++) {
  IDADDRESS_CHARSET_MAP[IDADDRESS_CHARSET[i]] = i;
}

/**
 * 版本号到字符的映射
 */
const VERSION_CHARS = ['q', 'p', 'z', 'r', 'y', 't'];

/**
 * 字符到版本号的映射
 */
const CHAR_TO_VERSION: Record<string, AddressVersion> = {
  'q': AddressVersion.P2PKH,
  'p': AddressVersion.P2SH,
  'z': AddressVersion.P2WPKH,
  'r': AddressVersion.P2WSH,
  'y': AddressVersion.P2MS,
  't': AddressVersion.P2TR,
};

/**
 * Polymod 算法 (用于 BCH 校验和)
 */
function polymod(values: number[]): number {
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
function hrpExpand(hrp: string): number[] {
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
 * 创建校验和
 */
function createChecksum(data: number[]): number[] {
  const hrp = 'id';
  const values = [...hrpExpand(hrp), ...data, 0, 0, 0, 0, 0, 0];
  const mod = polymod(values) ^ 1;
  
  const checksum: number[] = [];
  for (let i = 0; i < 6; i++) {
    checksum.push((mod >> (5 * (5 - i))) & 31);
  }
  return checksum;
}

/**
 * 验证校验和
 */
function verifyChecksum(data: number[]): boolean {
  const hrp = 'id';
  const values = [...hrpExpand(hrp), ...data];
  return polymod(values) === 1;
}

/**
 * 转换 8 位到 5 位
 */
function convertBits8to5(data: Uint8Array): number[] {
  let acc = 0;
  let bits = 0;
  const result: number[] = [];
  const maxv = 31; // (1 << 5) - 1

  for (const value of data) {
    acc = (acc << 8) | value;
    bits += 8;
    while (bits >= 5) {
      bits -= 5;
      result.push((acc >> bits) & maxv);
    }
  }

  if (bits > 0) {
    result.push((acc << (5 - bits)) & maxv);
  }

  return result;
}

/**
 * 转换 5 位到 8 位
 */
function convertBits5to8(data: number[]): Uint8Array {
  let acc = 0;
  let bits = 0;
  const result: number[] = [];
  const maxv = 255; // (1 << 8) - 1

  for (const value of data) {
    if (value < 0 || value >> 5 !== 0) {
      throw new Error('Invalid data value');
    }
    acc = (acc << 5) | value;
    bits += 5;
    while (bits >= 8) {
      bits -= 8;
      result.push((acc >> bits) & maxv);
    }
  }

  if (bits >= 5 || ((acc << (8 - bits)) & maxv) !== 0) {
    throw new Error('Invalid padding');
  }

  return new Uint8Array(result);
}

/**
 * 编码 IDAddress
 */
export function encodeIDAddress(version: AddressVersion, data: Uint8Array): string {
  if (version < 0 || version > 5) {
    throw new Error(`Invalid version: ${version}`);
  }

  // 转换数据为 5 位编码
  const converted = convertBits8to5(data);

  // 添加版本
  const dataWithVersion = [version, ...converted];

  // 计算校验和
  const checksum = createChecksum(dataWithVersion);

  // 组合最终数据
  const finalData = [...dataWithVersion, ...checksum];

  // 构建地址
  const versionChar = VERSION_CHARS[version];
  let result = 'id' + versionChar + '1';
  for (const d of finalData) {
    result += IDADDRESS_CHARSET[d];
  }

  return result;
}

/**
 * 解码 IDAddress
 */
export function decodeIDAddress(addr: string): IDAddressInfo {
  // 转换为小写
  addr = addr.toLowerCase();

  // 验证前缀
  if (!addr.startsWith('id')) {
    throw new Error('Invalid IDAddress: must start with "id"');
  }

  // 验证并提取版本字符
  const versionChar = addr[2];
  const version = CHAR_TO_VERSION[versionChar];
  if (version === undefined) {
    throw new Error(`Invalid version character: ${versionChar}`);
  }

  // 验证分隔符
  if (addr[3] !== '1') {
    throw new Error('Invalid IDAddress: missing separator');
  }

  // 解码数据部分
  const dataStr = addr.slice(4);
  const decoded: number[] = [];
  for (const char of dataStr) {
    const val = IDADDRESS_CHARSET_MAP[char];
    if (val === undefined) {
      throw new Error(`Invalid character: ${char}`);
    }
    decoded.push(val);
  }

  // 验证长度
  if (decoded.length < 7) { // 至少需要版本 + 一些数据 + 6字符校验和
    throw new Error('IDAddress too short');
  }

  // 验证校验和
  if (!verifyChecksum(decoded)) {
    throw new Error('Invalid checksum');
  }

  // 移除校验和
  const dataWithoutChecksum = decoded.slice(0, -6);

  // 验证版本
  if (dataWithoutChecksum[0] !== version) {
    throw new Error('Version mismatch');
  }

  // 转换数据
  const data = convertBits5to8(dataWithoutChecksum.slice(1));

  return { version, data };
}

/**
 * 验证 IDAddress 格式
 */
export function validateIDAddress(addr: string): boolean {
  try {
    decodeIDAddress(addr);
    return true;
  } catch {
    return false;
  }
}

export { AddressVersion, IDAddressInfo, getAddressType };
