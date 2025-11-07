#!/bin/bash

# RaftKV Certificate Generation Script
# Generates self-signed certificates for development and testing
# For production, use properly signed certificates from a CA

set -e

CERT_DIR="certs"
VALIDITY_DAYS=365

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}==================================${NC}"
echo -e "${BLUE}RaftKV Certificate Generator${NC}"
echo -e "${BLUE}==================================${NC}"
echo

# Create certs directory
echo -e "${YELLOW}Creating certificate directory...${NC}"
mkdir -p $CERT_DIR
cd $CERT_DIR

# Generate CA
echo -e "${YELLOW}Generating Certificate Authority (CA)...${NC}"
openssl req -new -x509 -days $VALIDITY_DAYS \
  -keyout ca-key.pem \
  -out ca-cert.pem \
  -nodes \
  -subj "/C=US/ST=State/L=City/O=RaftKV/CN=RaftKV Test CA" \
  2>/dev/null

echo -e "${GREEN}✓ CA certificate generated${NC}"

# Generate server certificate
echo -e "${YELLOW}Generating server certificate...${NC}"
openssl genrsa -out server-key.pem 2048 2>/dev/null
openssl req -new \
  -key server-key.pem \
  -out server-req.pem \
  -nodes \
  -subj "/C=US/ST=State/L=City/O=RaftKV/CN=localhost" \
  2>/dev/null

# Create server certificate extensions file
cat > server-ext.cnf <<EOF
subjectAltName = @alt_names
extendedKeyUsage = serverAuth

[alt_names]
DNS.1 = localhost
DNS.2 = *.localhost
IP.1 = 127.0.0.1
IP.2 = ::1
EOF

openssl x509 -req \
  -in server-req.pem \
  -CA ca-cert.pem \
  -CAkey ca-key.pem \
  -CAcreateserial \
  -out server-cert.pem \
  -days $VALIDITY_DAYS \
  -extfile server-ext.cnf \
  2>/dev/null

echo -e "${GREEN}✓ Server certificate generated${NC}"

# Generate client certificate
echo -e "${YELLOW}Generating client certificate...${NC}"
openssl genrsa -out client-key.pem 2048 2>/dev/null
openssl req -new \
  -key client-key.pem \
  -out client-req.pem \
  -nodes \
  -subj "/C=US/ST=State/L=City/O=RaftKV/CN=client" \
  2>/dev/null

# Create client certificate extensions file
cat > client-ext.cnf <<EOF
extendedKeyUsage = clientAuth
EOF

openssl x509 -req \
  -in client-req.pem \
  -CA ca-cert.pem \
  -CAkey ca-key.pem \
  -CAcreateserial \
  -out client-cert.pem \
  -days $VALIDITY_DAYS \
  -extfile client-ext.cnf \
  2>/dev/null

echo -e "${GREEN}✓ Client certificate generated${NC}"

# Generate certificates for each Raft node (node1, node2, node3)
for i in 1 2 3; do
  NODE="node$i"
  echo -e "${YELLOW}Generating certificate for $NODE...${NC}"

  openssl genrsa -out $NODE-key.pem 2048 2>/dev/null
  openssl req -new \
    -key $NODE-key.pem \
    -out $NODE-req.pem \
    -nodes \
    -subj "/C=US/ST=State/L=City/O=RaftKV/CN=$NODE" \
    2>/dev/null

  # Create node certificate extensions file
  cat > $NODE-ext.cnf <<EOF
subjectAltName = @alt_names
extendedKeyUsage = serverAuth, clientAuth

[alt_names]
DNS.1 = $NODE
DNS.2 = localhost
IP.1 = 127.0.0.1
EOF

  openssl x509 -req \
    -in $NODE-req.pem \
    -CA ca-cert.pem \
    -CAkey ca-key.pem \
    -CAcreateserial \
    -out $NODE-cert.pem \
    -days $VALIDITY_DAYS \
    -extfile $NODE-ext.cnf \
    2>/dev/null

  echo -e "${GREEN}✓ Certificate for $NODE generated${NC}"
done

# Set secure permissions
echo -e "${YELLOW}Setting secure permissions...${NC}"
chmod 600 *.pem
chmod 644 *-cert.pem ca-cert.pem

# Clean up temporary files
rm -f *.cnf *.req *.srl

echo
echo -e "${GREEN}==================================${NC}"
echo -e "${GREEN}Certificate generation complete!${NC}"
echo -e "${GREEN}==================================${NC}"
echo
echo "Generated certificates:"
echo "  CA:     ca-cert.pem, ca-key.pem"
echo "  Server: server-cert.pem, server-key.pem"
echo "  Client: client-cert.pem, client-key.pem"
echo "  Nodes:  node1-cert.pem, node2-cert.pem, node3-cert.pem"
echo
echo "Certificate validity: $VALIDITY_DAYS days"
echo
echo "Usage examples:"
echo "  HTTP Server:  --tls-cert=certs/server-cert.pem --tls-key=certs/server-key.pem"
echo "  gRPC Server:  --grpc-tls-cert=certs/server-cert.pem --grpc-tls-key=certs/server-key.pem"
echo "  mTLS:         --tls-ca=certs/ca-cert.pem --tls-mtls"
echo
echo -e "${YELLOW}⚠️  These are self-signed certificates for development only!${NC}"
echo -e "${YELLOW}   For production, use certificates from a trusted CA.${NC}"
echo
