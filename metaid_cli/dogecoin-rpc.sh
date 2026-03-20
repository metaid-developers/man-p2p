#!/bin/bash
# Dogecoin RPC wrapper script using docker exec
# Usage: ./dogecoin-rpc.sh <method> [params...]

# 检查是否提供了方法
if [ -z "$1" ]; then
    echo "Usage: $0 <method> [params...]"
    echo "Example: $0 getblockchaininfo"
    exit 1
fi

# 直接透传所有参数给 dogecoin-cli
docker exec dogecoin-node /dogecoin/Dogecoin/bin/dogecoin-cli -regtest -rpcuser=dogeuser -rpcpassword=dogepass "$@"
