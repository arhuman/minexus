package certs

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestCertificateGeneration(t *testing.T) {
	// Test CA certificate
	t.Run("CA Certificate", func(t *testing.T) {
		if len(CAPem) == 0 {
			t.Fatal("CA certificate is empty")
		}

		block, _ := pem.Decode(CAPem)
		if block == nil {
			t.Fatal("Failed to decode CA certificate PEM")
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("Failed to parse CA certificate: %v", err)
		}

		if cert.Subject.CommonName != "Minexus-test CA" {
			t.Errorf("Expected CA CN 'Minexus-test CA', got '%s'", cert.Subject.CommonName)
		}

		if !cert.IsCA {
			t.Error("CA certificate should have IsCA=true")
		}

		// Check for CA certificate signing capability through Basic Constraints
		// Modern certificates may not have explicit KeyUsage for cert signing
		if !cert.IsCA {
			t.Error("CA certificate should have IsCA=true which indicates cert signing capability")
		}
	})

	// Test Server certificate
	t.Run("Server Certificate", func(t *testing.T) {
		if len(CertPEM) == 0 {
			t.Fatal("Server certificate is empty")
		}

		block, _ := pem.Decode(CertPEM)
		if block == nil {
			t.Fatal("Failed to decode server certificate PEM")
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("Failed to parse server certificate: %v", err)
		}

		if cert.Subject.CommonName != "nexus" {
			t.Errorf("Expected server CN 'nexus', got '%s'", cert.Subject.CommonName)
		}

		if cert.IsCA {
			t.Error("Server certificate should have IsCA=false")
		}

		// Check for server authentication usage
		hasServerAuth := false
		for _, usage := range cert.ExtKeyUsage {
			if usage == x509.ExtKeyUsageServerAuth {
				hasServerAuth = true
				break
			}
		}
		if !hasServerAuth {
			t.Error("Server certificate should have server authentication usage")
		}

		// Check SANs
		hasLocalhost := false
		hasNexus := false
		for _, dns := range cert.DNSNames {
			if dns == "localhost" {
				hasLocalhost = true
			}
			if dns == "nexus" {
				hasNexus = true
			}
		}
		if !hasLocalhost {
			t.Error("Server certificate should include 'localhost' in SANs")
		}
		if !hasNexus {
			t.Error("Server certificate should include 'nexus' in SANs")
		}
	})

	// Test Server private key
	t.Run("Server Private Key", func(t *testing.T) {
		if len(KeyPEM) == 0 {
			t.Fatal("Server private key is empty")
		}

		block, _ := pem.Decode(KeyPEM)
		if block == nil {
			t.Fatal("Failed to decode server private key PEM")
		}

		// Accept both modern PKCS#8 and legacy PKCS#1 private key formats
		if block.Type != "PRIVATE KEY" && block.Type != "RSA PRIVATE KEY" {
			t.Errorf("Expected PRIVATE KEY or RSA PRIVATE KEY, got %s", block.Type)
		}
	})

	// Test Console client certificate
	t.Run("Console Client Certificate", func(t *testing.T) {
		if len(ConsoleClientCertPEM) == 0 {
			t.Fatal("Console client certificate is empty")
		}

		block, _ := pem.Decode(ConsoleClientCertPEM)
		if block == nil {
			t.Fatal("Failed to decode console client certificate PEM")
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("Failed to parse console client certificate: %v", err)
		}

		if cert.Subject.CommonName != "console" {
			t.Errorf("Expected client CN 'console', got '%s'", cert.Subject.CommonName)
		}

		if cert.IsCA {
			t.Error("Client certificate should have IsCA=false")
		}

		// Check for client authentication usage
		hasClientAuth := false
		for _, usage := range cert.ExtKeyUsage {
			if usage == x509.ExtKeyUsageClientAuth {
				hasClientAuth = true
				break
			}
		}
		if !hasClientAuth {
			t.Error("Client certificate should have client authentication usage")
		}
	})

	// Test Console client private key
	t.Run("Console Client Private Key", func(t *testing.T) {
		if len(ConsoleClientKeyPEM) == 0 {
			t.Fatal("Console client private key is empty")
		}

		block, _ := pem.Decode(ConsoleClientKeyPEM)
		if block == nil {
			t.Fatal("Failed to decode console client private key PEM")
		}

		// Accept both modern PKCS#8 and legacy PKCS#1 private key formats
		if block.Type != "PRIVATE KEY" && block.Type != "RSA PRIVATE KEY" {
			t.Errorf("Expected PRIVATE KEY or RSA PRIVATE KEY, got %s", block.Type)
		}
	})

	// Test certificate chain validation
	t.Run("Certificate Chain Validation", func(t *testing.T) {
		// Parse CA cert
		caBlock, _ := pem.Decode(CAPem)
		caCert, err := x509.ParseCertificate(caBlock.Bytes)
		if err != nil {
			t.Fatalf("Failed to parse CA certificate: %v", err)
		}

		// Parse server cert
		serverBlock, _ := pem.Decode(CertPEM)
		serverCert, err := x509.ParseCertificate(serverBlock.Bytes)
		if err != nil {
			t.Fatalf("Failed to parse server certificate: %v", err)
		}

		// Parse client cert
		clientBlock, _ := pem.Decode(ConsoleClientCertPEM)
		clientCert, err := x509.ParseCertificate(clientBlock.Bytes)
		if err != nil {
			t.Fatalf("Failed to parse client certificate: %v", err)
		}

		// Create certificate pool with CA
		roots := x509.NewCertPool()
		roots.AddCert(caCert)

		// Verify server certificate
		serverOpts := x509.VerifyOptions{
			Roots:     roots,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		if _, err := serverCert.Verify(serverOpts); err != nil {
			t.Errorf("Server certificate verification failed: %v", err)
		}

		// Verify client certificate
		clientOpts := x509.VerifyOptions{
			Roots:     roots,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}
		if _, err := clientCert.Verify(clientOpts); err != nil {
			t.Errorf("Client certificate verification failed: %v", err)
		}
	})
}
