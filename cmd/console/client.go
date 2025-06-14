package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"minexus/internal/config"
	pb "minexus/protogen"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

	connectTimeout := time.Duration(cfg.ConnectTimeout) * time.Second
	conn, err := grpc.Dial(cfg.ServerAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(connectTimeout),
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
