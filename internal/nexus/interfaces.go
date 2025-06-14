package nexus

import (
	"context"

	pb "minexus/protogen"
)

// MinionConnection represents the interface for a minion connection object.
// This abstraction allows the registry to work with minion connections without
// depending on the concrete implementation in the nexus package.
type MinionConnection interface {
	// GetInfo returns the host information for this minion connection.
	GetInfo() *pb.HostInfo
}

// MinionRegistry manages minion connections and tag operations.
// It provides methods to register minions, manage connections, and perform tag-based operations.
type MinionRegistry interface {
	// Register adds or updates a minion in the registry using host information.
	// Returns a RegisterResponse containing registration status and any conflict information.
	Register(hostInfo *pb.HostInfo) (*pb.RegisterResponse, error)

	// GetConnection retrieves the connection information for a specific minion.
	GetConnection(minionID string) (MinionConnection, bool)

	// ListMinions returns a list of all registered minions.
	ListMinions() []*pb.HostInfo

	// FindTargetMinions identifies minions that match the criteria in the command request.
	FindTargetMinions(req *pb.CommandRequest) []string

	// UpdateTags adds and removes tags for a specific minion.
	UpdateTags(minionID string, add map[string]string, remove []string) error

	// SetTags replaces all tags for a specific minion with the provided tags.
	SetTags(minionID string, tags map[string]string) error
}

// DatabaseService handles all database operations cleanly.
// It provides methods for persisting hosts, commands, and results.
type DatabaseService interface {
	// StoreHost persists host information to the database.
	StoreHost(ctx context.Context, hostInfo *pb.HostInfo) error

	// UpdateHost updates existing host information in the database.
	UpdateHost(ctx context.Context, hostInfo *pb.HostInfo) error

	// StoreCommand persists command information to the database.
	StoreCommand(ctx context.Context, commandID, minionID, payload string) error

	// UpdateCommandStatus updates the status of a command in the database.
	UpdateCommandStatus(ctx context.Context, commandID string, status string) error

	// StoreCommandResult persists command execution results to the database.
	StoreCommandResult(ctx context.Context, result *pb.CommandResult) error

	// GetCommandResults retrieves all results for a specific command.
	GetCommandResults(ctx context.Context, commandID string) ([]*pb.CommandResult, error)
}
