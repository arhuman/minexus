#!/bin/bash

# This script generates the necessary certificates for the Minexus mTLS implementation.
# It takes three arguments:
# 1. The hostname or IP address of the Nexus server.
# 2. The distinguished name for the certificates (e.g., "/CN=Minexus CA/O=Minexus").
# 3. The destination directory for the generated certificates.

set -e

if [ "$#" -ne 3 ]; then
    echo "Usage: $0 <nexus_hostname_or_ip> <distinguished_name> <destination_directory>"
    exit 1
fi

NEXUS_HOST=$1
DIST_NAME=$2
DEST_DIR=$3

mkdir -p "$DEST_DIR"
cd "$DEST_DIR"

# 1. Generate Certificate Authority
echo "Generating Certificate Authority..."
openssl genrsa -out ca.key 4096
openssl req -new -x509 -key ca.key -sha256 -subj "$DIST_NAME" -days 3650 -out ca.crt

# 2. Generate Server Certificate
echo "Generating Server Certificate..."
cat > server.conf << EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = $NEXUS_HOST
O = Minexus

[v3_req]
keyUsage = keyEncipherment, dataEncipherment, digitalSignature
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = $NEXUS_HOST
DNS.2 = localhost
IP.1 = 127.0.0.1
IP.2 = ::1
EOF

openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr -config server.conf
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 3650 -sha256 -extensions v3_req -extfile server.conf

# 3. Generate Console Client Certificate
echo "Generating Console Client Certificate..."
cat > console.conf << EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = console
O = Minexus

[v3_req]
keyUsage = keyEncipherment, dataEncipherment, digitalSignature
extendedKeyUsage = clientAuth
EOF

openssl genrsa -out console.key 4096
openssl req -new -key console.key -out console.csr -config console.conf
openssl x509 -req -in console.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out console.crt -days 3650 -sha256 -extensions v3_req -extfile console.conf

# 4. Verify Certificates
echo "Verifying Certificates..."
openssl verify -CAfile ca.crt server.crt
openssl verify -CAfile ca.crt console.crt

echo "Certificates generated successfully in $DEST_DIR"
