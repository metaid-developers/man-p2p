package dogecoin

import (
	"math/big"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// DogeMainNetParams defines the network parameters for the main Dogecoin network.
var DogeMainNetParams = chaincfg.Params{
	Name:        "mainnet",
	Net:         wire.BitcoinNet(0xc0c0c0c0), // Dogecoin MainNet magic
	DefaultPort: "22556",
	DNSSeeds: []chaincfg.DNSSeed{
		{Host: "seed.dogecoin.com", HasFiltering: true},
		{Host: "seed.multidoge.org", HasFiltering: true},
	},
	GenesisHash:      newHashFromStr("1a91e3dace36e2be3bf030a65679fe821aa1d6ef92e7c9902eb318182c355691"),
	PowLimit:         newBigIntFromHex("00000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
	CoinbaseMaturity: 240,
	PubKeyHashAddrID: 0x1e,   // starts with D
	ScriptHashAddrID: 0x16,   // starts with 9
	PrivateKeyID:     0x9e,   // starts with Q
	Bech32HRPSegwit:  "doge", // Not widely used but defined
	HDCoinType:       3,
}

// DogeTestNetParams defines the network parameters for the test Dogecoin network.
var DogeTestNetParams = chaincfg.Params{
	Name:        "testnet",
	Net:         wire.BitcoinNet(0xfcc1b7dc), // Dogecoin TestNet magic
	DefaultPort: "44556",
	DNSSeeds: []chaincfg.DNSSeed{
		{Host: "testseed.jrn.me.uk", HasFiltering: true},
	},
	GenesisHash:      newHashFromStr("bb0a78264637406b6360aad926284d544d7049f45189db5664f3c4d07350559e"),
	PowLimit:         newBigIntFromHex("00000fffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
	CoinbaseMaturity: 240,
	PubKeyHashAddrID: 0x71, // starts with n
	ScriptHashAddrID: 0xc4, // starts with 2
	PrivateKeyID:     0xf1, // starts with 9 or c
	Bech32HRPSegwit:  "tdoge",
	HDCoinType:       1,
}

// DogeRegTestParams defines the network parameters for the regression test Dogecoin network.
var DogeRegTestParams = chaincfg.Params{
	Name:             "regtest",
	Net:              wire.BitcoinNet(0xfabfb5da), // Dogecoin RegTest magic
	DefaultPort:      "18444",
	DNSSeeds:         []chaincfg.DNSSeed{},
	GenesisHash:      newHashFromStr("3d2160a3b5dc4a9d62e7404bb5aa85b0183cd8db1d244508f6003d23713e8819"),
	PowLimit:         newBigIntFromHex("7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
	CoinbaseMaturity: 150,
	PubKeyHashAddrID: 0x6f, // starts with m or n
	ScriptHashAddrID: 0xc4, // starts with 2
	PrivateKeyID:     0xef,
	Bech32HRPSegwit:  "rdoge",
	HDCoinType:       1,
}

func newHashFromStr(str string) *chainhash.Hash {
	hash, _ := chainhash.NewHashFromStr(str)
	return hash
}

func newBigIntFromHex(str string) *big.Int {
	i, _ := new(big.Int).SetString(str, 16)
	return i
}
