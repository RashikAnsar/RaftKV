#!/bin/bash

# Start RaftKV with mutual TLS (mTLS) enabled
# This requires both server AND client certificates

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}=================================${NC}"
echo -e "${BLUE}Starting RaftKV with mTLS${NC}"
echo -e "${BLUE}=================================${NC}"
echo

# Check if certificates exist
if [ ! -f "certs/server-cert.pem" ]; then
    echo -e "${YELLOW}Certificates not found. Generating...${NC}"
    ./scripts/generate-certs.sh
fi

# Start the server with mTLS
echo -e "${GREEN}Starting HTTPS server with client authentication on port 8443...${NC}"
./kvstore \
    --http-addr=:8443 \
    --tls \
    --mtls \
    --tls-cert=certs/server-cert.pem \
    --tls-key=certs/server-key.pem \
    --tls-ca=certs/ca-cert.pem \
    --data-dir=./data \
    --log-level=info

echo
echo -e "${GREEN}Server started with mTLS!${NC}"
echo
echo "Test with curl (requires client certificate):"
echo "  curl --cacert certs/ca-cert.pem \\"
echo "       --cert certs/client-cert.pem \\"
echo "       --key certs/client-key.pem \\"
echo "       https://localhost:8443/health"
echo
echo "Put a key:"
echo "  curl --cacert certs/ca-cert.pem \\"
echo "       --cert certs/client-cert.pem \\"
echo "       --key certs/client-key.pem \\"
echo "       -X PUT https://localhost:8443/keys/mykey -d 'myvalue'"
echo
