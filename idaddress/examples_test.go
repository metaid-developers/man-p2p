package idaddress

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// Example_encodeP2PKH 演示如何创建P2PKH地址
func Example_encodeP2PKH() {
	// 公钥哈希 (20字节)
	pubkeyHash, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")

	// 编码为ID地址
	addr, err := EncodeIDAddress(VersionP2PKH, pubkeyHash)
	if err != nil {
		panic(err)
	}

	fmt.Println("P2PKH Address:", addr)
	// Output: P2PKH Address: idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh
}

// Example_decodeAddress 演示如何解码ID地址
func Example_decodeAddress() {
	addr := "idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh"

	// 解码地址
	info, err := DecodeIDAddress(addr)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Version: %d (%s)\n", info.Version, GetAddressType(info.Version))
	fmt.Printf("Data: %x\n", info.Data)
}

// Example_createMultisigAddress 演示如何创建多签地址
func Example_createMultisigAddress() {
	// 三个压缩公钥
	pubkey1, _ := hex.DecodeString("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	pubkey2, _ := hex.DecodeString("02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5")
	pubkey3, _ := hex.DecodeString("03774ae7f858a9411e5ef4246b70c65aac5649980be5c17891bbec17895da008cb")

	pubkeys := [][]byte{pubkey1, pubkey2, pubkey3}

	// 创建2-of-3多签地址
	addr, err := NewP2MSAddress(2, 3, pubkeys)
	if err != nil {
		panic(err)
	}

	fmt.Println("2-of-3 Multisig Address:", addr)

	// 解码并提取信息
	info, _ := DecodeIDAddress(addr)
	m, n, _, _ := ExtractMultisigInfo(info)
	fmt.Printf("Multisig: %d-of-%d\n", m, n)
}

// Example_fromPublicKey 演示从公钥生成地址
func Example_fromPublicKey() {
	// 压缩公钥 (33字节)
	pubkey, _ := hex.DecodeString("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")

	// 生成P2PKH地址
	p2pkhAddr, _ := NewP2PKHAddress(pubkey)
	fmt.Println("P2PKH:", p2pkhAddr)

	// 生成P2WPKH地址
	p2wpkhAddr, _ := NewP2WPKHAddress(pubkey)
	fmt.Println("P2WPKH:", p2wpkhAddr)
}

// Example_validateAddress 演示地址验证
func Example_validateAddress() {
	addresses := []string{
		"idq1w508d6qejxtdg4y5r3zarvary0c5xw7k0aqkue", // 有效
		"idq1w508d6qejxtdg4y5r3zarvary0c5xw7k0aqkuf", // 无效校验和
		"invalid", // 无效格式
	}

	for _, addr := range addresses {
		if ValidateIDAddress(addr) {
			fmt.Printf("%s: valid\n", addr)
		} else {
			fmt.Printf("%s: invalid\n", addr)
		}
	}
}

// Example_convertBetweenFormats 演示不同格式之间的转换
func Example_convertBetweenFormats() {
	// 原始数据
	data := []byte{0x75, 0x1e, 0x76, 0xe8, 0x19, 0x91, 0x96, 0xd4,
		0x54, 0x94, 0x1c, 0x45, 0xd1, 0xb3, 0xa3, 0x23, 0xf1, 0x43, 0x3b, 0xd6}

	// 转换为ID地址
	idAddr, _ := EncodeIDAddress(VersionP2PKH, data)
	fmt.Println("ID Address:", idAddr)

	// 转换为Hex
	fmt.Println("Hex:", hex.EncodeToString(data))

	// 转换为Base64
	fmt.Println("Base64:", base64.StdEncoding.EncodeToString(data))
}

// Example_witnessAddresses 演示见证地址的使用
func Example_witnessAddresses() {
	// 创建P2WPKH地址 (SegWit v0 - 公钥哈希)
	pubkeyHash, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")
	p2wpkhAddr, _ := EncodeIDAddress(VersionP2WPKH, pubkeyHash)
	fmt.Println("P2WPKH:", p2wpkhAddr)

	// 创建P2WSH地址 (SegWit v0 - 脚本哈希)
	scriptHash, _ := hex.DecodeString("1863143c14c5166804bd19203356da136c985678cd4d27a1b8c6329604903262")
	p2wshAddr, _ := EncodeIDAddress(VersionP2WSH, scriptHash)
	fmt.Println("P2WSH:", p2wshAddr)
}

// Example_errorHandling 演示错误处理
func Example_errorHandling() {
	// 无效的数据长度
	shortData := []byte{0x01, 0x02, 0x03}
	_, err := EncodeIDAddress(VersionP2PKH, shortData)
	if err != nil {
		fmt.Println("Error:", err)
	}

	// 无效的版本
	validData := make([]byte, 20)
	_, err = EncodeIDAddress(99, validData)
	if err != nil {
		fmt.Println("Error:", err)
	}

	// 解码无效地址
	_, err = DecodeIDAddress("invalid_address")
	if err != nil {
		fmt.Println("Error:", err)
	}
}

// Example_multisigWorkflow 演示完整的多签工作流程
func Example_multisigWorkflow() {
	// 1. 准备参与者的公钥
	alice, _ := hex.DecodeString("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	bob, _ := hex.DecodeString("02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5")
	carol, _ := hex.DecodeString("03774ae7f858a9411e5ef4246b70c65aac5649980be5c17891bbec17895da008cb")

	// 2. 创建2-of-3多签地址
	pubkeys := [][]byte{alice, bob, carol}
	multisigAddr, _ := NewP2MSAddress(2, 3, pubkeys)
	fmt.Println("Multisig Address:", multisigAddr)

	// 3. 验证地址
	isValid := ValidateIDAddress(multisigAddr)
	fmt.Println("Valid:", isValid)

	// 4. 解码地址并提取信息
	info, _ := DecodeIDAddress(multisigAddr)
	fmt.Printf("Address Type: %s\n", GetAddressType(info.Version))

	// 5. 提取多签参数
	m, n, extractedKeys, _ := ExtractMultisigInfo(info)
	fmt.Printf("Required Signatures: %d-of-%d\n", m, n)
	fmt.Printf("Number of Pubkeys: %d\n", len(extractedKeys))
}
