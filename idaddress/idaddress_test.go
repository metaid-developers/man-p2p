package idaddress

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestEncodeDecodeP2PKH(t *testing.T) {
	// 测试数据: 20字节公钥哈希
	pubkeyHash, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")

	// 编码
	addr, err := EncodeIDAddress(VersionP2PKH, pubkeyHash)
	if err != nil {
		t.Fatalf("EncodeIDAddress failed: %v", err)
	}

	t.Logf("P2PKH Address: %s", addr)

	// 验证地址以正确的前缀开头
	if len(addr) < 4 || addr[:4] != "idq1" {
		t.Errorf("Expected address to start with 'idq1', got %s", addr)
	}

	// 解码
	info, err := DecodeIDAddress(addr)
	if err != nil {
		t.Fatalf("DecodeIDAddress failed: %v", err)
	}

	// 验证版本
	if info.Version != VersionP2PKH {
		t.Errorf("Expected version %d, got %d", VersionP2PKH, info.Version)
	}

	// 验证数据
	if !bytes.Equal(info.Data, pubkeyHash) {
		t.Errorf("Data mismatch.\nExpected: %x\nGot: %x", pubkeyHash, info.Data)
	}
}

func TestEncodeDecodeP2SH(t *testing.T) {
	scriptHash, _ := hex.DecodeString("89abcdefabbaabbaabbaabbaabbaabbaabbaabba")

	addr, err := EncodeIDAddress(VersionP2SH, scriptHash)
	if err != nil {
		t.Fatalf("EncodeIDAddress failed: %v", err)
	}

	t.Logf("P2SH Address: %s", addr)

	if len(addr) < 4 || addr[:4] != "idp1" {
		t.Errorf("Expected address to start with 'idp1', got %s", addr)
	}

	info, err := DecodeIDAddress(addr)
	if err != nil {
		t.Fatalf("DecodeIDAddress failed: %v", err)
	}

	if info.Version != VersionP2SH {
		t.Errorf("Expected version %d, got %d", VersionP2SH, info.Version)
	}

	if !bytes.Equal(info.Data, scriptHash) {
		t.Errorf("Data mismatch")
	}
}

func TestEncodeDecodeP2WPKH(t *testing.T) {
	witnessHash, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")

	addr, err := EncodeIDAddress(VersionP2WPKH, witnessHash)
	if err != nil {
		t.Fatalf("EncodeIDAddress failed: %v", err)
	}

	t.Logf("P2WPKH Address: %s", addr)

	if len(addr) < 4 || addr[:4] != "idz1" {
		t.Errorf("Expected address to start with 'idz1', got %s", addr)
	}

	info, err := DecodeIDAddress(addr)
	if err != nil {
		t.Fatalf("DecodeIDAddress failed: %v", err)
	}

	if info.Version != VersionP2WPKH {
		t.Errorf("Expected version %d, got %d", VersionP2WPKH, info.Version)
	}

	if !bytes.Equal(info.Data, witnessHash) {
		t.Errorf("Data mismatch")
	}
}

func TestEncodeDecodeP2WSH(t *testing.T) {
	witnessScriptHash, _ := hex.DecodeString("1863143c14c5166804bd19203356da136c985678cd4d27a1b8c6329604903262")

	addr, err := EncodeIDAddress(VersionP2WSH, witnessScriptHash)
	if err != nil {
		t.Fatalf("EncodeIDAddress failed: %v", err)
	}

	t.Logf("P2WSH Address: %s", addr)

	if len(addr) < 4 || addr[:4] != "idr1" {
		t.Errorf("Expected address to start with 'idr1', got %s", addr)
	}

	info, err := DecodeIDAddress(addr)
	if err != nil {
		t.Fatalf("DecodeIDAddress failed: %v", err)
	}

	if info.Version != VersionP2WSH {
		t.Errorf("Expected version %d, got %d", VersionP2WSH, info.Version)
	}

	if !bytes.Equal(info.Data, witnessScriptHash) {
		t.Errorf("Data mismatch")
	}
}

func TestNewP2MSAddress(t *testing.T) {
	// 创建3个压缩公钥 (仅用于测试)
	pubkey1, _ := hex.DecodeString("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	pubkey2, _ := hex.DecodeString("02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5")
	pubkey3, _ := hex.DecodeString("03774ae7f858a9411e5ef4246b70c65aac5649980be5c17891bbec17895da008cb")

	pubkeys := [][]byte{pubkey1, pubkey2, pubkey3}

	// 创建2-of-3多签地址
	addr, err := NewP2MSAddress(2, 3, pubkeys)
	if err != nil {
		t.Fatalf("NewP2MSAddress failed: %v", err)
	}

	t.Logf("P2MS Address (2-of-3): %s", addr)

	if len(addr) < 4 || addr[:4] != "idy1" {
		t.Errorf("Expected address to start with 'idy1', got %s", addr)
	}

	// 解码并验证
	info, err := DecodeIDAddress(addr)
	if err != nil {
		t.Fatalf("DecodeIDAddress failed: %v", err)
	}

	if info.Version != VersionP2MS {
		t.Errorf("Expected version %d, got %d", VersionP2MS, info.Version)
	}

	// 提取多签信息
	m, n, extractedPubkeys, err := ExtractMultisigInfo(info)
	if err != nil {
		t.Fatalf("ExtractMultisigInfo failed: %v", err)
	}

	if m != 2 {
		t.Errorf("Expected m=2, got %d", m)
	}
	if n != 3 {
		t.Errorf("Expected n=3, got %d", n)
	}

	for i := 0; i < 3; i++ {
		if !bytes.Equal(extractedPubkeys[i], pubkeys[i]) {
			t.Errorf("Pubkey %d mismatch", i)
		}
	}
}

func TestNewP2PKHAddress(t *testing.T) {
	// 测试公钥 (压缩格式)
	pubkey, _ := hex.DecodeString("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")

	addr, err := NewP2PKHAddress(pubkey)
	if err != nil {
		t.Fatalf("NewP2PKHAddress failed: %v", err)
	}

	t.Logf("Generated P2PKH Address: %s", addr)

	// 验证地址
	if !ValidateIDAddress(addr) {
		t.Error("Address validation failed")
	}
}

func TestNewP2SHAddress(t *testing.T) {
	// 简单的脚本 (用于测试)
	script := []byte{0x00, 0x14, 0x75, 0x1e, 0x76, 0xe8, 0x19, 0x91, 0x96, 0xd4,
		0x54, 0x94, 0x1c, 0x45, 0xd1, 0xb3, 0xa3, 0x23, 0xf1, 0x43, 0x3b, 0xd6}

	addr, err := NewP2SHAddress(script)
	if err != nil {
		t.Fatalf("NewP2SHAddress failed: %v", err)
	}

	t.Logf("Generated P2SH Address: %s", addr)

	if !ValidateIDAddress(addr) {
		t.Error("Address validation failed")
	}
}

func TestValidateIDAddress(t *testing.T) {
	validTests := []struct {
		name    string
		version AddressVersion
		data    string
	}{
		{"P2PKH", VersionP2PKH, "751e76e8199196d454941c45d1b3a323f1433bd6"},
		{"P2SH", VersionP2SH, "89abcdefabbaabbaabbaabbaabbaabbaabbaabba"},
	}

	for _, tt := range validTests {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := hex.DecodeString(tt.data)
			addr, _ := EncodeIDAddress(tt.version, data)

			if !ValidateIDAddress(addr) {
				t.Errorf("Valid address failed validation: %s", addr)
			}
		})
	}

	// 测试无效地址
	invalidAddresses := []string{
		"invalid",
		"id11qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq",  // 错误的HRP
		"idq1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqz", // 错误的校验和
		"idq1", // 太短
		"idb1qpzry9x8gf2tvdw0s3jn54khce6mua7lx2d3qm", // 无效版本
	}

	for _, addr := range invalidAddresses {
		if ValidateIDAddress(addr) {
			t.Errorf("Invalid address passed validation: %s", addr)
		}
	}
}

func TestInvalidVersions(t *testing.T) {
	data := make([]byte, 20)

	_, err := EncodeIDAddress(99, data)
	if err == nil {
		t.Error("Expected error for invalid version")
	}
}

func TestInvalidDataLength(t *testing.T) {
	tests := []struct {
		name    string
		version AddressVersion
		length  int
	}{
		{"P2PKH wrong length", VersionP2PKH, 19},
		{"P2SH wrong length", VersionP2SH, 21},
		{"P2WSH wrong length", VersionP2WSH, 31},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, tt.length)
			_, err := EncodeIDAddress(tt.version, data)
			if err == nil {
				t.Error("Expected error for invalid data length")
			}
		})
	}
}

func TestChecksumDetection(t *testing.T) {
	pubkeyHash, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")
	addr, _ := EncodeIDAddress(VersionP2PKH, pubkeyHash)

	// 修改地址中的一个字符
	modified := []byte(addr)
	if modified[len(modified)-1] == 'q' {
		modified[len(modified)-1] = 'p'
	} else {
		modified[len(modified)-1] = 'q'
	}

	modifiedAddr := string(modified)

	// 验证应该失败
	if ValidateIDAddress(modifiedAddr) {
		t.Error("Checksum should have detected the error")
	}
}

func TestCaseInsensitivity(t *testing.T) {
	pubkeyHash, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")
	addr, _ := EncodeIDAddress(VersionP2PKH, pubkeyHash)

	// 转换为大写
	upperAddr := bytes.ToUpper([]byte(addr))

	// 应该仍然能够解码
	info, err := DecodeIDAddress(string(upperAddr))
	if err != nil {
		t.Fatalf("Failed to decode uppercase address: %v", err)
	}

	if !bytes.Equal(info.Data, pubkeyHash) {
		t.Error("Data mismatch after case conversion")
	}
}

func TestMultisigInvalidParameters(t *testing.T) {
	pubkey, _ := hex.DecodeString("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")

	tests := []struct {
		name    string
		m       int
		n       int
		pubkeys [][]byte
	}{
		{"m > n", 3, 2, [][]byte{pubkey, pubkey}},
		{"m = 0", 0, 2, [][]byte{pubkey, pubkey}},
		{"n = 0", 1, 0, [][]byte{}},
		{"n > 15", 1, 16, make([][]byte, 16)},
		{"pubkey count mismatch", 2, 3, [][]byte{pubkey}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewP2MSAddress(tt.m, tt.n, tt.pubkeys)
			if err == nil {
				t.Error("Expected error for invalid multisig parameters")
			}
		})
	}
}

func TestGetAddressType(t *testing.T) {
	tests := []struct {
		version  AddressVersion
		expected string
	}{
		{VersionP2PKH, "Pay-to-PubKey-Hash"},
		{VersionP2SH, "Pay-to-Script-Hash"},
		{VersionP2WPKH, "Pay-to-Witness-PubKey-Hash"},
		{VersionP2WSH, "Pay-to-Witness-Script-Hash"},
		{VersionP2MS, "Pay-to-Multisig"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := GetAddressType(tt.version)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// 性能测试
func BenchmarkEncodeP2PKH(b *testing.B) {
	pubkeyHash, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EncodeIDAddress(VersionP2PKH, pubkeyHash)
	}
}

func BenchmarkDecodeP2PKH(b *testing.B) {
	pubkeyHash, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")
	addr, _ := EncodeIDAddress(VersionP2PKH, pubkeyHash)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeIDAddress(addr)
	}
}

func BenchmarkValidateAddress(b *testing.B) {
	pubkeyHash, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")
	addr, _ := EncodeIDAddress(VersionP2PKH, pubkeyHash)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateIDAddress(addr)
	}
}

func BenchmarkNewP2MSAddress(b *testing.B) {
	pubkey1, _ := hex.DecodeString("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	pubkey2, _ := hex.DecodeString("02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5")
	pubkey3, _ := hex.DecodeString("03774ae7f858a9411e5ef4246b70c65aac5649980be5c17891bbec17895da008cb")
	pubkeys := [][]byte{pubkey1, pubkey2, pubkey3}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewP2MSAddress(2, 3, pubkeys)
	}
}

func TestEncodeDecodeP2TR(t *testing.T) {
	// 测试数据: 32字节Taproot输出密钥（x-only公钥）
	outputKey, _ := hex.DecodeString("cc8a4bc64d897bddc5fbc2f670f7a8ba0b386779106cf1223c6fc5d7cd6fc115")

	// 编码
	addr, err := EncodeIDAddress(VersionP2TR, outputKey)
	if err != nil {
		t.Fatalf("EncodeIDAddress failed: %v", err)
	}

	t.Logf("P2TR Address: %s", addr)

	// 验证地址以正确的前缀开头
	if len(addr) < 4 || addr[:4] != "idt1" {
		t.Errorf("Expected address to start with 'idt1', got %s", addr)
	}

	// 解码
	info, err := DecodeIDAddress(addr)
	if err != nil {
		t.Fatalf("DecodeIDAddress failed: %v", err)
	}

	// 验证版本
	if info.Version != VersionP2TR {
		t.Errorf("Expected version %d, got %d", VersionP2TR, info.Version)
	}

	// 验证数据
	if !bytes.Equal(info.Data, outputKey) {
		t.Errorf("Data mismatch.\nExpected: %x\nGot: %x", outputKey, info.Data)
	}

	// 验证类型描述
	addrType := GetAddressType(info.Version)
	if addrType != "Pay-to-Taproot" {
		t.Errorf("Expected 'Pay-to-Taproot', got '%s'", addrType)
	}
}

func TestNewP2TRAddress(t *testing.T) {
	// 测试1: 从33字节压缩公钥创建
	compressedPubkey, _ := hex.DecodeString("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")

	addr1, err := NewP2TRAddress(compressedPubkey)
	if err != nil {
		t.Fatalf("NewP2TRAddress with compressed pubkey failed: %v", err)
	}

	t.Logf("P2TR Address from compressed pubkey: %s", addr1)

	if !ValidateIDAddress(addr1) {
		t.Error("Address validation failed")
	}

	// 测试2: 从32字节x-only公钥创建
	xOnlyPubkey, _ := hex.DecodeString("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")

	addr2, err := NewP2TRAddress(xOnlyPubkey)
	if err != nil {
		t.Fatalf("NewP2TRAddress with x-only pubkey failed: %v", err)
	}

	t.Logf("P2TR Address from x-only pubkey: %s", addr2)

	if !ValidateIDAddress(addr2) {
		t.Error("Address validation failed")
	}

	// 两个地址应该相同（都使用相同的x坐标）
	if addr1 != addr2 {
		t.Errorf("Addresses should be the same.\nFrom compressed: %s\nFrom x-only: %s", addr1, addr2)
	}

	// 测试3: 无效长度
	invalidPubkey := make([]byte, 30)
	_, err = NewP2TRAddress(invalidPubkey)
	if err == nil {
		t.Error("Expected error for invalid pubkey length")
	}
}

func BenchmarkEncodeP2TR(b *testing.B) {
	outputKey, _ := hex.DecodeString("cc8a4bc64d897bddc5fbc2f670f7a8ba0b386779106cf1223c6fc5d7cd6fc115")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EncodeIDAddress(VersionP2TR, outputKey)
	}
}

func BenchmarkDecodeP2TR(b *testing.B) {
	outputKey, _ := hex.DecodeString("cc8a4bc64d897bddc5fbc2f670f7a8ba0b386779106cf1223c6fc5d7cd6fc115")
	addr, _ := EncodeIDAddress(VersionP2TR, outputKey)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeIDAddress(addr)
	}
}
