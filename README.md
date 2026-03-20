# man-indexer-v2

A high-performance blockchain inscription indexer for MetaID protocol, supporting Bitcoin, Dogecoin, and MicroVision Chain (MVC).

## Overview

**man-indexer-v2** is a multi-chain blockchain indexer designed specifically for the MetaID protocol. It efficiently indexes and queries inscription data (PINs) across multiple blockchain networks, providing RESTful API access for developers and applications.

### Key Features

- 🚀 **Multi-Chain Support**: Bitcoin (BTC), Dogecoin (DOGE), and MicroVision Chain (MVC)
- ⚡ **High Performance**: Built on PebbleDB for optimized read/write operations
- 🔄 **Real-Time Monitoring**: ZMQ-based live transaction streaming
- 📊 **Comprehensive Indexing**: Complete PIN inscription tracking and transfer history
- 🔌 **RESTful API**: Easy-to-use HTTP endpoints for data queries
- 🎯 **Protocol Support**: Native MRC20 token transfers and MetaID operations
- 🌐 **Network Flexibility**: Mainnet, Testnet, and Regtest support for all chains

## Supported Blockchains

| Blockchain | Status | Networks | Inscription Format |
|------------|--------|----------|-------------------|
| **Bitcoin (BTC)** | ✅ Stable | Mainnet, Testnet, Regtest | SegWit Witness (P2WSH/Taproot) |
| **Dogecoin (DOGE)** | ✅ Stable | Mainnet, Testnet, Regtest | P2SH ScriptSig |
| **MicroVision Chain (MVC)** | ✅ Stable | Mainnet | Custom Format |

## New: Dogecoin Support 🐕

The latest version includes comprehensive Dogecoin blockchain support with unique adaptations:

### Dogecoin Adapter Features

- ✅ **P2SH Inscription Parsing**: Extracts inscriptions from Pay-to-Script-Hash scripts
- ✅ **MetaID-CLI Compatible**: Fully compatible with metaid-cli Dogecoin inscription format
- ✅ **Multi-Network**: Supports Mainnet, Testnet, and Regtest environments
- ✅ **ZMQ Integration**: Real-time transaction monitoring via ZeroMQ
- ✅ **MRC20 Support**: Native token transfer tracking
- ✅ **Complete Transfer Tracking**: Full PIN ownership and transfer history

### Quick Start (Dogecoin)

```bash
# Using the quick start script (recommended)
./start_doge_indexer.sh

# Manual start
./manindexer -chain doge -config config_doge.toml

# Multi-chain indexing
./manindexer -chain btc,doge -config config.toml
```

For detailed Dogecoin setup and usage, see: [Dogecoin Adapter Guide](DOGECOIN_ADAPTER.md)

## Installation

### Prerequisites

- **Go 1.20+**: Required for compilation
- **Docker** (optional): For running blockchain nodes
- **Blockchain Node**: Access to RPC endpoints for each chain you want to index

### Build from Source

```bash
# Clone the repository
git clone https://github.com/metaid-developers/man-indexer-v2.git
cd man-indexer-v2

# Build the binary
go build -o manindexer

# Verify installation
./manindexer --help
```

### Using Make

```bash
make build    # Build the binary
make test     # Run tests
make clean    # Clean build artifacts
```

## Configuration

### Configuration File

Create a `config.toml` file with your blockchain settings:

```toml
# Protocol identifier (hex encoded "metaid")
protocolID = "6d6574616964"

# Bitcoin Configuration
[btc]
initialHeight = 844446                  # Starting block height
rpcHost = "127.0.0.1:8332"             # Bitcoin Core RPC endpoint
rpcUser = "bitcoin"                     # RPC username
rpcPass = "password"                    # RPC password
rpcHttpPostMode = true
rpcDisableTLS = true
zmqHost = "tcp://127.0.0.1:28332"      # ZMQ endpoint for real-time updates
popCutNum = 21                          # Proof-of-Publication cut number

# MicroVision Chain Configuration
[mvc]
initialHeight = 86500
rpcHost = "127.0.0.1:9882"
rpcUser = "mvc"
rpcPass = "password"
rpcHttpPostMode = true
rpcDisableTLS = true
zmqHost = "tcp://127.0.0.1:28882"
popCutNum = 21

# Dogecoin Configuration
[doge]
initialHeight = 0                       # Start from genesis (or specify height)
rpcHost = "127.0.0.1:22555"            # Dogecoin Core RPC endpoint
rpcUser = "dogecoin"
rpcPass = "password"
rpcHttpPostMode = true
rpcDisableTLS = true
zmqHost = "tcp://127.0.0.1:28555"
popCutNum = 21

# Database Configuration (PebbleDB)
[pebble]
dir = "./man_base_data_pebble"         # Data storage directory
num = 10                                # Number of shards for optimization

# Web API Configuration
[web]
port = "8080"                           # API server port
host = "localhost"                      # API server host
pemFile = ""                            # TLS certificate (optional)
keyFile = ""                            # TLS key (optional)
```

### Configuration Options Explained

- **initialHeight**: Block height to start indexing from. Set to 0 to index from genesis (may take significant time)
- **rpcHost**: Blockchain node RPC endpoint (ensure node is fully synced)
- **zmqHost**: ZeroMQ endpoint for real-time transaction broadcasting
- **popCutNum**: Proof-of-Publication parameter for MetaID protocol
- **num**: Database sharding level (higher = better performance, more disk usage)

## Usage

### Starting the Indexer

#### Single Chain Indexing

```bash
# Index Bitcoin mainnet
./manindexer -chain btc -config config.toml

# Index Dogecoin mainnet
./manindexer -chain doge -config config.toml

# Index MVC
./manindexer -chain mvc -config config.toml
```

#### Multi-Chain Indexing

```bash
# Index all three chains simultaneously
./manindexer -chain btc,mvc,doge -config config.toml

# Bitcoin + Dogecoin only
./manindexer -chain btc,doge -config config.toml
```

#### Network Selection

```bash
# Mainnet (default)
./manindexer -chain btc -config config.toml

# Testnet
./manindexer -chain btc -test 1 -config config.toml

# Regtest (local testing)
./manindexer -chain btc -test 2 -config config.toml
```

### Command Line Parameters

```bash
Usage: ./manindexer [OPTIONS]

Core Options:
  -chain string       Blockchain type: btc, mvc, doge (comma-separated for multiple)
  -config string      Path to configuration file (default: config.toml)
  -database string    Database type: pebble (default: pebble)
  -test string        Network mode: 0=mainnet, 1=testnet, 2=regtest (default: 0)
  -server string      Start Web API server: 1=enable, 0=disable (default: 1)

Bitcoin-Specific Options:
  -btc_height int           Starting block height for Bitcoin
  -btc_rpc_host string      Bitcoin RPC endpoint
  -btc_rpc_user string      Bitcoin RPC username
  -btc_rpc_password string  Bitcoin RPC password
  -btc_zmqpubrawtx string   Bitcoin ZMQ endpoint

Dogecoin-Specific Options:
  -doge_height int           Starting block height for Dogecoin
  -doge_rpc_host string      Dogecoin RPC endpoint
  -doge_rpc_user string      Dogecoin RPC username
  -doge_rpc_password string  Dogecoin RPC password
  -doge_zmqpubrawtx string   Dogecoin ZMQ endpoint

MVC-Specific Options:
  -mvc_height int           Starting block height for MVC
  -mvc_rpc_host string      MVC RPC endpoint
  -mvc_rpc_user string      MVC RPC username
  -mvc_rpc_password string  MVC RPC password
  -mvc_zmqpubrawtx string   MVC ZMQ endpoint
```

### Example Commands

```bash
# Start with custom configuration
./manindexer -chain doge -config config_doge.toml -server 1

# Override RPC settings via command line
./manindexer \
  -chain doge \
  -test 2 \
  -doge_rpc_host 127.0.0.1:18332 \
  -doge_rpc_user regtest \
  -doge_rpc_password regtest \
  -doge_zmqpubrawtx tcp://127.0.0.1:18444

# Multi-chain with specific starting heights
./manindexer \
  -chain btc,doge \
  -btc_height 850000 \
  -doge_height 5000000 \
  -config config.toml
```

## API Reference

The indexer automatically starts a RESTful API server (default port: 8080) for querying indexed data.

### Core Endpoints

#### Get Block Information
```http
GET /api/block/{chain}/{height}
```

**Parameters:**
- `chain`: Blockchain identifier (btc, doge, mvc)
- `height`: Block height number

**Response:**
```json
{
  "height": 850000,
  "hash": "00000000000000000002...",
  "timestamp": 1705849200,
  "txCount": 2543
}
```

#### Query PIN Inscriptions
```http
POST /api/pins/{chain}
Content-Type: application/json

{
  "path": "/test",
  "cursor": "",
  "size": 20
}
```

**Parameters:**
- `chain`: Blockchain identifier
- `path`: MetaID path filter
- `cursor`: Pagination cursor (empty for first page)
- `size`: Number of results (max: 100)

**Response:**
```json
{
  "code": 200,
  "data": {
    "list": [
      {
        "id": "abc123...",
        "path": "/test",
        "content": "Hello World",
        "contentType": "text/plain",
        "creator": "1A1zP1eP5QGefi...",
        "timestamp": 1705849200
      }
    ],
    "total": 150,
    "nextCursor": "xyz789..."
  }
}
```

#### Get Address Inscriptions
```http
GET /api/address/{chain}/{address}?size=20&cursor=
```

**Parameters:**
- `chain`: Blockchain identifier
- `address`: Blockchain address
- `size`: Results per page (optional, default: 20)
- `cursor`: Pagination cursor (optional)

#### Get PIN Details
```http
GET /api/pin/{chain}/{pinId}
```

**Parameters:**
- `chain`: Blockchain identifier
- `pinId`: PIN inscription identifier

**Response:**
```json
{
  "id": "abc123...",
  "txid": "def456...",
  "output": "def456...:0",
  "path": "/profile/name",
  "content": "MetaID User",
  "contentType": "text/plain",
  "contentLength": 12,
  "creator": "1A1zP1eP5QGefi...",
  "owner": "1A1zP1eP5QGefi...",
  "genesisHeight": 850000,
  "timestamp": 1705849200,
  "pop": "abc123...",
  "popLv": 3
}
```

### Additional Endpoints

- `GET /api/count` - Get indexer statistics (total pins, blocks, metaids)
- `GET /api/metaid/{metaidRoot}` - Query by MetaID root
- `GET /api/path/{chain}/{path}` - Get all pins for a specific path
- `POST /api/transfers/{chain}` - Query PIN transfer history
- `GET /content/{pinId}` - Get raw content data for a PIN
- `GET /api/blockfile?chain={chain}&height={height}` - Download block data file

For complete API documentation, visit `/swagger` after starting the server (if Swagger UI is enabled).

## Project Structure

```
man-indexer-v2/
├── adapter/                    # Blockchain adapters
│   ├── bitcoin/               # Bitcoin adapter implementation
│   │   ├── bitcoin.go        # Chain interface
│   │   ├── indexer.go        # Indexer implementation
│   │   └── zmq.go            # ZMQ handler
│   ├── dogecoin/              # Dogecoin adapter implementation  
│   │   ├── dogecoin.go       # Chain interface
│   │   ├── indexer.go        # P2SH inscription parser
│   │   ├── params.go         # Network parameters
│   │   └── zmq.go            # ZMQ handler
│   ├── microvisionchain/      # MVC adapter implementation
│   │   ├── mvc.go            # Chain interface
│   │   ├── indexer.go        # Indexer implementation
│   │   └── zmq.go            # ZMQ handler
│   ├── chain.go               # Chain interface definition
│   ├── indexer.go             # Indexer interface definition
│   └── metaid.go              # MetaID protocol handlers
├── api/                       # RESTful API server
│   ├── webapi.go             # Web UI endpoints
│   ├── btc_jsonapi.go        # JSON API endpoints
│   └── respond/              # Response helpers
├── common/                    # Shared utilities
│   ├── config.go             # Configuration management
│   ├── tools.go              # Helper functions
│   ├── id_coins.go           # Chain ID mappings
│   └── pop.go                # Proof-of-Publication
├── docs/                      # API documentation
│   ├── swagger.json          # Swagger/OpenAPI spec
│   └── swagger.yaml
├── idaddress/                 # ID address conversion
│   ├── converter.go          # Address converter
│   ├── test_mvc.sh           # MVC testing script
│   └── cmd/                  # Command-line tools
├── man/                       # Core indexer logic
│   ├── man.go                # Main indexer orchestrator
│   ├── indexer_pebble.go     # PebbleDB indexer
│   ├── blockfile.go          # Block file management
│   ├── mempool.go            # Mempool monitoring
│   └── man_function.go       # Helper functions
├── mrc20/                     # MRC20 token support
│   ├── mrc20.go              # Token operations
│   └── util.go               # Utilities
├── pebblestore/               # PebbleDB storage layer
│   ├── store.go              # Core storage operations
│   ├── data.go               # Data models
│   ├── creatordb.go          # Creator indexing
│   └── pincount.go           # Statistics
├── pin/                       # PIN data structures
│   ├── pin.go                # PIN model
│   ├── pop.go                # Proof-of-Publication
│   ├── validator.go          # Data validation
│   └── blockfile.proto       # Protocol buffers
├── tools/                     # Development tools
│   └── parse_doge_tx.go      # Dogecoin transaction parser
├── web/                       # Web UI assets
│   ├── static/               # Static files (CSS, JS)
│   └── template/             # HTML templates
├── app.go                     # Application entry point
├── config.toml                # Main configuration file
├── config_doge.toml           # Dogecoin-specific config
├── config_regtest.toml        # Regtest configuration
├── go.mod                     # Go module dependencies
├── Makefile                   # Build automation
├── Dockerfile                 # Container image
└── README.md                  # This file
```

## Development

### Adding New Blockchain Support

To add support for a new blockchain:

1. **Create Adapter Package**: In `adapter/`, create a new directory for your chain
2. **Implement Interfaces**: Implement both `Chain` and `Indexer` interfaces
3. **Add Configuration**: Update `common/config.go` with chain-specific config
4. **Register Adapter**: Add initialization in `man/man.go` `InitAdapter()`
5. **Add Network Params**: Define network parameters (magic bytes, address prefixes, etc.)
6. **Implement Inscription Parser**: Parse chain-specific inscription formats
7. **Add ZMQ Support**: Implement real-time transaction monitoring
8. **Test Thoroughly**: Create test cases for all inscription types

Reference the [Dogecoin adapter implementation](adapter/dogecoin/) as a complete example.

### Running Tests

```bash
# Run all tests
make test

# Run specific package tests
go test ./adapter/dogecoin/...
go test ./man/...
go test ./pebblestore/...

# Run with coverage
go test -cover ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Local Development Setup

```bash
# 1. Start a regtest blockchain node (example: Dogecoin)
docker run -d --name dogecoin-regtest \
  -p 18332:18332 \
  -p 18444:18444 \
  -e NETWORK=regtest \
  -e RPC_USER=regtest \
  -e RPC_PASSWORD=regtest \
  ich777/dogecoin-core

# 2. Build and run indexer
go build -o manindexer
./manindexer -chain doge -test 2 -config config_regtest.toml

# 3. Test inscription creation (requires metaid-cli)
cd /path/to/metaid-cli
./metaid-cli inscribe create \
  --chain doge \
  --payload '{"hello": "world"}' \
  --path "/test" \
  --address <your-doge-address>

# 4. Query indexed data
curl http://localhost:8080/api/pins/doge -X POST \
  -H "Content-Type: application/json" \
  -d '{"path": "/test", "size": 10}'
```

## Dogecoin Documentation

For comprehensive Dogecoin-specific information:

- [**Dogecoin Adapter Guide**](DOGECOIN_ADAPTER.md) - Detailed setup, configuration, and usage
- [**Implementation Summary**](IMPLEMENTATION_SUMMARY.md) - Technical architecture and design decisions
- [**Quick Start Script**](start_doge_indexer.sh) - One-command Dogecoin indexer deployment

### Dogecoin Key Differences

Unlike Bitcoin's SegWit-based inscriptions, Dogecoin uses:

- **P2SH Scripts**: Inscriptions stored in ScriptSig redeem scripts
- **Legacy Addresses**: P2PKH/P2SH address formats (no Bech32)
- **Different Network Magic**: Unique protocol identifiers
- **Custom Fee Calculation**: Dogecoin-specific fee structures

## Testing

### Bitcoin Regtest Testing

```bash
# Start Bitcoin regtest node
docker run -d --name bitcoin-regtest \
  -p 18443:18443 \
  -p 18444:18444 \
  ruimarinho/bitcoin-core:latest \
  -regtest \
  -rpcuser=regtest \
  -rpcpassword=regtest \
  -rpcallowip=0.0.0.0/0 \
  -zmqpubrawtx=tcp://0.0.0.0:18444

# Start indexer
./manindexer -chain btc -test 2 -config config_regtest.toml

# Mine some blocks
bitcoin-cli -regtest -rpcuser=regtest -rpcpassword=regtest generate 101
```

### Dogecoin Regtest Testing

```bash
# Start Dogecoin regtest node
docker run -d --name dogecoin-regtest \
  -p 18332:18332 \
  -p 18444:18444 \
  -e NETWORK=regtest \
  -e RPC_USER=regtest \
  -e RPC_PASSWORD=regtest \
  ich777/dogecoin-core

# Start indexer
./manindexer -chain doge -test 2 \
  -doge_rpc_host 127.0.0.1:18332 \
  -doge_rpc_user regtest \
  -doge_rpc_password regtest \
  -doge_zmqpubrawtx tcp://127.0.0.1:18444

# Create test inscription (using metaid-cli)
./metaid-cli inscribe create \
  --chain doge \
  --payload '{"test": "Hello Dogecoin!"}' \
  --path "/test" \
  --address <your-address> \
  --feerate 100000
```

### Integration Testing

```bash
# Run end-to-end tests
cd tests/
./integration_test.sh

# Test specific chain
./integration_test.sh doge

# Test all chains
./integration_test.sh all
```

## Troubleshooting

### Connection Issues

**Problem**: Cannot connect to blockchain node

**Solutions**:
1. Verify node is running: `ps aux | grep dogecoin` (or bitcoin/mvc)
2. Check RPC port is correct in config
3. Confirm RPC username/password match node configuration
4. Test RPC connection: `curl --user username:password http://localhost:22555`
5. Check firewall rules allow local connections
6. Review node logs for errors

### Inscription Parsing Issues

**Problem**: Inscriptions not being indexed correctly

**Solutions**:
1. Verify inscription format matches protocol specification
2. For Dogecoin: Check P2SH redeem script structure
3. For Bitcoin: Verify SegWit witness data format
4. Enable debug logging: Add `-debug=1` flag
5. Check indexer logs: `tail -f indexer.log`
6. Verify `protocolID` in config matches inscription protocol

### Performance Issues

**Problem**: Slow indexing or high memory usage

**Solutions**:
1. Increase database shards: `[pebble] num = 20`
2. Set higher starting block: `initialHeight = 5000000`
3. Disable full-node mode if not needed: `isFullNode = false`
4. Increase system resources (RAM, CPU cores)
5. Use SSD for database storage
6. Enable query caching in configuration

### Database Errors

**Problem**: PebbleDB corruption or errors

**Solutions**:
1. Stop indexer gracefully: `kill -SIGTERM <pid>`
2. Backup data: `cp -r man_base_data_pebble man_base_data_pebble.backup`
3. Clear and re-index: `rm -rf man_base_data_pebble && ./manindexer ...`
4. Check disk space: `df -h`
5. Verify file permissions: `ls -la man_base_data_pebble/`

For Dogecoin-specific troubleshooting, see: [Dogecoin Adapter Troubleshooting](DOGECOIN_ADAPTER.md#troubleshooting)

## Performance Optimization

### Database Optimization

```toml
[pebble]
dir = "./man_base_data_pebble"
num = 20  # Increase shards for better parallel performance
```

**Recommendations**:
- `num = 10`: Light usage, single-chain indexing
- `num = 20`: Medium usage, multi-chain indexing  
- `num = 30+`: Heavy usage, high-performance requirements

### Sync Optimization

```toml
# Skip early blocks without inscriptions
[btc]
initialHeight = 844446  # Start from first inscription block

[doge]
initialHeight = 5000000  # Skip early Dogecoin history

# Use appropriate database location
[pebble]
dir = "/fast/ssd/path/man_base_data_pebble"  # Use SSD for performance
```

### Network Optimization

```toml
# Increase RPC timeout for slow connections
[btc]
rpcTimeout = 60  # seconds

# Use local node for best performance
rpcHost = "127.0.0.1:8332"  # Local > Remote

# Enable connection pooling
rpcMaxConnections = 10
```

### Memory Management

```bash
# Set Go garbage collection target
export GOGC=50  # More aggressive GC (lower memory, slight CPU increase)

# Limit max memory usage
ulimit -v 8388608  # 8GB limit (in KB)

# Monitor memory usage
watch -n 5 'ps aux | grep manindexer'
```

## Production Deployment

### Using Docker

```dockerfile
# Build image
docker build -t manindexer:latest .

# Run container
docker run -d \
  --name manindexer \
  -p 8080:8080 \
  -v $(pwd)/config.toml:/app/config.toml \
  -v $(pwd)/data:/app/man_base_data_pebble \
  --restart unless-stopped \
  manindexer:latest \
  -chain btc,doge -config /app/config.toml
```

### Using Systemd

Create `/etc/systemd/system/manindexer.service`:

```ini
[Unit]
Description=MetaID Blockchain Indexer
After=network.target

[Service]
Type=simple
User=indexer
WorkingDirectory=/opt/manindexer
ExecStart=/opt/manindexer/manindexer -chain btc,doge -config /opt/manindexer/config.toml
Restart=on-failure
RestartSec=10
StandardOutput=append:/var/log/manindexer/output.log
StandardError=append:/var/log/manindexer/error.log

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/manindexer/man_base_data_pebble

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl daemon-reload
sudo systemctl enable manindexer
sudo systemctl start manindexer
sudo systemctl status manindexer
```

### Monitoring

```bash
# View logs
journalctl -u manindexer -f

# Check sync status
curl http://localhost:8080/api/count

# Monitor disk usage
du -sh man_base_data_pebble/

# Check process health
ps aux | grep manindexer
```

## Contributing

We welcome contributions from the community! Here's how you can help:

### Reporting Issues

- Use GitHub Issues for bug reports
- Include: OS, Go version, chain type, config, error logs
- Provide steps to reproduce the issue

### Pull Requests

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/your-feature`
3. Commit changes: `git commit -am 'Add your feature'`
4. Push to branch: `git push origin feature/your-feature`
5. Open a Pull Request with detailed description

### Development Guidelines

- Follow Go best practices and idioms
- Write unit tests for new features
- Update documentation for API changes
- Run `go fmt` and `go vet` before committing
- Keep commits atomic and well-described

### Code Review Process

1. All PRs require at least one review
2. CI/CD tests must pass
3. Documentation must be updated
4. Maintain backward compatibility when possible

## License

This project is licensed under the **MIT License** - see the [LICENSE](LICENSE) file for details.

## Related Projects

- [**metaid-cli**](https://github.com/metaid-developers/metaid-cli) - Command-line tool for creating MetaID inscriptions
- [**Dogecoin Core**](https://github.com/dogecoin/dogecoin) - Official Dogecoin blockchain client
- [**Bitcoin Core**](https://github.com/bitcoin/bitcoin) - Official Bitcoin blockchain client
- [**MVC**](https://github.com/mvc-labs) - MicroVision Chain official repositories

## Resources

- **Documentation**: [MetaID Protocol Docs](https://docs.metaid.io)
- **GitHub**: [metaid-developers](https://github.com/metaid-developers)
- **Community**: Join our Discord/Telegram (links TBD)
- **API Status**: Check service health at `/api/health`

## Acknowledgments

- Bitcoin Core development team for foundational blockchain technology
- Dogecoin community for continued support and enthusiasm
- MVC team for innovative blockchain solutions
- All contributors who have helped improve this project

## Support

For questions and support:

- 📖 Read the documentation first
- 🐛 Report bugs via GitHub Issues
- 💬 Join community discussions
- 📧 Contact maintainers (see MAINTAINERS file)

---