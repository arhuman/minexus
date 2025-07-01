package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/arhuman/minexus/internal/certs"
	"github.com/arhuman/minexus/internal/config"
	pb "github.com/arhuman/minexus/protogen"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// GRPCClient handles all gRPC communication with the Nexus server
type GRPCClient struct {
	client pb.ConsoleServiceClient
	conn   *grpc.ClientConn
	logger *zap.Logger
}

// NewGRPCClient creates a new gRPC client instance
func NewGRPCClient(cfg *config.ConsoleConfig, logger *zap.Logger) (*GRPCClient, error) {
	// Connect to Nexus server
	logger.Info("Connecting to Nexus server", zap.String("address", cfg.ServerAddr))

	// Diagnostic: Check if we can resolve the hostname before attempting connection
	host, port, err := net.SplitHostPort(cfg.ServerAddr)
	if err != nil {
		logger.Error("Invalid server address format", zap.String("address", cfg.ServerAddr), zap.Error(err))
		return nil, fmt.Errorf("invalid server address format: %w", err)
	}

	logger.Debug("Testing hostname resolution", zap.String("hostname", host), zap.String("port", port))
	ips, err := net.LookupIP(host)
	if err != nil {
		logger.Error("Failed to resolve hostname - this is likely the source of the 'produced zero addresses' error",
			zap.String("hostname", host),
			zap.Error(err))
		logger.Info("Network diagnosis: hostname resolution failed",
			zap.String("suggestion", "If running console outside Docker, use 'localhost' instead of 'nexus_server'"))
	} else {
		logger.Info("Hostname resolved successfully",
			zap.String("hostname", host),
			zap.Int("ip_count", len(ips)))
		for i, ip := range ips {
			logger.Debug("Resolved IP", zap.Int("index", i), zap.String("ip", ip.String()))
		}
	}

	// Configure mTLS credentials for console client authentication
	logger.Info("Configuring mTLS for console client authentication")

	// Load console client certificate and private key
	clientCert, err := tls.X509KeyPair(certs.ConsoleClientCertPEM, certs.ConsoleClientKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load console client certificate: %w", err)
	}

	// Load CA certificate for server verification
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(certs.CAPem) {
		return nil, fmt.Errorf("failed to load CA certificate")
	}

	// Configure mTLS with client certificate authentication and CA verification
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   "nexus", // Must match server certificate CommonName
	}

	creds := credentials.NewTLS(tlsConfig)
	logger.Info("mTLS credentials configured for console client",
		zap.String("server_name", tlsConfig.ServerName))

	// Create connection using modern gRPC pattern with timeout
	conn, err := grpc.NewClient(cfg.ServerAddr,
		grpc.WithTransportCredentials(creds),
		grpc.WithConnectParams(grpc.ConnectParams{
			MinConnectTimeout: time.Duration(cfg.ConnectTimeout) * time.Second,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	logger.Info("Connected to Nexus server")

	// Create Console service client
	client := pb.NewConsoleServiceClient(conn)

	return &GRPCClient{
		client: client,
		conn:   conn,
		logger: logger,
	}, nil
}

// Close closes the gRPC connection
func (gc *GRPCClient) Close() error {
	if gc.conn != nil {
		return gc.conn.Close()
	}
	return nil
}

// ListMinions lists all connected minions
func (gc *GRPCClient) ListMinions(ctx context.Context) (*pb.MinionList, error) {
	return gc.client.ListMinions(ctx, &pb.Empty{})
}

// ListTags lists all available tags
func (gc *GRPCClient) ListTags(ctx context.Context) (*pb.TagList, error) {
	return gc.client.ListTags(ctx, &pb.Empty{})
}

// SendCommand sends a command to minions
func (gc *GRPCClient) SendCommand(ctx context.Context, req *pb.CommandRequest) (*pb.CommandDispatchResponse, error) {
	return gc.client.SendCommand(ctx, req)
}

// GetCommandResults gets command execution results
func (gc *GRPCClient) GetCommandResults(ctx context.Context, req *pb.ResultRequest) (*pb.CommandResults, error) {
	return gc.client.GetCommandResults(ctx, req)
}

// SetTags sets tags for a minion (replaces all existing tags)
func (gc *GRPCClient) SetTags(ctx context.Context, req *pb.SetTagsRequest) (*pb.Ack, error) {
	return gc.client.SetTags(ctx, req)
}

// UpdateTags updates tags for a minion (add/remove specific tags)
func (gc *GRPCClient) UpdateTags(ctx context.Context, req *pb.UpdateTagsRequest) (*pb.Ack, error) {
	return gc.client.UpdateTags(ctx, req)
}

// Helper functions for formatting display output

// FormatTags formats tags map for display
func FormatTags(tags map[string]string) string {
	if len(tags) == 0 {
		return "-"
	}

	var parts []string
	for k, v := range tags {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}

	result := strings.Join(parts, ", ")
	if len(result) > 30 {
		return result[:27] + "..."
	}
	return result
}

// FormatLastSeen formats Unix timestamp for display
func FormatLastSeen(timestamp int64) string {
	if timestamp == 0 {
		return "Never"
	}

	lastSeen := time.Unix(timestamp, 0)
	now := time.Now()

	duration := now.Sub(lastSeen)

	if duration < time.Minute {
		return "Just now"
	} else if duration < time.Hour {
		minutes := int(duration.Minutes())
		return fmt.Sprintf("%dm ago", minutes)
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		return fmt.Sprintf("%dh ago", hours)
	} else {
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}
