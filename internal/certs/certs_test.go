package certs

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestCertificateGeneration(t *testing.T) {
	t.Run("CA Certificate", func(t *testing.T) {
		testCACertificate(t)
	})

	t.Run("Server Certificate", func(t *testing.T) {
		testServerCertificate(t)
	})

	t.Run("Server Private Key", func(t *testing.T) {
		testPrivateKey(t, KeyPEM, "Server private key")
	})

	t.Run("Console Client Certificate", func(t *testing.T) {
		testClientCertificate(t)
	})

	t.Run("Console Client Private Key", func(t *testing.T) {
		testPrivateKey(t, ConsoleClientKeyPEM, "Console client private key")
	})

	t.Run("Certificate Chain Validation", func(t *testing.T) {
		testCertificateChainValidation(t)
	})
}

// testCACertificate tests the CA certificate properties
func testCACertificate(t *testing.T) {
	cert := parseCertificateFromPEM(t, CAPem, "CA certificate")

	if cert.Subject.CommonName != "Minexus-test CA" {
		t.Errorf("Expected CA CN 'Minexus-test CA', got '%s'", cert.Subject.CommonName)
	}

	if !cert.IsCA {
		t.Error("CA certificate should have IsCA=true")
	}
}

// testServerCertificate tests the server certificate properties
func testServerCertificate(t *testing.T) {
	cert := parseCertificateFromPEM(t, CertPEM, "Server certificate")

	if cert.Subject.CommonName != "nexus" {
		t.Errorf("Expected server CN 'nexus', got '%s'", cert.Subject.CommonName)
	}

	if cert.IsCA {
		t.Error("Server certificate should have IsCA=false")
	}

	validateExtKeyUsage(t, cert.ExtKeyUsage, x509.ExtKeyUsageServerAuth, "server authentication")
	validateDNSNames(t, cert.DNSNames, []string{"localhost", "nexus"})
}

// testClientCertificate tests the client certificate properties
func testClientCertificate(t *testing.T) {
	cert := parseCertificateFromPEM(t, ConsoleClientCertPEM, "Console client certificate")

	if cert.Subject.CommonName != "console" {
		t.Errorf("Expected client CN 'console', got '%s'", cert.Subject.CommonName)
	}

	if cert.IsCA {
		t.Error("Client certificate should have IsCA=false")
	}

	validateExtKeyUsage(t, cert.ExtKeyUsage, x509.ExtKeyUsageClientAuth, "client authentication")
}

// testPrivateKey tests private key properties
func testPrivateKey(t *testing.T, keyPEM []byte, keyName string) {
	if len(keyPEM) == 0 {
		t.Fatalf("%s is empty", keyName)
	}

	block, _ := pem.Decode(keyPEM)
	if block == nil {
		t.Fatalf("Failed to decode %s PEM", keyName)
	}

	if block.Type != "PRIVATE KEY" && block.Type != "RSA PRIVATE KEY" {
		t.Errorf("Expected PRIVATE KEY or RSA PRIVATE KEY, got %s", block.Type)
	}
}

// testCertificateChainValidation tests certificate chain validation
func testCertificateChainValidation(t *testing.T) {
	caCert := parseCertificateFromPEM(t, CAPem, "CA certificate")
	serverCert := parseCertificateFromPEM(t, CertPEM, "server certificate")
	clientCert := parseCertificateFromPEM(t, ConsoleClientCertPEM, "client certificate")

	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	verifyCertificate(t, serverCert, roots, x509.ExtKeyUsageServerAuth, "Server")
	verifyCertificate(t, clientCert, roots, x509.ExtKeyUsageClientAuth, "Client")
}

// parseCertificateFromPEM is a helper function to parse a certificate from PEM bytes
func parseCertificateFromPEM(t *testing.T, pemBytes []byte, certName string) *x509.Certificate {
	if len(pemBytes) == 0 {
		t.Fatalf("%s is empty", certName)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatalf("Failed to decode %s PEM", certName)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse %s: %v", certName, err)
	}

	return cert
}

// validateExtKeyUsage validates that a certificate has the expected extended key usage
func validateExtKeyUsage(t *testing.T, usages []x509.ExtKeyUsage, expected x509.ExtKeyUsage, usageName string) {
	for _, usage := range usages {
		if usage == expected {
			return
		}
	}
	t.Errorf("Certificate should have %s usage", usageName)
}

// validateDNSNames validates that a certificate has the expected DNS names
func validateDNSNames(t *testing.T, dnsNames []string, expectedNames []string) {
	for _, expected := range expectedNames {
		found := false
		for _, dns := range dnsNames {
			if dns == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Certificate should include '%s' in SANs", expected)
		}
	}
}

// verifyCertificate verifies a certificate against a root CA pool
func verifyCertificate(t *testing.T, cert *x509.Certificate, roots *x509.CertPool, keyUsage x509.ExtKeyUsage, certType string) {
	opts := x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{keyUsage},
	}
	if _, err := cert.Verify(opts); err != nil {
		t.Errorf("%s certificate verification failed: %v", certType, err)
	}
}
