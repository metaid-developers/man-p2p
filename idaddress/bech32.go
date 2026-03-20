package idaddress

import (
	"errors"
	"strings"
)

// Bech32 字符集
const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

var bech32CharsetMap = map[rune]int{
	'q': 0, 'p': 1, 'z': 2, 'r': 3, 'y': 4, '9': 5, 'x': 6, '8': 7,
	'g': 8, 'f': 9, '2': 10, 't': 11, 'v': 12, 'd': 13, 'w': 14, '0': 15,
	's': 16, '3': 17, 'j': 18, 'n': 19, '5': 20, '4': 21, 'k': 22, 'h': 23,
	'c': 24, 'e': 25, '6': 26, 'm': 27, 'u': 28, 'a': 29, '7': 30, 'l': 31,
}

// Bech32 编码类型
type Bech32Encoding int

const (
	Bech32  Bech32Encoding = 1 // SegWit v0
	Bech32m Bech32Encoding = 2 // SegWit v1+ (Taproot)
)

// bech32Polymod 计算 Bech32 校验和多项式模
func bech32Polymod(values []int) int {
	gen := []int{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}

	chk := 1
	for _, v := range values {
		b := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ v
		for i := 0; i < 5; i++ {
			if (b>>uint(i))&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

// bech32HrpExpand 扩展 HRP
func bech32HrpExpand(hrp string) []int {
	result := make([]int, 0, len(hrp)*2+1)
	for _, c := range hrp {
		result = append(result, int(c>>5))
	}
	result = append(result, 0)
	for _, c := range hrp {
		result = append(result, int(c&31))
	}
	return result
}

// bech32VerifyChecksum 验证校验和
func bech32VerifyChecksum(hrp string, data []int, encoding Bech32Encoding) bool {
	values := append(bech32HrpExpand(hrp), data...)
	const bech32Const = 1
	const bech32mConst = 0x2bc830a3

	polymod := bech32Polymod(values)
	if encoding == Bech32 {
		return polymod == bech32Const
	}
	return polymod == bech32mConst
}

// convertBits8to5 转换位组 (8位转5位，用于 Bech32)
func convertBits8to5(data []byte, fromBits, toBits uint, pad bool) ([]byte, error) {
	acc := 0
	bits := uint(0)
	ret := make([]byte, 0, len(data)*int(fromBits)/int(toBits)+1)
	maxv := (1 << toBits) - 1

	for _, value := range data {
		acc = (acc << fromBits) | int(value)
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			ret = append(ret, byte((acc>>bits)&maxv))
		}
	}

	if pad {
		if bits > 0 {
			ret = append(ret, byte((acc<<(toBits-bits))&maxv))
		}
	} else if bits >= fromBits || ((acc<<(toBits-bits))&maxv) != 0 {
		return nil, errors.New("invalid padding")
	}

	return ret, nil
}

// Bech32Decode 解码 Bech32/Bech32m 地址
func Bech32Decode(addr string) (hrp string, version byte, program []byte, encoding Bech32Encoding, err error) {
	// 转换为小写
	addr = strings.ToLower(addr)

	// 查找分隔符
	pos := strings.LastIndex(addr, "1")
	if pos < 1 || pos+7 > len(addr) || len(addr) > 90 {
		return "", 0, nil, 0, errors.New("invalid bech32 address format")
	}

	// 分离 HRP 和数据部分
	hrp = addr[:pos]
	data := addr[pos+1:]

	// 解码数据
	decoded := make([]int, 0, len(data))
	for _, c := range data {
		val, ok := bech32CharsetMap[c]
		if !ok {
			return "", 0, nil, 0, errors.New("invalid bech32 character")
		}
		decoded = append(decoded, val)
	}

	// 尝试验证校验和 (先尝试 Bech32m，再尝试 Bech32)
	encoding = Bech32m
	if !bech32VerifyChecksum(hrp, decoded, Bech32m) {
		encoding = Bech32
		if !bech32VerifyChecksum(hrp, decoded, Bech32) {
			return "", 0, nil, 0, errors.New("invalid bech32 checksum")
		}
	}

	// 移除校验和（最后6个字符）
	decoded = decoded[:len(decoded)-6]

	if len(decoded) < 1 {
		return "", 0, nil, 0, errors.New("invalid bech32 data length")
	}

	// 第一个字节是见证版本
	version = byte(decoded[0])

	// 转换剩余数据从 5 位到 8 位
	converted, err := convertBits8to5(toByteArray(decoded[1:]), 5, 8, false)
	if err != nil {
		return "", 0, nil, 0, err
	}

	program = converted

	// 验证程序长度
	if len(program) < 2 || len(program) > 40 {
		return "", 0, nil, 0, errors.New("invalid witness program length")
	}

	// 验证版本和编码的匹配
	if version == 0 && encoding != Bech32 {
		return "", 0, nil, 0, errors.New("witness version 0 must use bech32")
	}
	if version != 0 && encoding != Bech32m {
		return "", 0, nil, 0, errors.New("witness version 1+ must use bech32m")
	}

	return hrp, version, program, encoding, nil
}

// toByteArray 将 int 数组转换为 byte 数组
func toByteArray(ints []int) []byte {
	bytes := make([]byte, len(ints))
	for i, v := range ints {
		bytes[i] = byte(v)
	}
	return bytes
}

// Bech32Encode 编码为 Bech32/Bech32m 地址
func Bech32Encode(hrp string, version byte, program []byte) (string, error) {
	// 选择编码类型
	encoding := Bech32
	if version != 0 {
		encoding = Bech32m
	}

	// 验证程序长度
	if len(program) < 2 || len(program) > 40 {
		return "", errors.New("invalid witness program length")
	}

	// 转换程序从 8 位到 5 位
	converted, err := convertBits8to5(program, 8, 5, true)
	if err != nil {
		return "", err
	}

	// 添加见证版本
	data := make([]int, 0, len(converted)+1)
	data = append(data, int(version))
	for _, b := range converted {
		data = append(data, int(b))
	}

	// 计算校验和
	values := append(bech32HrpExpand(hrp), data...)
	values = append(values, 0, 0, 0, 0, 0, 0)

	const bech32Const = 1
	const bech32mConst = 0x2bc830a3

	polymod := bech32Polymod(values)
	if encoding == Bech32 {
		polymod ^= bech32Const
	} else {
		polymod ^= bech32mConst
	}

	// 提取校验和
	checksum := make([]int, 6)
	for i := 0; i < 6; i++ {
		checksum[i] = (polymod >> (5 * (5 - i))) & 31
	}

	// 组合最终地址
	data = append(data, checksum...)
	var result strings.Builder
	result.WriteString(hrp)
	result.WriteRune('1')
	for _, d := range data {
		result.WriteByte(bech32Charset[d])
	}

	return result.String(), nil
}
