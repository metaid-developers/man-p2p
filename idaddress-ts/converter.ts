/**
 * 地址转换函数
 * 在不同区块链地址格式和 IDAddress 之间转换
 */

import {
  AddressVersion,
  base58CheckDecode,
  base58CheckEncode,
  bech32Decode,
  bech32Encode,
} from './base';
import {
  encodeIDAddress,
  decodeIDAddress,
  IDAddressInfo,
} from './idaddress';

/**
 * 从 Bitcoin 地址转换为 IDAddress
 */
export function convertFromBitcoin(bitcoinAddr: string): string {
  // 尝试 Base58Check 解码 (传统地址)
  try {
    const { version, payload } = base58CheckDecode(bitcoinAddr);
    return convertFromLegacyBitcoin(version, payload);
  } catch (e) {
    // 继续尝试 Bech32
  }

  // 尝试 Bech32 解码 (SegWit 地址)
  try {
    const { hrp, version, program } = bech32Decode(bitcoinAddr);
    return convertFromSegWitBitcoin(hrp, version, program);
  } catch (e) {
    throw new Error(`Unsupported address format: ${bitcoinAddr}`);
  }
}

/**
 * 从传统 Bitcoin 地址转换
 */
function convertFromLegacyBitcoin(version: number, payload: Uint8Array): string {
  let idVersion: AddressVersion;

  switch (version) {
    case 0x00: // Bitcoin 主网 P2PKH
    case 0x6F: // Bitcoin 测试网 P2PKH
    case 0x1E: // Dogecoin 主网 P2PKH
      idVersion = AddressVersion.P2PKH;
      break;
    case 0x05: // Bitcoin 主网 P2SH
    case 0xC4: // Bitcoin 测试网 P2SH
    case 0x16: // Dogecoin 主网 P2SH
      idVersion = AddressVersion.P2SH;
      break;
    default:
      throw new Error(`Unsupported version byte: 0x${version.toString(16)}`);
  }

  return encodeIDAddress(idVersion, payload);
}

/**
 * 从 SegWit Bitcoin 地址转换
 */
function convertFromSegWitBitcoin(hrp: string, witnessVersion: number, program: Uint8Array): string {
  // 只支持主网和测试网
  if (hrp !== 'bc' && hrp !== 'tb') {
    throw new Error(`Unsupported network: ${hrp}`);
  }

  switch (witnessVersion) {
    case 0:
      // SegWit v0
      if (program.length === 20) {
        // P2WPKH
        return encodeIDAddress(AddressVersion.P2WPKH, program);
      } else if (program.length === 32) {
        // P2WSH
        return encodeIDAddress(AddressVersion.P2WSH, program);
      }
      throw new Error(`Invalid witness v0 program length: ${program.length}`);
    case 1:
      // Taproot (P2TR)
      if (program.length === 32) {
        return encodeIDAddress(AddressVersion.P2TR, program);
      }
      throw new Error(`Invalid taproot program length: ${program.length}`);
    default:
      throw new Error(`Unsupported witness version: ${witnessVersion}`);
  }
}

/**
 * 从 IDAddress 转换为 Bitcoin 地址
 */
export function convertToBitcoin(idAddr: string, network: 'mainnet' | 'testnet' = 'mainnet'): string {
  const info = decodeIDAddress(idAddr);

  // 确定 HRP
  const hrp = network === 'mainnet' ? 'bc' : 'tb';

  switch (info.version) {
    case AddressVersion.P2PKH: {
      // 传统 P2PKH 地址
      const version = network === 'mainnet' ? 0x00 : 0x6F;
      return base58CheckEncode(version, info.data);
    }

    case AddressVersion.P2SH: {
      // 传统 P2SH 地址
      const version = network === 'mainnet' ? 0x05 : 0xC4;
      return base58CheckEncode(version, info.data);
    }

    case AddressVersion.P2WPKH: {
      // SegWit v0 P2WPKH
      if (info.data.length !== 20) {
        throw new Error(`Invalid P2WPKH data length: ${info.data.length}`);
      }
      return bech32Encode(hrp, 0, info.data);
    }

    case AddressVersion.P2WSH: {
      // SegWit v0 P2WSH
      if (info.data.length !== 32) {
        throw new Error(`Invalid P2WSH data length: ${info.data.length}`);
      }
      return bech32Encode(hrp, 0, info.data);
    }

    case AddressVersion.P2TR: {
      // Taproot P2TR
      if (info.data.length !== 32) {
        throw new Error(`Invalid P2TR data length: ${info.data.length}`);
      }
      return bech32Encode(hrp, 1, info.data);
    }

    default:
      throw new Error(`Cannot convert version ${info.version} to Bitcoin address`);
  }
}

/**
 * 从 IDAddress 转换为 Dogecoin 地址
 */
export function convertToDogecoin(idAddr: string): string {
  const info = decodeIDAddress(idAddr);

  let version: number;
  switch (info.version) {
    case AddressVersion.P2PKH:
      version = 0x1E; // Dogecoin P2PKH
      break;
    case AddressVersion.P2SH:
      version = 0x16; // Dogecoin P2SH
      break;
    default:
      throw new Error(`Cannot convert version ${info.version} to Dogecoin address`);
  }

  return base58CheckEncode(version, info.data);
}

/**
 * 从 IDAddress 转换为 MVC 地址
 * MVC 使用与 Bitcoin 相同的版本字节
 */
export function convertToMVC(idAddr: string): string {
  return convertToBitcoin(idAddr, 'mainnet');
}

/**
 * 通用转换函数：将 GlobalMetaId (IDAddress) 转换为指定链的地址
 */
export function convertToGlobalMetaId(address: string): string {
  return convertFromBitcoin(address);
}

/**
 * 验证 GlobalMetaId 格式
 */
export function validateGlobalMetaId(globalMetaId: string): boolean {
  try {
    decodeIDAddress(globalMetaId);
    return true;
  } catch {
    return false;
  }
}

/**
 * 地址转换器类
 */
export class AddressConverter {
  private defaultNetwork: 'mainnet' | 'testnet';

  constructor(defaultNetwork: 'mainnet' | 'testnet' = 'mainnet') {
    this.defaultNetwork = defaultNetwork;
  }

  /**
   * 转换任意区块链地址到 ID 地址
   */
  toID(addr: string): string {
    return convertFromBitcoin(addr);
  }

  /**
   * 转换 ID 地址到指定网络的地址
   */
  fromID(idAddr: string, network?: string): string {
    const targetNetwork = network || this.defaultNetwork;

    switch (targetNetwork) {
      case 'bitcoin':
      case 'mainnet':
      case 'testnet':
        return convertToBitcoin(idAddr, targetNetwork as 'mainnet' | 'testnet');
      case 'dogecoin':
        return convertToDogecoin(idAddr);
      case 'mvc':
        return convertToMVC(idAddr);
      default:
        throw new Error(`Unsupported network: ${targetNetwork}`);
    }
  }

  /**
   * 批量转换地址
   */
  batch(addrs: string[]): Array<{ result: string | null; error: Error | null }> {
    return addrs.map((addr) => {
      try {
        const result = this.toID(addr);
        return { result, error: null };
      } catch (error) {
        return { result: null, error: error as Error };
      }
    });
  }
}
