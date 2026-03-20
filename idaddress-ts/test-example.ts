/**
 * 测试示例
 */

import {
  convertToGlobalMetaId,
  convertToBitcoin,
  convertToDogecoin,
  validateGlobalMetaId,
  AddressConverter,
  decodeIDAddress,
  getAddressType,
} from './index';

function runTests() {
  console.log('=== IDAddress TypeScript 测试 ===\n');

  // 测试 1: Bitcoin Legacy 地址
  console.log('【测试 1】Bitcoin Legacy P2PKH');
  const btcAddr = '1KAZD8sxTDkNzcjtCpKJ9ynPGsB8oryoFk';
  const btcGlobal = convertToGlobalMetaId(btcAddr);
  console.log(`地址: ${btcAddr}`);
  console.log(`GlobalMetaId: ${btcGlobal}`);
  const btcConverted = convertToBitcoin(btcGlobal, 'mainnet');
  console.log(`转换回: ${btcConverted}`);
  console.log(`✅ 匹配: ${btcAddr === btcConverted}\n`);

  // 测试 2: SegWit 地址
  console.log('【测试 2】Bitcoin SegWit P2WPKH');
  const segwitAddr = 'bc1qfxd3xp3q65ulewmzrfx50pxw45qjfvgdsfq4ah';
  const segwitGlobal = convertToGlobalMetaId(segwitAddr);
  console.log(`地址: ${segwitAddr}`);
  console.log(`GlobalMetaId: ${segwitGlobal}`);
  const segwitConverted = convertToBitcoin(segwitGlobal, 'mainnet');
  console.log(`转换回: ${segwitConverted}`);
  console.log(`✅ 匹配: ${segwitAddr === segwitConverted}\n`);

  // 测试 3: Taproot 地址
  console.log('【测试 3】Bitcoin Taproot P2TR');
  const taprootAddr = 'bc1pj93y5u8q8uszm5mktzu0hxs5keras3kzzt06n7e0ky0qenwdt0yqawfyy0';
  const taprootGlobal = convertToGlobalMetaId(taprootAddr);
  console.log(`地址: ${taprootAddr}`);
  console.log(`GlobalMetaId: ${taprootGlobal}`);
  const taprootConverted = convertToBitcoin(taprootGlobal, 'mainnet');
  console.log(`转换回: ${taprootConverted}`);
  console.log(`✅ 匹配: ${taprootAddr === taprootConverted}\n`);

  // 测试 4: Dogecoin 地址
  console.log('【测试 4】Dogecoin P2PKH');
  const dogeAddr = 'DFo712BpLysLsoF6kSjTN6pPmZXxibtWcG';
  const dogeGlobal = convertToGlobalMetaId(dogeAddr);
  console.log(`地址: ${dogeAddr}`);
  console.log(`GlobalMetaId: ${dogeGlobal}`);
  const dogeConverted = convertToDogecoin(dogeGlobal);
  console.log(`转换回: ${dogeConverted}`);
  console.log(`✅ 匹配: ${dogeAddr === dogeConverted}\n`);

  // 测试 5: 跨链身份统一
  console.log('【测试 5】跨链身份统一');
  const mvcAddr = '195gtuVbW9DsKPnSZLrt9kdJrQmvrAt7e3';
  const dogeAddr2 = 'DDDnSASEoZ89rPy3HvrShWnujYWEABGhUB';
  
  const mvcGlobal = convertToGlobalMetaId(mvcAddr);
  const dogeGlobal2 = convertToGlobalMetaId(dogeAddr2);
  
  console.log(`MVC 地址: ${mvcAddr}`);
  console.log(`MVC GlobalMetaId: ${mvcGlobal}`);
  console.log(`Dogecoin 地址: ${dogeAddr2}`);
  console.log(`Dogecoin GlobalMetaId: ${dogeGlobal2}`);
  console.log(`✅ 相同: ${mvcGlobal === dogeGlobal2}`);
  console.log(`统一 GlobalMetaId: ${mvcGlobal}\n`);

  // 测试 6: 地址验证
  console.log('【测试 6】地址验证');
  console.log(`验证 "${btcGlobal}": ${validateGlobalMetaId(btcGlobal)}`);
  console.log(`验证 "invalid-address": ${validateGlobalMetaId('invalid-address')}\n`);

  // 测试 7: 解码地址信息
  console.log('【测试 7】解码地址信息');
  const info = decodeIDAddress(btcGlobal);
  console.log(`GlobalMetaId: ${btcGlobal}`);
  console.log(`版本: ${info.version} (${getAddressType(info.version)})`);
  console.log(`数据长度: ${info.data.length} bytes`);
  console.log(`数据(hex): ${Buffer.from(info.data).toString('hex')}\n`);

  // 测试 8: 使用转换器类
  console.log('【测试 8】使用 AddressConverter 类');
  const converter = new AddressConverter('mainnet');
  
  const addresses = [
    '1KAZD8sxTDkNzcjtCpKJ9ynPGsB8oryoFk',
    'DFo712BpLysLsoF6kSjTN6pPmZXxibtWcG',
    'bc1qfxd3xp3q65ulewmzrfx50pxw45qjfvgdsfq4ah',
  ];

  const results = converter.batch(addresses);
  results.forEach((r, i) => {
    if (r.result) {
      console.log(`✅ ${addresses[i]} -> ${r.result}`);
    } else {
      console.log(`❌ ${addresses[i]} 失败: ${r.error?.message}`);
    }
  });

  console.log('\n=== 所有测试完成 ===');
}

// 运行测试
runTests();
