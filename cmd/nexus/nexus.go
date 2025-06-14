// Package main provides the Nexus server command-line interface.
// Nexus is a gRPC server that manages minions and console connections.
package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"minexus/internal/config"
	"minexus/internal/logging"
	"minexus/internal/nexus"
	"minexus/internal/version"
	pb "minexus/protogen"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

func main() {

	// Check for version flag
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("Nexus %s\n", version.Info())
		return
	}

	// Load configuration from environment, .env file, and command line flags
	cfg, err := config.LoadNexusConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Set up logging
	var logger *zap.Logger
	if cfg.Debug {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}
	defer logger.Sync()

	logger, start := logging.FuncLogger(logger, "main")
	defer logging.FuncExit(logger, start)

	// Display version information
	logger.Info("Starting Nexus", zap.String("version", version.Component("Nexus")))

	// Log the configuration (with sensitive data masked)
	cfg.LogConfig(logger)

	// Create nexus server
	server, err := nexus.NewServer(cfg.DBConnectionString(), logger)
	if err != nil {
		logger.Fatal("Failed to create server", zap.Error(err))
	}
	defer server.Shutdown()

	// Create a TCP listener
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		logger.Fatal("Failed to listen", zap.Error(err))
	}

	// Create a new gRPC server with options
	s := grpc.NewServer(
		grpc.MaxRecvMsgSize(cfg.MaxMsgSize),
		grpc.MaxSendMsgSize(cfg.MaxMsgSize),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second, // Increased minimum time between pings
			PermitWithoutStream: true,             // Allow pings even when there are no active streams
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     30 * time.Minute, // Increased idle timeout
			MaxConnectionAge:      60 * time.Minute, // Increased max age
			MaxConnectionAgeGrace: 10 * time.Second, // Increased grace period
			Time:                  60 * time.Second, // Matches client's ping interval
			Timeout:               20 * time.Second, // Matches client's timeout
		}),
	)

	// Register services
	pb.RegisterMinionServiceServer(s, server)
	pb.RegisterConsoleServiceServer(s, server)

	// Register reflection service for grpcurl and similar tools
	reflection.Register(s)

	// Start server in a goroutine
	go func() {
		logger.Info("Server listening", zap.String("address", lis.Addr().String()))
		if err := s.Serve(lis); err != nil {
			logger.Fatal("Failed to serve", zap.Error(err))
		}
	}()

	// Set up graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down gRPC server...")
	s.GracefulStop()
	logger.Info("Server stopped")
}
