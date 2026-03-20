package idaddress

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ripemd160"
)

const (
	// HRP 人类可读部分
	HRP = "id"

	// Separator 分隔符
	Separator = "1"

	// ChecksumLength 校验和长度
	ChecksumLength = 6
)

// AddressVersion 地址版本定义
type AddressVersion byte

const (
	VersionP2PKH  AddressVersion = 0 // Pay-to-PubKey-Hash
	VersionP2SH   AddressVersion = 1 // Pay-to-Script-Hash
	VersionP2WPKH AddressVersion = 2 // Pay-to-Witness-PubKey-Hash
	VersionP2WSH  AddressVersion = 3 // Pay-to-Witness-Script-Hash
	VersionP2MS   AddressVersion = 4 // Pay-to-Multisig
	VersionP2TR   AddressVersion = 5 // Pay-to-Taproot (Schnorr)
)

// VersionChar 版本字符映射
var VersionChar = map[AddressVersion]string{
	VersionP2PKH:  "q",
	VersionP2SH:   "p",
	VersionP2WPKH: "z",
	VersionP2WSH:  "r",
	VersionP2MS:   "y",
	VersionP2TR:   "t",
}

// CharVersion 字符到版本的映射
var CharVersion = map[string]AddressVersion{
	"q": VersionP2PKH,
	"p": VersionP2SH,
	"z": VersionP2WPKH,
	"r": VersionP2WSH,
	"y": VersionP2MS,
	"t": VersionP2TR,
}

// Charset Base32字符集
const Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// CharsetMap 字符到值的映射
var CharsetMap map[byte]int

func init() {
	CharsetMap = make(map[byte]int)
	for i := 0; i < len(Charset); i++ {
		CharsetMap[Charset[i]] = i
	}
}

// AddressInfo 地址信息
type AddressInfo struct {
	Version AddressVersion
	Data    []byte
	Address string
}

// EncodeIDAddress 编码ID地址
// version: 地址版本 (0-4)
// data: 地址数据 (公钥哈希、脚本哈希等)
func EncodeIDAddress(version AddressVersion, data []byte) (string, error) {
	// 验证版本
	versionChar, ok := VersionChar[version]
	if !ok {
		return "", fmt.Errorf("invalid version: %d", version)
	}

	// 验证数据长度
	if err := validateDataLength(version, data); err != nil {
		return "", err
	}

	// 转换为5位编码
	converted, err := convertBits(data, 8, 5, true)
	if err != nil {
		return "", err
	}

	// 计算校验和 - HRP需要包含版本字符
	hrpWithVersion := HRP + versionChar
	hrpExpand := hrpExpand(hrpWithVersion)
	checksum := createChecksum(hrpExpand, converted)

	// 组合最终地址 - 只包含数据和校验和
	combined := append(converted, checksum...)
	encoded := make([]byte, len(combined))
	for i, val := range combined {
		encoded[i] = Charset[val]
	}

	return hrpWithVersion + Separator + string(encoded), nil
}

// DecodeIDAddress 解码ID地址
func DecodeIDAddress(addr string) (*AddressInfo, error) {
	// 转换为小写
	addr = strings.ToLower(addr)

	// 检查HRP
	if !strings.HasPrefix(addr, HRP) {
		return nil, errors.New("invalid HRP: must start with 'id'")
	}

	// 查找分隔符
	sepIndex := strings.LastIndex(addr, Separator)
	if sepIndex == -1 {
		return nil, errors.New("separator not found")
	}

	// 提取版本
	if sepIndex < len(HRP)+1 {
		return nil, errors.New("invalid address format")
	}
	versionChar := addr[len(HRP):sepIndex]
	version, ok := CharVersion[versionChar]
	if !ok {
		return nil, fmt.Errorf("invalid version character: %s", versionChar)
	}

	// 提取数据部分
	dataStr := addr[sepIndex+1:]
	if len(dataStr) < ChecksumLength {
		return nil, errors.New("address too short")
	}

	// 解码数据
	decoded := make([]int, len(dataStr))
	for i, c := range dataStr {
		val, ok := CharsetMap[byte(c)]
		if !ok {
			return nil, fmt.Errorf("invalid character: %c", c)
		}
		decoded[i] = val
	}

	// 验证校验和 - HRP需要包含版本字符
	hrpWithVersion := HRP + versionChar
	hrpExpand := hrpExpand(hrpWithVersion)
	if !verifyChecksum(hrpExpand, decoded) {
		return nil, errors.New("checksum verification failed")
	}

	// 移除校验和
	data := decoded[:len(decoded)-ChecksumLength]

	// 转换回8位
	convertedInts, err := convertBits(data, 5, 8, false)
	if err != nil {
		return nil, err
	}
	converted := intSliceToBytes(convertedInts)

	// 验证数据长度
	if err := validateDataLength(version, converted); err != nil {
		return nil, err
	}

	return &AddressInfo{
		Version: version,
		Data:    converted,
		Address: addr,
	}, nil
}

// ValidateIDAddress 验证ID地址是否有效
func ValidateIDAddress(addr string) bool {
	_, err := DecodeIDAddress(addr)
	return err == nil
}

// convertBits 转换位宽
func convertBits(data interface{}, fromBits, toBits int, pad bool) ([]int, error) {
	// 转换输入为字节数组
	var byteData []byte
	switch v := data.(type) {
	case []byte:
		byteData = v
	case []int:
		byteData = intSliceToBytes(v)
	default:
		return nil, errors.New("invalid data type")
	}

	acc := 0
	bits := 0
	ret := []int{}
	maxv := (1 << toBits) - 1
	maxAcc := (1 << (fromBits + toBits - 1)) - 1

	for _, value := range byteData {
		if int(value) < 0 || int(value)>>fromBits != 0 {
			return nil, errors.New("invalid data value")
		}
		acc = ((acc << fromBits) | int(value)) & maxAcc
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			ret = append(ret, (acc>>bits)&maxv)
		}
	}

	if pad {
		if bits > 0 {
			ret = append(ret, (acc<<(toBits-bits))&maxv)
		}
	} else if bits >= fromBits || ((acc<<(toBits-bits))&maxv) != 0 {
		return nil, errors.New("invalid padding")
	}

	return ret, nil
}

// hrpExpand 扩展HRP
func hrpExpand(hrp string) []int {
	ret := make([]int, len(hrp)*2+1)
	for i, c := range hrp {
		ret[i] = int(c) >> 5
		ret[i+len(hrp)+1] = int(c) & 31
	}
	ret[len(hrp)] = 0
	return ret
}

// polymod 多项式模运算
func polymod(values []int) int {
	gen := []int{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := 1
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ v
		for i := 0; i < 5; i++ {
			if (top>>i)&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

// createChecksum 创建校验和
func createChecksum(hrpExpand []int, data []int) []int {
	values := append(hrpExpand, data...)
	values = append(values, []int{0, 0, 0, 0, 0, 0}...)
	mod := polymod(values) ^ 1
	ret := make([]int, 6)
	for i := 0; i < 6; i++ {
		ret[i] = (mod >> (5 * (5 - i))) & 31
	}
	return ret
}

// verifyChecksum 验证校验和
func verifyChecksum(hrpExpand []int, data []int) bool {
	values := append(hrpExpand, data...)
	return polymod(values) == 1
}

// validateDataLength 验证数据长度
func validateDataLength(version AddressVersion, data []byte) error {
	switch version {
	case VersionP2PKH, VersionP2SH, VersionP2WPKH:
		if len(data) != 20 {
			return fmt.Errorf("invalid data length for version %d: expected 20, got %d", version, len(data))
		}
	case VersionP2WSH:
		if len(data) != 32 {
			return fmt.Errorf("invalid data length for version %d: expected 32, got %d", version, len(data))
		}
	case VersionP2TR:
		if len(data) != 32 {
			return fmt.Errorf("invalid data length for version %d: expected 32, got %d", version, len(data))
		}
	case VersionP2MS:
		if len(data) < 2 {
			return errors.New("invalid multisig data: too short")
		}
		m := int(data[0])
		n := int(data[1])
		if m == 0 || n == 0 || m > n || n > 15 {
			return fmt.Errorf("invalid multisig parameters: m=%d, n=%d", m, n)
		}
		expectedLen := 2 + n*33 // 2字节头 + n个压缩公钥
		if len(data) != expectedLen {
			return fmt.Errorf("invalid multisig data length: expected %d, got %d", expectedLen, len(data))
		}
	default:
		return fmt.Errorf("unknown version: %d", version)
	}
	return nil
}

// intSliceToBytes 将int切片转换为字节切片
func intSliceToBytes(data []int) []byte {
	ret := make([]byte, len(data))
	for i, v := range data {
		ret[i] = byte(v)
	}
	return ret
}

// Hash160 计算Hash160 (SHA256 + RIPEMD160)
func Hash160(data []byte) []byte {
	hash := sha256.Sum256(data)
	ripemd := ripemd160.New()
	ripemd.Write(hash[:])
	return ripemd.Sum(nil)
}

// Hash256 计算双重SHA256
func Hash256(data []byte) []byte {
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	return second[:]
}

// NewP2PKHAddress 从公钥创建P2PKH地址
func NewP2PKHAddress(pubkey []byte) (string, error) {
	hash := Hash160(pubkey)
	return EncodeIDAddress(VersionP2PKH, hash)
}

// NewP2SHAddress 从脚本创建P2SH地址
func NewP2SHAddress(script []byte) (string, error) {
	hash := Hash160(script)
	return EncodeIDAddress(VersionP2SH, hash)
}

// NewP2WPKHAddress 从公钥创建P2WPKH地址
func NewP2WPKHAddress(pubkey []byte) (string, error) {
	hash := Hash160(pubkey)
	return EncodeIDAddress(VersionP2WPKH, hash)
}

// NewP2WSHAddress 从见证脚本创建P2WSH地址
func NewP2WSHAddress(script []byte) (string, error) {
	hash := sha256.Sum256(script)
	return EncodeIDAddress(VersionP2WSH, hash[:])
}

// NewP2TRAddress 从Schnorr公钥创建P2TR地址
// pubkey: 32字节的x-only公钥（Taproot输出密钥）
// 如果输入是33字节的压缩公钥，会自动提取x坐标
func NewP2TRAddress(pubkey []byte) (string, error) {
	var outputKey []byte

	if len(pubkey) == 33 {
		// 压缩公钥格式（0x02/0x03 + x坐标）
		// 提取x坐标作为输出密钥
		outputKey = pubkey[1:]
	} else if len(pubkey) == 32 {
		// 直接使用32字节的x-only公钥
		outputKey = pubkey
	} else {
		return "", fmt.Errorf("invalid pubkey length: expected 32 or 33 bytes, got %d", len(pubkey))
	}

	// Taproot使用x-only公钥（32字节）
	return EncodeIDAddress(VersionP2TR, outputKey)
}

// NewP2MSAddress 创建多签地址
// m: 需要的签名数
// n: 总公钥数
// pubkeys: 公钥列表 (压缩格式，每个33字节)
func NewP2MSAddress(m, n int, pubkeys [][]byte) (string, error) {
	if m <= 0 || n <= 0 || m > n || n > 15 {
		return "", fmt.Errorf("invalid multisig parameters: m=%d, n=%d", m, n)
	}
	if len(pubkeys) != n {
		return "", fmt.Errorf("pubkey count mismatch: expected %d, got %d", n, len(pubkeys))
	}

	// 验证所有公钥都是压缩格式
	for i, pubkey := range pubkeys {
		if len(pubkey) != 33 {
			return "", fmt.Errorf("pubkey %d is not compressed (expected 33 bytes, got %d)", i, len(pubkey))
		}
		if pubkey[0] != 0x02 && pubkey[0] != 0x03 {
			return "", fmt.Errorf("pubkey %d has invalid prefix", i)
		}
	}

	// 构建数据: m + n + pubkeys
	data := make([]byte, 2+n*33)
	data[0] = byte(m)
	data[1] = byte(n)
	for i, pubkey := range pubkeys {
		copy(data[2+i*33:2+(i+1)*33], pubkey)
	}

	return EncodeIDAddress(VersionP2MS, data)
}

// GetAddressType 获取地址类型描述
func GetAddressType(version AddressVersion) string {
	switch version {
	case VersionP2PKH:
		return "Pay-to-PubKey-Hash"
	case VersionP2SH:
		return "Pay-to-Script-Hash"
	case VersionP2WPKH:
		return "Pay-to-Witness-PubKey-Hash"
	case VersionP2WSH:
		return "Pay-to-Witness-Script-Hash"
	case VersionP2MS:
		return "Pay-to-Multisig"
	case VersionP2TR:
		return "Pay-to-Taproot"
	default:
		return "Unknown"
	}
}

// ExtractMultisigInfo 从P2MS地址提取多签信息
func ExtractMultisigInfo(info *AddressInfo) (m, n int, pubkeys [][]byte, err error) {
	if info.Version != VersionP2MS {
		return 0, 0, nil, errors.New("not a multisig address")
	}

	if len(info.Data) < 2 {
		return 0, 0, nil, errors.New("invalid multisig data")
	}

	m = int(info.Data[0])
	n = int(info.Data[1])

	pubkeys = make([][]byte, n)
	for i := 0; i < n; i++ {
		start := 2 + i*33
		end := start + 33
		if end > len(info.Data) {
			return 0, 0, nil, errors.New("invalid multisig data length")
		}
		pubkeys[i] = make([]byte, 33)
		copy(pubkeys[i], info.Data[start:end])
	}

	return m, n, pubkeys, nil
}
