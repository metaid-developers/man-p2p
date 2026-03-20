package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"

	"man-p2p/idaddress"
)

func main() {
	// 定义子命令
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "encode":
		encodeCmd()
	case "decode":
		decodeCmd()
	case "validate":
		validateCmd()
	case "convert":
		convertCmd()
	case "multisig":
		multisigCmd()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("ID Address CLI Tool")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  idaddr <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  encode     Encode data into ID address")
	fmt.Println("  decode     Decode ID address")
	fmt.Println("  validate   Validate ID address")
	fmt.Println("  convert    Convert between address formats")
	fmt.Println("  multisig   Create multisig address")
	fmt.Println("  help       Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  # Encode P2PKH address")
	fmt.Println("  idaddr encode p2pkh 751e76e8199196d454941c45d1b3a323f1433bd6")
	fmt.Println()
	fmt.Println("  # Encode P2TR (Taproot) address")
	fmt.Println("  idaddr encode p2tr cc8a4bc64d897bddc5fbc2f670f7a8ba0b386779106cf1223c6fc5d7cd6fc115")
	fmt.Println()
	fmt.Println("  # Decode ID address")
	fmt.Println("  idaddr decode idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh")
	fmt.Println()
	fmt.Println("  # Validate ID address")
	fmt.Println("  idaddr validate idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh")
	fmt.Println()
	fmt.Println("  # Convert Bitcoin address to ID address")
	fmt.Println("  idaddr convert 1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa")
	fmt.Println()
	fmt.Println("  # Convert ID address to Bitcoin address")
	fmt.Println("  idaddr convert idq1w508d6qejxtdg4y5r3zarvary0c5xw7ky30xwh bitcoin")
}

func encodeCmd() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: idaddr encode <version> <hex_data>")
		fmt.Println("Versions: p2pkh, p2sh, p2wpkh, p2wsh, p2tr")
		os.Exit(1)
	}

	versionStr := os.Args[2]
	dataHex := os.Args[3]

	// 解析数据
	data, err := hex.DecodeString(dataHex)
	if err != nil {
		fmt.Printf("Error: Invalid hex data: %v\n", err)
		os.Exit(1)
	}

	// 解析版本
	var version idaddress.AddressVersion
	switch strings.ToLower(versionStr) {
	case "p2pkh":
		version = idaddress.VersionP2PKH
	case "p2sh":
		version = idaddress.VersionP2SH
	case "p2wpkh":
		version = idaddress.VersionP2WPKH
	case "p2wsh":
		version = idaddress.VersionP2WSH
	case "p2tr", "taproot":
		version = idaddress.VersionP2TR
	default:
		fmt.Printf("Error: Unknown version: %s\n", versionStr)
		os.Exit(1)
	}

	// 编码地址
	addr, err := idaddress.EncodeIDAddress(version, data)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ID Address: %s\n", addr)
	fmt.Printf("Type: %s\n", idaddress.GetAddressType(version))
}

func decodeCmd() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: idaddr decode <address>")
		os.Exit(1)
	}

	addr := os.Args[2]
	info, err := idaddress.DecodeIDAddress(addr)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Address: %s\n", addr)
	fmt.Printf("Version: %d\n", info.Version)
	fmt.Printf("Type: %s\n", idaddress.GetAddressType(info.Version))
	fmt.Printf("Data: %x\n", info.Data)
	fmt.Printf("Data Length: %d bytes\n", len(info.Data))

	// 如果是多签地址，显示额外信息
	if info.Version == idaddress.VersionP2MS {
		m, n, pubkeys, err := idaddress.ExtractMultisigInfo(info)
		if err == nil {
			fmt.Printf("\nMultisig Info:\n")
			fmt.Printf("  Required Signatures: %d\n", m)
			fmt.Printf("  Total Public Keys: %d\n", n)
			fmt.Printf("  Public Keys:\n")
			for i, pubkey := range pubkeys {
				fmt.Printf("    %d: %x\n", i+1, pubkey)
			}
		}
	}
}

func validateCmd() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: idaddr validate <address>")
		os.Exit(1)
	}

	addr := os.Args[2]
	valid := idaddress.ValidateIDAddress(addr)

	if valid {
		fmt.Printf("✓ Address is valid: %s\n", addr)
		info, _ := idaddress.DecodeIDAddress(addr)
		fmt.Printf("  Type: %s\n", idaddress.GetAddressType(info.Version))
	} else {
		fmt.Printf("✗ Address is invalid: %s\n", addr)
		os.Exit(1)
	}
}

func convertCmd() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: idaddr convert <address> [target_network]")
		fmt.Println("Target networks: bitcoin, testnet, dogecoin")
		os.Exit(1)
	}

	from := os.Args[2]
	target := ""
	if len(os.Args) >= 4 {
		target = os.Args[3]
	}

	// 判断是否是ID地址
	isIDAddr := strings.HasPrefix(strings.ToLower(from), "id")

	if isIDAddr {
		// ID地址 -> 其他格式
		if target == "" {
			fmt.Println("Error: Target network is required")
			os.Exit(1)
		}

		var converted string
		var err error

		switch strings.ToLower(target) {
		case "bitcoin", "mainnet":
			converted, err = idaddress.ConvertToBitcoin(from, "mainnet")
		case "testnet":
			converted, err = idaddress.ConvertToBitcoin(from, "testnet")
		case "dogecoin", "doge":
			converted, err = idaddress.ConvertToDogecoin(from)
		default:
			fmt.Printf("Error: Unknown target network: %s\n", target)
			os.Exit(1)
		}

		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Source (ID): %s\n", from)
		fmt.Printf("Target (%s): %s\n", target, converted)
	} else {
		// 其他格式 -> ID地址
		converted, err := idaddress.ConvertFromBitcoin(from)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Source: %s\n", from)
		fmt.Printf("ID Address: %s\n", converted)

		info, _ := idaddress.DecodeIDAddress(converted)
		fmt.Printf("Type: %s\n", idaddress.GetAddressType(info.Version))
	}
}

func multisigCmd() {
	fs := flag.NewFlagSet("multisig", flag.ExitOnError)
	m := fs.Int("m", 0, "Required signatures")
	n := fs.Int("n", 0, "Total public keys")
	keys := fs.String("keys", "", "Comma-separated hex public keys")

	fs.Parse(os.Args[2:])

	if *m <= 0 || *n <= 0 {
		fmt.Println("Error: m and n must be positive")
		fs.Usage()
		os.Exit(1)
	}

	if *keys == "" {
		fmt.Println("Error: Public keys are required")
		fs.Usage()
		os.Exit(1)
	}

	// 解析公钥
	keyStrs := strings.Split(*keys, ",")
	if len(keyStrs) != *n {
		fmt.Printf("Error: Expected %d public keys, got %d\n", *n, len(keyStrs))
		os.Exit(1)
	}

	pubkeys := make([][]byte, *n)
	for i, keyStr := range keyStrs {
		key, err := hex.DecodeString(strings.TrimSpace(keyStr))
		if err != nil {
			fmt.Printf("Error: Invalid hex public key %d: %v\n", i+1, err)
			os.Exit(1)
		}
		if len(key) != 33 {
			fmt.Printf("Error: Public key %d must be 33 bytes (compressed)\n", i+1)
			os.Exit(1)
		}
		pubkeys[i] = key
	}

	// 创建多签地址
	addr, err := idaddress.NewP2MSAddress(*m, *n, pubkeys)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Multisig Address (%d-of-%d): %s\n", *m, *n, addr)
	fmt.Printf("\nPublic Keys:\n")
	for i, pubkey := range pubkeys {
		fmt.Printf("  %d: %x\n", i+1, pubkey)
	}

	if idaddress.ValidateIDAddress(addr) {
		fmt.Printf("\n✓ Address is valid\n")
	}
}
