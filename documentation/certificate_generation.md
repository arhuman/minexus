# Certificate Generation Guide for mTLS Implementation

This guide explains how to generate and manage the static certificates used for mTLS authentication between console and nexus components.

## Overview

The mTLS implementation uses a static PKI (Public Key Infrastructure) with embedded certificates to ensure consistent authentication across all builds and deployments. The certificates are stored in [`internal/certs/files/`](../internal/certs/files/) and embedded during compilation.

## Certificate Structure

The PKI consists of three main components:

1. **Certificate Authority (CA)**: [`ca.crt`](../internal/certs/files/ca.crt) + [`ca.key`](../internal/certs/files/ca.key)
2. **Server Certificate**: [`server.crt`](../internal/certs/files/server.crt) + [`server.key`](../internal/certs/files/server.key)
3. **Console Client Certificate**: [`console.crt`](../internal/certs/files/console.crt) + [`console.key`](../internal/certs/files/console.key)

## Certificate Generation Steps

### Automated Generation

A script is provided to automate the generation of all necessary certificates. To use it, run the following command from the root of the repository:

```bash
./internal/certs/files/mkcerts.sh <nexus_hostname_or_ip> "/CN=Minexus CA/O=Minexus" <destination_directory>
```

For example, to generate certificates for a Nexus server running on `localhost` and store them in `internal/certs/files/dev`, you would run:

```bash
./internal/certs/files/mkcerts.sh localhost "/CN=Minexus CA/O=Minexus" internal/certs/files/dev
```

### Manual Generation

### 1. Generate Certificate Authority

```bash
# Create certificate files directory
mkdir -p internal/certs/files
cd internal/certs/files

# Generate CA private key (4096-bit RSA)
openssl genrsa -out ca.key 4096

# Generate self-signed CA certificate (10-year validity)
openssl req -new -x509 -key ca.key -sha256 -subj "/CN=Minexus CA/O=Minexus" -days 3650 -out ca.crt
```

### 2. Generate Server Certificate

Create server certificate configuration:

```bash
cat > server.conf << EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = nexus
O = Minexus

[v3_req]
keyUsage = keyEncipherment, dataEncipherment, digitalSignature
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = nexus
DNS.2 = localhost
IP.1 = 127.0.0.1
IP.2 = ::1
EOF
```

Generate server certificate:

```bash
# Generate server private key
openssl genrsa -out server.key 4096

# Generate certificate signing request
openssl req -new -key server.key -out server.csr -config server.conf

# Sign with CA (10-year validity)
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 3650 -sha256 -extensions v3_req -extfile server.conf
```

### 3. Generate Console Client Certificate

Create console certificate configuration:

```bash
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
```

Generate console client certificate:

```bash
# Generate console private key
openssl genrsa -out console.key 4096

# Generate certificate signing request
openssl req -new -key console.key -out console.csr -config console.conf

# Sign with CA (10-year validity)
openssl x509 -req -in console.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out console.crt -days 3650 -sha256 -extensions v3_req -extfile console.conf
```

## Certificate Verification

Verify the certificate chain:

```bash
# Verify server certificate against CA
openssl verify -CAfile ca.crt server.crt

# Verify console certificate against CA
openssl verify -CAfile ca.crt console.crt

# Check certificate details
openssl x509 -in server.crt -text -noout
openssl x509 -in console.crt -text -noout
```

## Certificate Integration

The certificates are automatically embedded in the Go binaries using `go:embed` directives in [`internal/certs/certs.go`](../internal/certs/certs.go):

```go
package certs

import _ "embed"

var (
    // Certificate Authority
    //go:embed files/ca.crt
    CAPem []byte

    // Server Certificate (signed by CA)
    //go:embed files/server.crt
    CertPEM []byte

    //go:embed files/server.key
    KeyPEM []byte

    // Console Client Certificate (signed by CA)
    //go:embed files/console.crt
    ConsoleClientCertPEM []byte

    //go:embed files/console.key
    ConsoleClientKeyPEM []byte
)
```

## Certificate Lifecycle

### Current Certificates
- **Validity**: 10 years from generation date
- **Key Size**: 4096-bit RSA
- **Hash Algorithm**: SHA-256
- **Server SAN**: localhost, 127.0.0.1, ::1, nexus

### Certificate Rotation

To rotate certificates:

1. **Generate new certificates** following the steps above
2. **Replace files** in [`internal/certs/files/`](../internal/certs/files/)
3. **Rebuild all components**:
   ```bash
   go build -o console ./cmd/console
   docker compose build --no-cache nexus
   docker compose up -d nexus minion
   ```
4. **Test the new certificates**:
   ```bash
   SLOW_TESTS=1 go test -v -run TestIntegrationSuite/MTLSConnectivity
   ```

## Security Considerations

### Advantages of Static Certificates
- **Consistent PKI** across all builds and deployments
- **Zero-config deployment** - no external certificate management
- **Simplified testing** - certificates don't change between builds
- **Docker compatibility** - same certificates in containers and local binaries

### Security Implications
- **Private keys embedded** in binaries (acceptable for closed-source deployment)
- **Certificate rotation** requires rebuilding binaries
- **No external CA dependency** - self-contained security model

### Production Considerations

For production environments requiring external certificate management:

1. **External Certificate Override**: Consider adding environment variables to override embedded certificates:
   ```bash
   MTLS_CA_CERT_FILE=/path/to/ca.crt
   MTLS_SERVER_CERT_FILE=/path/to/server.crt
   MTLS_SERVER_KEY_FILE=/path/to/server.key
   MTLS_CLIENT_CERT_FILE=/path/to/client.crt
   MTLS_CLIENT_KEY_FILE=/path/to/client.key
   ```

2. **Certificate Monitoring**: Implement certificate expiration monitoring
3. **Automated Rotation**: Develop automation for certificate lifecycle management

## Troubleshooting

### Common Issues

**Certificate Mismatch Errors**:
```
x509: certificate signed by unknown authority
```
- Ensure all components built with same certificate files
- Verify CA certificate is consistent across all builds

**Hostname Verification Failures**:
```
x509: certificate is not valid for any names, but wanted to match nexus
```
- Check server certificate SAN includes required hostnames
- Verify ServerName in client configuration matches certificate CN

**Permission Errors**:
```
permission denied reading certificate files
```
- Check file permissions on certificate files
- Ensure build process has access to certificate directory

### Debugging Commands

```bash
# Check certificate chain
openssl verify -CAfile internal/certs/files/ca.crt internal/certs/files/server.crt
openssl verify -CAfile internal/certs/files/ca.crt internal/certs/files/console.crt

# Test mTLS connection manually
openssl s_client -connect localhost:11973 -cert internal/certs/files/console.crt \
  -key internal/certs/files/console.key -CAfile internal/certs/files/ca.crt

# Check certificate expiration
openssl x509 -in internal/certs/files/server.crt -noout -dates
openssl x509 -in internal/certs/files/console.crt -noout -dates
```

## Conclusion

The static certificate approach provides a robust, zero-config mTLS implementation that ensures consistent authentication across all deployment scenarios while maintaining the simplicity of embedded certificates. The 10-year validity period minimizes operational overhead while providing strong security for the console-nexus communication channel.