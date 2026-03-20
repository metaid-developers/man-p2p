#!/bin/bash

# Dogecoin Indexer Quick Start Script
# This script helps you quickly set up and run the Dogecoin indexer

set -e

echo "========================================"
echo "   Dogecoin Indexer Quick Start"
echo "========================================"
echo ""

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Check if Dogecoin node is running
check_dogecoin_node() {
    echo -e "${BLUE}Checking Dogecoin node connection...${NC}"
    if curl -s --user "${DOGE_RPC_USER}:${DOGE_RPC_PASS}" \
        --data-binary '{"jsonrpc":"1.0","id":"1","method":"getblockchaininfo","params":[]}' \
        -H 'content-type: text/plain;' \
        "${DOGE_RPC_HOST}" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Dogecoin node is running${NC}"
        return 0
    else
        echo -e "${RED}✗ Cannot connect to Dogecoin node${NC}"
        return 1
    fi
}

# Start Dogecoin regtest node
start_dogecoin_regtest() {
    echo -e "${BLUE}Starting Dogecoin regtest node...${NC}"
    docker run -d --name dogecoin-regtest \
        -p 18332:18332 \
        -p 18444:18444 \
        -e NETWORK=regtest \
        -e RPC_USER=regtest \
        -e RPC_PASSWORD=regtest \
        ich777/dogecoin-core
    
    echo -e "${GREEN}✓ Dogecoin regtest node started${NC}"
    echo "Waiting for node to initialize..."
    sleep 5
}

# Generate test blocks
generate_test_blocks() {
    echo -e "${BLUE}Generating test blocks...${NC}"
    local address=$1
    curl --user regtest:regtest --data-binary \
        "{\"jsonrpc\":\"1.0\",\"id\":\"1\",\"method\":\"generatetoaddress\",\"params\":[101,\"${address}\"]}" \
        -H 'content-type: text/plain;' \
        http://127.0.0.1:18332/ > /dev/null 2>&1
    echo -e "${GREEN}✓ Generated 101 test blocks${NC}"
}

# Build indexer
build_indexer() {
    echo -e "${BLUE}Building indexer...${NC}"
    go build -o manindexer .
    echo -e "${GREEN}✓ Indexer built successfully${NC}"
}

# Main menu
main_menu() {
    echo ""
    echo "Please select an option:"
    echo "1) Start Dogecoin Regtest + Indexer (Development)"
    echo "2) Start Dogecoin Testnet Indexer"
    echo "3) Start Dogecoin Mainnet Indexer"
    echo "4) Build only"
    echo "5) Exit"
    echo ""
    read -p "Enter your choice [1-5]: " choice

    case $choice in
        1)
            start_regtest_setup
            ;;
        2)
            start_testnet_indexer
            ;;
        3)
            start_mainnet_indexer
            ;;
        4)
            build_indexer
            ;;
        5)
            echo "Exiting..."
            exit 0
            ;;
        *)
            echo -e "${RED}Invalid option${NC}"
            main_menu
            ;;
    esac
}

# Start regtest setup
start_regtest_setup() {
    echo ""
    echo "========================================"
    echo "   Dogecoin Regtest Setup"
    echo "========================================"
    echo ""

    # Check if Docker is running
    if ! docker ps > /dev/null 2>&1; then
        echo -e "${RED}Error: Docker is not running${NC}"
        exit 1
    fi

    # Check if dogecoin-regtest container exists
    if docker ps -a | grep -q dogecoin-regtest; then
        echo "Dogecoin regtest container already exists"
        read -p "Do you want to restart it? (y/n): " restart
        if [ "$restart" = "y" ]; then
            docker stop dogecoin-regtest 2>/dev/null || true
            docker rm dogecoin-regtest 2>/dev/null || true
            start_dogecoin_regtest
        else
            docker start dogecoin-regtest 2>/dev/null || true
        fi
    else
        start_dogecoin_regtest
    fi

    # Set environment variables
    export DOGE_RPC_USER="regtest"
    export DOGE_RPC_PASS="regtest"
    export DOGE_RPC_HOST="http://127.0.0.1:18332"

    # Check connection
    if check_dogecoin_node; then
        echo ""
        echo -e "${GREEN}Dogecoin node is ready!${NC}"
        
        # Build indexer
        build_indexer

        echo ""
        echo "Starting indexer in 3 seconds..."
        sleep 3

        # Start indexer
        ./manindexer \
            -chain doge \
            -test 2 \
            -doge_rpc_host 127.0.0.1:18332 \
            -doge_rpc_user regtest \
            -doge_rpc_password regtest \
            -doge_zmqpubrawtx tcp://127.0.0.1:18444 \
            -doge_height 0 \
            -config config_doge.toml
    else
        echo -e "${RED}Failed to connect to Dogecoin node${NC}"
        exit 1
    fi
}

# Start testnet indexer
start_testnet_indexer() {
    echo ""
    echo "========================================"
    echo "   Dogecoin Testnet Indexer"
    echo "========================================"
    echo ""

    read -p "Enter Dogecoin testnet RPC host [127.0.0.1:44555]: " rpc_host
    rpc_host=${rpc_host:-127.0.0.1:44555}

    read -p "Enter RPC username: " rpc_user
    read -sp "Enter RPC password: " rpc_pass
    echo ""

    read -p "Enter ZMQ address [tcp://127.0.0.1:44556]: " zmq_host
    zmq_host=${zmq_host:-tcp://127.0.0.1:44556}

    read -p "Enter initial block height [0]: " initial_height
    initial_height=${initial_height:-0}

    build_indexer

    echo ""
    echo "Starting Dogecoin testnet indexer..."
    ./manindexer \
        -chain doge \
        -test 1 \
        -doge_rpc_host "$rpc_host" \
        -doge_rpc_user "$rpc_user" \
        -doge_rpc_password "$rpc_pass" \
        -doge_zmqpubrawtx "$zmq_host" \
        -doge_height "$initial_height" \
        -config config_doge.toml
}

# Start mainnet indexer
start_mainnet_indexer() {
    echo ""
    echo "========================================"
    echo "   Dogecoin Mainnet Indexer"
    echo "========================================"
    echo ""

    read -p "Enter Dogecoin mainnet RPC host [127.0.0.1:22555]: " rpc_host
    rpc_host=${rpc_host:-127.0.0.1:22555}

    read -p "Enter RPC username: " rpc_user
    read -sp "Enter RPC password: " rpc_pass
    echo ""

    read -p "Enter ZMQ address [tcp://127.0.0.1:28555]: " zmq_host
    zmq_host=${zmq_host:-tcp://127.0.0.1:28555}

    read -p "Enter initial block height [0]: " initial_height
    initial_height=${initial_height:-0}

    build_indexer

    echo ""
    echo "Starting Dogecoin mainnet indexer..."
    ./manindexer \
        -chain doge \
        -doge_rpc_host "$rpc_host" \
        -doge_rpc_user "$rpc_user" \
        -doge_rpc_password "$rpc_pass" \
        -doge_zmqpubrawtx "$zmq_host" \
        -doge_height "$initial_height" \
        -config config_doge.toml
}

# Run main menu
main_menu
