package certs

import _ "embed"

// Static certificate files embedded at build time
// These certificates provide a consistent PKI across all builds and deployments

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
