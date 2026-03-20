#!/bin/bash
curl --user test:test \
     --data-binary "{\"jsonrpc\":\"1.0\",\"id\":\"test\",\"method\":\"$1\",\"params\":[$2]}" \
     -H 'content-type: text/plain;' \
     http://127.0.0.1:18443/ 2>/dev/null
