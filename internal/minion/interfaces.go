package minion

import (
	"context"
	"time"

	pb "github.com/arhuman/minexus/protogen"
)

// ConnectionManager handles stream management and connection state for minions.
// It provides methods to establish, maintain, and monitor gRPC connections to the nexus server.
type ConnectionManager interface {
	// Connect establishes a connection to the nexus server.
	Connect(ctx context.Context) error

	// Disconnect closes the connection to the nexus server.
	Disconnect() error

	// IsConnected returns true if the minion is currently connected to the nexus server.
	IsConnected() bool

	// Stream returns the active command stream client for receiving commands from the nexus.
	Stream() (pb.MinionService_StreamCommandsClient, error)

	// HandleReconnection manages reconnection logic with exponential backoff.
	HandleReconnection(ctx context.Context) error
}

// CommandExecutor handles the execution of commands received from the nexus server.
// It provides methods to execute commands and determine command compatibility.
type CommandExecutor interface {
	// Execute runs the specified command and returns the result.
	Execute(ctx context.Context, cmd *pb.Command) (*pb.CommandResult, error)

	// CanHandle determines if this executor can handle the given command type.
	CanHandle(cmd *pb.Command) bool
}

// RegistrationManager handles minion registration and heartbeat functionality.
// It provides methods to register with the nexus and maintain periodic registration.
type RegistrationManager interface {
	// Register performs initial registration with the nexus server using host information.
	Register(ctx context.Context, hostInfo *pb.HostInfo) (*pb.RegisterResponse, error)

	// PeriodicRegister performs periodic registration heartbeats at the specified interval.
	PeriodicRegister(ctx context.Context, interval time.Duration) error
}
