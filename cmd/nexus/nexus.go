// Package main provides the Nexus server command-line interface.
// Nexus is a gRPC server that manages minions and console connections.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/arhuman/minexus/internal/certs"
	"github.com/arhuman/minexus/internal/config"
	"github.com/arhuman/minexus/internal/logging"
	"github.com/arhuman/minexus/internal/nexus"
	"github.com/arhuman/minexus/internal/version"
	"github.com/arhuman/minexus/internal/web"
	pb "github.com/arhuman/minexus/protogen"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

func main() {
	// Check for version flag
	if version.CheckAndHandleVersionFlag("Nexus") {
		return
	}

	// Load configuration from environment, .env file, and command line flags
	cfg, err := config.LoadNexusConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Set up logging
	logger, _, err := logging.SetupLogger(cfg.Debug)
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}
	defer logger.Sync()

	logger, start := logging.FuncLogger(logger, "main")
	defer logging.FuncExit(logger, start)

	// Display version information
	logger.Info("Starting Nexus with triple-server architecture", zap.String("version", version.Component("Nexus")))

	// Log the configuration (with sensitive data masked)
	cfg.LogConfig(logger)

	// Create nexus server
	nexusServer, err := nexus.NewServer(cfg.DBConnectionString(), logger)
	if err != nil {
		logger.Fatal("Failed to create server", zap.Error(err))
	}
	defer nexusServer.Shutdown()

	// Load server certificate for both servers
	logger.Info("Loading embedded TLS certificates")
	serverCert, err := tls.X509KeyPair(certs.CertPEM, certs.KeyPEM)
	if err != nil {
		logger.Fatal("Failed to load embedded TLS certificates", zap.Error(err))
	}

	// Parse CA certificate for mTLS client verification
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(certs.CAPem) {
		logger.Fatal("Failed to parse CA certificate")
	}

	// Create minion server (standard TLS)
	minionServer := createMinionServer(cfg, serverCert, logger)
	minionListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.MinionPort))
	if err != nil {
		logger.Fatal("Failed to create minion listener", zap.Error(err))
	}

	// Create console server (mTLS)
	consoleServer := createConsoleServer(cfg, serverCert, caCertPool, logger)
	consoleListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.ConsolePort))
	if err != nil {
		logger.Fatal("Failed to create console listener", zap.Error(err))
	}

	// Register services on both servers
	pb.RegisterMinionServiceServer(minionServer, nexusServer)
	pb.RegisterConsoleServiceServer(consoleServer, nexusServer)

	// Register reflection service for grpcurl and similar tools
	reflection.Register(minionServer)
	reflection.Register(consoleServer)

	// Start all three servers concurrently
	var wg sync.WaitGroup
	var serverReady sync.WaitGroup
	wg.Add(3)
	serverReady.Add(3)

	// Start minion server
	go func() {
		defer wg.Done()
		logger.Info("Minion server starting (TLS)",
			zap.String("address", minionListener.Addr().String()),
			zap.Int("port", cfg.MinionPort))

		// Signal server is about to start
		go func() {
			time.Sleep(100 * time.Millisecond) // Brief delay for server to initialize
			logger.Info("Minion server ready for connections", zap.Int("port", cfg.MinionPort))
			serverReady.Done()
		}()

		if err := minionServer.Serve(minionListener); err != nil {
			logger.Error("Minion server failed", zap.Error(err))
		}
	}()

	// Start console server
	go func() {
		defer wg.Done()
		logger.Info("Console server starting (mTLS)",
			zap.String("address", consoleListener.Addr().String()),
			zap.Int("port", cfg.ConsolePort))

		// Signal server is about to start
		go func() {
			time.Sleep(100 * time.Millisecond) // Brief delay for server to initialize
			logger.Info("Console server ready for connections", zap.Int("port", cfg.ConsolePort))
			serverReady.Done()
		}()

		if err := consoleServer.Serve(consoleListener); err != nil {
			logger.Error("Console server failed", zap.Error(err))
		}
	}()

	// Start web server
	go func() {
		defer wg.Done()
		logger.Info("Web server starting (HTTP)",
			zap.Int("port", cfg.WebPort),
			zap.Bool("enabled", cfg.WebEnabled))

		// Signal server is about to start
		go func() {
			time.Sleep(100 * time.Millisecond) // Brief delay for server to initialize
			if cfg.WebEnabled {
				logger.Info("Web server ready for connections", zap.Int("port", cfg.WebPort))
			} else {
				logger.Info("Web server disabled")
			}
			serverReady.Done()
		}()

		if err := web.StartWebServer(cfg, nexusServer, logger); err != nil {
			if cfg.WebEnabled {
				logger.Error("Web server failed", zap.Error(err))
			}
		}
	}()

	// Wait for all three servers to be ready
	go func() {
		serverReady.Wait()
		logger.Info("🚀 NEXUS FULLY READY - All servers accepting connections",
			zap.Int("minion_port", cfg.MinionPort),
			zap.Int("console_port", cfg.ConsolePort),
			zap.Int("web_port", cfg.WebPort),
			zap.Bool("web_enabled", cfg.WebEnabled))
	}()

	// Set up graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down all servers...")

	// Gracefully stop all servers
	go func() {
		logger.Info("Stopping minion server...")
		minionServer.GracefulStop()
	}()

	go func() {
		logger.Info("Stopping console server...")
		consoleServer.GracefulStop()
	}()

	go func() {
		logger.Info("Stopping web server...")
		// Web server shutdown is handled by process termination
	}()

	// Wait for all servers to finish
	wg.Wait()
	logger.Info("All servers stopped")
}

// createMinionServer creates a gRPC server for minion connections with standard TLS
func createMinionServer(cfg *config.NexusConfig, serverCert tls.Certificate, logger *zap.Logger) *grpc.Server {
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}

	creds := credentials.NewTLS(tlsConfig)
	opts := []grpc.ServerOption{
		grpc.Creds(creds),
		grpc.MaxRecvMsgSize(cfg.MaxMsgSize),
		grpc.MaxSendMsgSize(cfg.MaxMsgSize),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     10 * time.Minute, // Reduced from 30 to 10 minutes
			MaxConnectionAge:      15 * time.Minute, // Reduced from 60 to 15 minutes
			MaxConnectionAgeGrace: 10 * time.Second,
			Time:                  60 * time.Second,
			Timeout:               20 * time.Second,
		}),
	}

	logger.Info("Minion server TLS credentials configured successfully")
	return grpc.NewServer(opts...)
}

// createConsoleServer creates a gRPC server for console connections with mTLS
func createConsoleServer(cfg *config.NexusConfig, serverCert tls.Certificate, caCertPool *x509.CertPool, logger *zap.Logger) *grpc.Server {
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caCertPool,
	}

	creds := credentials.NewTLS(tlsConfig)
	opts := []grpc.ServerOption{
		grpc.Creds(creds),
		grpc.MaxRecvMsgSize(cfg.MaxMsgSize),
		grpc.MaxSendMsgSize(cfg.MaxMsgSize),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     10 * time.Minute, // Reduced from 30 to 10 minutes
			MaxConnectionAge:      15 * time.Minute, // Reduced from 60 to 15 minutes
			MaxConnectionAgeGrace: 10 * time.Second,
			Time:                  60 * time.Second,
			Timeout:               20 * time.Second,
		}),
	}

	logger.Info("Console server mTLS credentials configured successfully")
	return grpc.NewServer(opts...)
}
