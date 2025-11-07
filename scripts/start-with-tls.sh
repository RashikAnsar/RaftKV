#!/bin/bash

# Start RaftKV with TLS enabled
# This script demonstrates how to run RaftKV with HTTPS and gRPC TLS

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}=================================${NC}"
echo -e "${BLUE}Starting RaftKV with TLS${NC}"
echo -e "${BLUE}=================================${NC}"
echo

# Check if certificates exist
if [ ! -f "certs/server-cert.pem" ]; then
    echo -e "${YELLOW}Certificates not found. Generating...${NC}"
    ./scripts/generate-certs.sh
fi

# Start the server with TLS
echo -e "${GREEN}Starting HTTPS server on port 8443...${NC}"
./kvstore \
    --http-addr=:8443 \
    --tls \
    --tls-cert=certs/server-cert.pem \
    --tls-key=certs/server-key.pem \
    --data-dir=./data \
    --log-level=info

echo
echo -e "${GREEN}Server started with TLS!${NC}"
echo
echo "Test with curl:"
echo "  curl --cacert certs/ca-cert.pem https://localhost:8443/health"
echo
echo "Put a key:"
echo "  curl --cacert certs/ca-cert.pem -X PUT https://localhost:8443/keys/mykey -d 'myvalue'"
echo
echo "Get a key:"
echo "  curl --cacert certs/ca-cert.pem https://localhost:8443/keys/mykey"
echo
