/**
 * IDAddress TypeScript Library
 * 入口文件 - 导出所有公共 API
 */

// 基础类型和函数
export {
  AddressVersion,
  Bech32Encoding,
  IDAddressInfo,
  getAddressType,
  base58Encode,
  base58Decode,
  base58CheckEncode,
  base58CheckDecode,
  bech32Encode,
  bech32Decode,
} from './base';

// IDAddress 编解码
export {
  encodeIDAddress,
  decodeIDAddress,
  validateIDAddress,
} from './idaddress';

// 地址转换
export {
  convertFromBitcoin,
  convertToBitcoin,
  convertToDogecoin,
  convertToMVC,
  convertToGlobalMetaId,
  validateGlobalMetaId,
  AddressConverter,
} from './converter';

/**
 * 使用示例：
 * 
 * ```typescript
 * import { convertToGlobalMetaId, convertToBitcoin } from 'idaddress';
 * 
 * // Bitcoin Legacy 地址 -> GlobalMetaId
 * const globalId = await convertToGlobalMetaId('1KAZD8sxTDkNzcjtCpKJ9ynPGsB8oryoFk');
 * console.log(globalId); // idq1caqw2z0gn5gx3c79ach7s0p8h7ra3ggetcxu2a
 * 
 * // GlobalMetaId -> Bitcoin 地址
 * const btcAddr = await convertToBitcoin(globalId, 'mainnet');
 * console.log(btcAddr); // 1KAZD8sxTDkNzcjtCpKJ9ynPGsB8oryoFk
 * 
 * // Dogecoin 地址 -> GlobalMetaId
 * const dogeGlobal = await convertToGlobalMetaId('DFo712BpLysLsoF6kSjTN6pPmZXxibtWcG');
 * console.log(dogeGlobal); // idq1wnshx9kvz379ssjz38ku4q7vdwz36lg5hlr2gy
 * 
 * // SegWit 地址 -> GlobalMetaId
 * const segwitGlobal = await convertToGlobalMetaId('bc1qfxd3xp3q65ulewmzrfx50pxw45qjfvgdsfq4ah');
 * console.log(segwitGlobal); // idz1fxd3xp3q65ulewmzrfx50pxw45qjfvgd0j24uz
 * 
 * // Taproot 地址 -> GlobalMetaId
 * const taprootGlobal = await convertToGlobalMetaId('bc1pj93y5u8q8uszm5mktzu0hxs5keras3kzzt06n7e0ky0qenwdt0yqawfyy0');
 * console.log(taprootGlobal); // idt1j93y5u8q8uszm5mktzu0hxs5keras3kzzt06n7e0ky0qenwdt0yqqk89qy
 * ```
 */
