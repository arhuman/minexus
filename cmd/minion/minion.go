// Package main implements the Minion command-line application.
// Minion is a worker node that connects to a Nexus server to receive and execute commands.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arhuman/minexus/internal/certs"
	"github.com/arhuman/minexus/internal/config"
	"github.com/arhuman/minexus/internal/logging"
	"github.com/arhuman/minexus/internal/minion"
	"github.com/arhuman/minexus/internal/version"
	pb "github.com/arhuman/minexus/protogen"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

// setupGRPCConnection establishes connection to the server
func setupGRPCConnection(cfg *config.MinionConfig, logger *zap.Logger) (*grpc.ClientConn, error) {
	logger, start := logging.FuncLogger(logger, "setupGRPCConnection")
	defer logging.FuncExit(logger, start)

	// Configure TLS credentials (mandatory, embedded)
	logger.Info("Configuring embedded TLS for minion client")

	cert, err := tls.X509KeyPair(certs.CertPEM, certs.KeyPEM)
	if err != nil {
		logger.Error("Failed to load embedded TLS certificates", zap.Error(err))
		return nil, fmt.Errorf("failed to load embedded TLS certificates: %w", err)
	}

	// Create certificate pool with the CA certificate
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(certs.CAPem) {
		logger.Error("Failed to load CA certificate")
		return nil, fmt.Errorf("failed to load CA certificate")
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caCertPool,
		InsecureSkipVerify: false,
	}

	creds := credentials.NewTLS(tlsConfig)
	logger.Info("Embedded TLS credentials configured for minion client with CA validation")

	// Create connection using modern gRPC pattern with timeout, keepalive and connection parameters
	conn, err := grpc.NewClient(cfg.ServerAddr,
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                60 * time.Second, // Send pings every 60 seconds
			Timeout:             20 * time.Second, // Wait 20 seconds for ping ack
			PermitWithoutStream: true,             // Allow pings even without active streams
		}),
		grpc.WithConnectParams(grpc.ConnectParams{
			MinConnectTimeout: time.Duration(cfg.ConnectTimeout) * time.Second,
		}),
	)

	return conn, err
}

func main() {
	// Check for version flag
	if version.CheckAndHandleVersionFlag("Minion") {
		return
	}

	// Load configuration from environment, .env file, and command line flags
	cfg, err := config.LoadMinionConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Set up logging with atomic level for dynamic log level control
	logger, atom, err := logging.SetupLogger(cfg.Debug)
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}
	defer logger.Sync()

	// Display version information
	logger.Info("Starting Minion", zap.String("version", version.Component("Minion")))

	// Log the configuration
	cfg.LogConfig(logger)

	logger.Info("Connecting to server", zap.String("address", cfg.ServerAddr))

	// Set up gRPC connection to the server with configurable timeout
	conn, err := setupGRPCConnection(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to connect to server", zap.Error(err), zap.String("address", cfg.ServerAddr))
	}
	defer conn.Close()
	logger.Info("Connected to Nexus server")

	// Create the gRPC client
	minionClient := pb.NewMinionServiceClient(conn)

	// Create minion instance with configurable intervals
	heartbeatInterval := time.Duration(cfg.HeartbeatInterval) * time.Second
	initialReconnectDelay := time.Duration(cfg.InitialReconnectDelay) * time.Second
	maxReconnectDelay := time.Duration(cfg.MaxReconnectDelay) * time.Second
	shellTimeout := time.Duration(cfg.DefaultShellTimeout) * time.Second
	streamTimeout := time.Duration(cfg.StreamTimeout) * time.Second
	m := minion.NewMinion(cfg.ID, minionClient, heartbeatInterval, initialReconnectDelay, maxReconnectDelay, shellTimeout, streamTimeout, logger, atom)

	// Create context that can be canceled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start minion
	if err := m.Start(ctx); err != nil {
		logger.Fatal("Failed to start minion", zap.Error(err))
	}
	logger.Info("Minion started successfully")

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for termination signal
	<-sigChan
	logger.Info("Received termination signal, shutting down...")

	// Stop minion gracefully
	m.Stop()
	logger.Info("Minion stopped")
}
