# MRC20 Module Configuration Guide

## Overview

MRC20 is a Bitcoin-based token protocol similar to BRC20 but with enhanced features. This module implements the complete MRC20 protocol including Deploy, Mint, and Transfer operations.

## Features

- ✅ **Deploy**: Create new MRC20 tokens with customizable parameters
- ✅ **Mint**: Mint tokens with Shovel mechanism (PIN verification)
- ✅ **Transfer**: Transfer tokens between addresses
- ✅ **Native Transfer**: Automatic detection of UTXO-based transfers
- ✅ **PebbleDB Storage**: Efficient key-value storage for MRC20 data
- ✅ **Configurable**: Enable/disable via configuration file

## Configuration

### Enable MRC20 Module

Add `"mrc20"` to the `module` array in your config file:

```toml
module = ["metaname", "mrc721", "mrc20", "metaso_notifcation"]
```

### Set MRC20 Start Height

Configure the block height where MRC20 processing should begin for each chain:

```toml
[btc]
initialHeight = 800000
mrc20Height = 820000  # MRC20 starts at block 820000
# ... other btc config

[mvc]
initialHeight = 86500
mrc20Height = 100000  # MRC20 starts at block 100000
# ... other mvc config

[doge]
initialHeight = 5000000
mrc20Height = 5050000  # MRC20 starts at block 5050000
# ... other doge config
```

### Full Example Configuration

```toml
version = "0.2"
net = "mainnet"
protocolID = "6d6574616964"
module = ["metaname", "mrc721", "mrc20", "metaso_notifcation"]

[btc]
initialHeight = 800000
mrc20Height = 820000
rpcHost = "127.0.0.1:8332"
rpcUser = "bitcoin"
rpcPass = "password"
rpcHttpPostMode = true
rpcDisableTLS = true
zmqHost = "tcp://127.0.0.1:28332"

[pebble]
dir = "./man_base_data_pebble"
num = 16
```

## MRC20 Protocol Paths

The MRC20 module recognizes the following PIN paths:

- `/ft/mrc20/deploy` - Deploy a new MRC20 token
- `/ft/mrc20/mint` - Mint tokens
- `/ft/mrc20/transfer` - Transfer tokens

## Deploy Format

```json
{
  "tick": "TEST",
  "tokenName": "Test Token",
  "decimals": "8",
  "amtPerMint": "1000",
  "mintCount": "21000000",
  "beginHeight": "820000",
  "endHeight": "1000000",
  "metadata": "",
  "type": "standard",
  "premineCount": "0",
  "pinCheck": {
    "creator": "",
    "lvl": "4",
    "path": "/ft/mrc20/shovel",
    "count": "1"
  },
  "payCheck": {
    "payTo": "bc1qaddress...",
    "payAmount": "1000"
  }
}
```

## Mint Format

```json
{
  "id": "mrc20_deploy_pin_id",
  "vout": "0"
}
```

## Transfer Format

```json
[
  {
    "id": "mrc20_id",
    "amount": "1000.5",
    "vout": 1
  },
  {
    "id": "mrc20_id",
    "amount": "500.5",
    "vout": 2
  }
]
```

## Shovel Mechanism

The Shovel mechanism prevents double-use of PINs for minting. It supports:

- **PoP Level Check**: Require specific PoP difficulty (leading zeros)
- **Creator Check**: Only allow PINs from specific creators
- **Path Check**: Match PINs by path (supports wildcards and content matching)
- **Count Check**: Require a specific number of valid PINs

### Shovel Check Examples

```json
{
  "pinCheck": {
    "lvl": "4",        // Require PoP with 4 leading zeros
    "count": "3"       // Need 3 valid shovels
  }
}
```

```json
{
  "pinCheck": {
    "creator": "metaid_xxx",  // Only PINs from this creator
    "count": "1"
  }
}
```

```json
{
  "pinCheck": {
    "path": "/app/shovel/*",  // Match path with wildcard
    "count": "5"
  }
}
```

```json
{
  "pinCheck": {
    "path": "/profile['type'='miner']",  // Match JSON field
    "count": "1"
  }
}
```

## Database Storage

MRC20 data is stored in PebbleDB with the following key patterns:

### Keys

- `mrc20_utxo_{txPoint}` - UTXO data
- `mrc20_tick_{mrc20Id}` - Token information
- `mrc20_tick_name_{tickName}` - Tick name index
- `mrc20_addr_{address}_{mrc20Id}_{txPoint}` - Address balance index
- `mrc20_shovel_{mrc20Id}_{pinId}` - Used shovels
- `mrc20_op_tx_{txId}` - Operation transaction tracking

### Query Methods

Available methods in `PebbleData`:

- `SaveMrc20Pin(utxoList []mrc20.Mrc20Utxo)` - Save UTXO data
- `SaveMrc20Tick(tickList []mrc20.Mrc20DeployInfo)` - Save token info
- `GetMrc20TickInfo(mrc20Id, tickName string)` - Get token info
- `GetMrc20UtxoByOutPutList(outputList []string, isMempool bool)` - Get UTXOs
- `GetMrc20ByAddressAndTick(address, tickId string)` - Get address balance
- `GetMrc20Balance(address, tickId string)` - Calculate balance
- `GetMrc20TickList(start, limit int)` - List all tokens
- `GetMrc20UtxoList(address string, start, limit int)` - List address UTXOs

## Verification Rules

### Deploy Validation

- Tick length: 2-24 characters
- Token name length: 1-48 characters
- Decimals: 0-12
- AmtPerMint: 1-1,000,000,000,000
- MintCount: 1-1,000,000,000,000
- Unique tick name per chain
- Premine validation (if specified)

### Mint Validation

- Token must exist
- No cross-chain minting
- Respect mint limit
- Block height range check
- Shovel verification
- PayCheck validation (if required)
- Vout validation

### Transfer Validation

- Valid JSON format
- Amount > 0
- Sufficient balance
- Decimals precision check
- Output validation
- Automatic change calculation

## Native Transfer Detection

The module automatically detects "native" transfers where MRC20 UTXOs are spent without explicit transfer PINs. The change is automatically sent to the first valid output.

## Performance

- Batch operations for efficient storage
- Indexed queries for fast balance lookups
- Separate MrcDb for MRC20 data
- Optimized prefix scans

## Troubleshooting

### MRC20 not processing

1. Check if `"mrc20"` is in the `module` array
2. Verify `mrc20Height` is set and the current height is >= mrc20Height
3. Check logs for errors

### Balance not updating

1. Verify UTXO status (should not be -1 for spent)
2. Check if transaction was properly indexed
3. Query `GetMrc20UtxoByOutPutList` to inspect UTXO state

### Shovel validation failing

1. Check PoP difficulty requirements
2. Verify creator MetaID
3. Ensure path matching is correct
4. Confirm shovel count requirement

## API Integration

You can query MRC20 data through the PebbleData methods:

```go
// Get token info
info, err := PebbleStore.GetMrc20TickInfo("mrc20_id", "")

// Get balance
balance, err := PebbleStore.GetMrc20Balance("address", "mrc20_id")

// List all tokens
tokens, err := PebbleStore.GetMrc20TickList(0, 100)
```

## Notes

- MRC20 processing starts at the configured `mrc20Height` for each chain
- All MRC20 data is stored separately in the MrcDb database
- The module is thread-safe and can handle concurrent operations
- Native transfers are automatically detected and processed
- Spent UTXOs are marked with `Status = -1`

## Support

For issues or questions, please refer to the project documentation or open an issue on the repository.
