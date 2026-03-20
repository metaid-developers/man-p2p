package common

import (
	"log"
	"man-p2p/idaddress"
)

// ConvertToGlobalMetaId 将区块链地址转换为全局统一的 IDAddress 格式
// 支持 Bitcoin、Dogecoin 等多链地址
func ConvertToGlobalMetaId(address string) string {
	if address == "" || address == "errorAddr" {
		return ""
	}

	// 尝试转换为 IDAddress
	idAddr, err := idaddress.ConvertFromBitcoin(address)
	if err != nil {
		// 如果转换失败，记录日志并返回空字符串
		// 这可能是因为地址格式不被支持（如 Bech32）或校验和错误
		log.Printf("[WARN] Failed to convert address %s to GlobalMetaId: %v", address, err)
		return ""
	}

	return idAddr
}

// ValidateGlobalMetaId 验证 GlobalMetaId 是否是有效的 IDAddress
func ValidateGlobalMetaId(globalMetaId string) bool {
	return idaddress.ValidateIDAddress(globalMetaId)
}
