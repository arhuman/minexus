// Package nexus provides the core Nexus server implementation for the Minexus system.
// The Nexus server acts as the central coordinator that manages minion connections,
// command distribution, and result collection in a distributed command execution environment.
package nexus

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"minexus/internal/command"
	"minexus/internal/logging"
	pb "minexus/protogen"

	_ "github.com/lib/pq"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Server represents the core Nexus server that implements both MinionService and ConsoleService
// gRPC interfaces. It orchestrates operations between the database service and minion registry
// to provide distributed command execution capabilities for the Minexus system.
type Server struct {
	pb.UnimplementedMinionServiceServer
	pb.UnimplementedConsoleServiceServer

	logger          *zap.Logger
	dbService       DatabaseService
	minionRegistry  MinionRegistry
	pendingCommands map[string]*CommandTracker
	commandRegistry *command.Registry
}

// CommandTracker tracks the execution status and results of commands sent to minions.
// It maintains state information for distributed command execution across the system.
type CommandTracker struct {
	// Add fields as needed
}

// NewServer creates and initializes a new Nexus server instance with the specified
// database connection string and logger. It sets up the extracted services for
// database operations and minion registry management.
// Returns an error if database connection fails.
func NewServer(dbConnectionString string, logger *zap.Logger) (*Server, error) {

	logger, start := logging.FuncLogger(logger, "NewServer")
	defer logging.FuncExit(logger, start)

	var dbService DatabaseService

	// Initialize database connection if needed
	if dbConnectionString != "" {
		db, err := sql.Open("postgres", dbConnectionString)
		if err != nil {
			logger.Error("Failed to open database connection",
				zap.String("connection_string", dbConnectionString))
			return nil, err
		}

		// Test the database connection but don't fail server creation
		// This allows graceful degradation when database is unavailable
		if err := db.Ping(); err != nil {
			logger.Warn("Failed to ping database - database operations may fail",
				zap.String("connection_string", dbConnectionString))
			// Still set the database connection; individual operations will handle errors
		}

		dbService = NewDatabaseService(db, logger)
	}

	// Create minion registry with database service (may be nil)
	var dbServiceImpl *DatabaseServiceImpl
	if dbService != nil {
		dbServiceImpl = dbService.(*DatabaseServiceImpl)
	}
	minionRegistry := NewMinionRegistry(dbServiceImpl, logger)

	// Create the server instance with extracted services
	s := &Server{
		logger:          logger,
		dbService:       dbService,
		minionRegistry:  minionRegistry,
		pendingCommands: make(map[string]*CommandTracker),
		commandRegistry: command.SetupCommands(),
	}

	logger.Debug("Server created successfully")
	return s, nil
}

// Shutdown gracefully shuts down the Nexus server, closing database connections
// and cleaning up resources. This method should be called when the server is
// being terminated to ensure proper cleanup.
func (s *Server) Shutdown() {
	logger, start := logging.FuncLogger(s.logger, "Server.Shutdown")
	defer logging.FuncExit(logger, start)

	// Database cleanup is handled by the database service internally
	// No direct cleanup needed for the registry
	logger.Debug("Server shutdown completed")
}

// generateMinionID generates a unique ID for a minion.
func generateMinionID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// Register handles minion registration requests in the MinionService.
// When a minion connects to the Nexus, it calls this method to register itself
// and receive an assigned ID for future communications.
func (s *Server) Register(ctx context.Context, hostInfo *pb.HostInfo) (*pb.RegisterResponse, error) {
	logger, start := logging.FuncLogger(s.logger, "nexus.Server.Register")
	defer logging.FuncExit(logger, start)

	// Use provided ID if available, otherwise generate a new one
	var minionID string
	if hostInfo.Id != "" {
		minionID = hostInfo.Id
	} else {
		minionID = generateMinionID()
	}

	// Update the hostInfo with the final ID
	hostInfo.Id = minionID

	logger.Debug("Registering minion", zap.String("host_id", hostInfo.Id))

	// Register minion using the extracted registry
	resp, err := s.minionRegistry.Register(hostInfo)
	if err != nil {
		logger.Error("Failed to register minion",
			zap.String("host_id", hostInfo.Id))
		return nil, fmt.Errorf("failed to register minion: %v", err)
	}

	if !resp.Success {
		logger.Info("Registration unsuccessful",
			zap.String("host_id", hostInfo.Id),
			zap.String("error", resp.ErrorMessage))
	} else {
		logger.Info("Minion registered successfully",
			zap.String("host_id", hostInfo.Id))
	}

	return resp, nil
}

// GetMinionIDFromContext extracts the minion ID from gRPC metadata.
func GetMinionIDFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	values := md.Get("minion-id")
	if len(values) == 0 {
		return ""
	}

	return values[0]
}

// StreamCommands implements bidirectional streaming RPC for command exchange.
// This replaces the previous GetCommands, SendCommandResult, and UpdateCommandStatus methods
// with a single bidirectional stream for more efficient communication.
func (s *Server) StreamCommands(stream pb.MinionService_StreamCommandsServer) error {
	logger, start := logging.FuncLogger(s.logger, "nexus.Server.StreamCommands")
	defer logging.FuncExit(logger, start)

	minionID := GetMinionIDFromContext(stream.Context())
	if minionID == "" {
		logger.Error("Minion ID not provided")
		return status.Error(codes.Unauthenticated, "minion ID not provided")
	}

	// Get connection from registry
	minionRegistryImpl := s.minionRegistry.(*MinionRegistryImpl)
	conn, exists := minionRegistryImpl.GetConnectionImpl(minionID)
	if !exists {
		logger.Error("Minion not found", zap.String("minion_id", minionID))
		return status.Error(codes.NotFound, "minion not found")
	}

	logger.Debug("Minion connected to command stream", zap.String("minion_id", minionID))
	minionRegistryImpl.UpdateLastSeen(minionID)

	// Create error channel for coordinating goroutine termination
	errCh := make(chan error, 1)

	// Start goroutine to receive messages from minion
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}

			switch m := msg.Message.(type) {
			case *pb.CommandStreamMessage_Result:
				// Handle command result
				result := m.Result
				if s.dbService != nil {
					if err := s.dbService.StoreCommandResult(stream.Context(), result); err != nil {
						logger.Error("Failed to store command result",
							zap.String("command_id", result.CommandId),
							zap.String("minion_id", result.MinionId))
					}
				}

			case *pb.CommandStreamMessage_Status:
				// Handle status update
				status := m.Status
				if s.dbService != nil {
					if err := s.dbService.UpdateCommandStatus(stream.Context(), status.CommandId, status.Status); err != nil {
						logger.Error("Failed to update command status",
							zap.String("command_id", status.CommandId),
							zap.String("minion_id", status.MinionId))
					}
				}
			}
		}
	}()

	// Main loop for sending commands
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()

		case err := <-errCh:
			return err

		case cmd, ok := <-conn.CommandCh:
			if !ok {
				logger.Warn("Command channel closed", zap.String("minion_id", minionID))
				return nil
			}

			msg := &pb.CommandStreamMessage{
				Message: &pb.CommandStreamMessage_Command{
					Command: cmd,
				},
			}

			if err := stream.Send(msg); err != nil {
				logger.Error("Failed to send command",
					zap.String("minion_id", minionID),
					zap.String("command_id", cmd.Id))
				return err
			}

			logger.Debug("Command sent successfully",
				zap.String("minion_id", minionID),
				zap.String("command_id", cmd.Id))
		}
	}
}

// ListMinions returns a list of all registered minions in the ConsoleService.
// This method is used by administrative clients to get an overview of all
// available minions in the system.
func (s *Server) ListMinions(ctx context.Context, empty *pb.Empty) (*pb.MinionList, error) {
	logger, start := logging.FuncLogger(s.logger, "Nexus.ListMinions")
	defer logging.FuncExit(logger, start)

	minions := s.minionRegistry.ListMinions()
	logger.Debug("Listed minions", zap.Int("count", len(minions)))
	return &pb.MinionList{Minions: minions}, nil
}

// ListTags returns all available tags in the system in the ConsoleService.
// Tags are used for grouping and selecting minions for command execution.
func (s *Server) ListTags(ctx context.Context, empty *pb.Empty) (*pb.TagList, error) {
	logger, start := logging.FuncLogger(s.logger, "Nexus.ListTags")
	defer logging.FuncExit(logger, start)

	minionRegistryImpl := s.minionRegistry.(*MinionRegistryImpl)
	tags := minionRegistryImpl.ListTags()
	logger.Debug("Listed tags", zap.Int("count", len(tags)))
	return &pb.TagList{Tags: tags}, nil
}

// SetTags sets the complete tag set for a specific minion in the ConsoleService.
// This operation replaces all existing tags for the specified minion with the new set.
func (s *Server) SetTags(ctx context.Context, req *pb.SetTagsRequest) (*pb.Ack, error) {
	logger, start := logging.FuncLogger(s.logger, "Nexus.SetTags")
	defer logging.FuncExit(logger, start)

	logger.Debug("Setting tags",
		zap.String("minion_id", req.MinionId),
		zap.Int("tag_count", len(req.Tags)))

	if err := s.minionRegistry.SetTags(req.MinionId, req.Tags); err != nil {
		logger.Error("Failed to set tags",
			zap.String("minion_id", req.MinionId))
		return &pb.Ack{Success: false}, err
	}

	logger.Debug("Tags set successfully",
		zap.String("minion_id", req.MinionId))

	return &pb.Ack{Success: true}, nil
}

// UpdateTags performs incremental updates to a minion's tags in the ConsoleService.
// This method can add new tags or remove existing ones without affecting other tags.
func (s *Server) UpdateTags(ctx context.Context, req *pb.UpdateTagsRequest) (*pb.Ack, error) {
	logger, start := logging.FuncLogger(s.logger, "Nexus.UpdateTags")
	defer logging.FuncExit(logger, start)

	logger.Debug("Updating tags",
		zap.String("minion_id", req.MinionId),
		zap.Int("add_count", len(req.Add)),
		zap.Int("remove_count", len(req.RemoveKeys)))

	if err := s.minionRegistry.UpdateTags(req.MinionId, req.Add, req.RemoveKeys); err != nil {
		logger.Error("Failed to update tags",
			zap.String("minion_id", req.MinionId))
		return &pb.Ack{Success: false}, err
	}

	logger.Debug("Tags updated successfully",
		zap.String("minion_id", req.MinionId))

	return &pb.Ack{Success: true}, nil
}

// validateCommand checks if a command is valid
func (s *Server) validateCommand(cmd *pb.Command) error {
	logger, start := logging.FuncLogger(s.logger, "Nexus.validateCommand")
	defer logging.FuncExit(logger, start)

	if cmd == nil {
		logger.Error("Command is nil")
		return fmt.Errorf("command is nil")
	}

	if cmd.Payload == "" {
		logger.Error("Command payload is empty")
		return fmt.Errorf("command payload is empty")
	}

	// For system commands, check if they are registered
	if cmd.Type == pb.CommandType_SYSTEM {
		payload := strings.TrimSpace(cmd.Payload)

		// Check if it's a known command in the registry
		if strings.HasPrefix(payload, "system:") || strings.HasPrefix(payload, "file:") {
			// Extract the command name (everything before the first space or the whole string)
			cmdName := strings.Fields(payload)[0]
			if _, exists := s.commandRegistry.GetCommand(cmdName); !exists {
				logger.Error("Unknown command", zap.String("command", cmdName))
				return fmt.Errorf("unknown command: %s", cmdName)
			}
		}
		// For other system commands (shell commands), we allow them through
	}

	logger.Debug("Command validated successfully",
		zap.String("command_id", cmd.Id),
		zap.String("payload", cmd.Payload))

	return nil
}

// MatchesTags checks if a HostInfo matches the given TagSelector.
// This is a utility function used by tests and other components.
func MatchesTags(info *pb.HostInfo, selector *pb.TagSelector) bool {
	if selector == nil {
		return true
	}

	for _, rule := range selector.Rules {
		switch condition := rule.Condition.(type) {
		case *pb.TagMatch_Equals:
			if value, exists := info.Tags[rule.Key]; !exists || value != condition.Equals {
				return false
			}
		case *pb.TagMatch_Exists:
			if condition.Exists {
				if _, exists := info.Tags[rule.Key]; !exists {
					return false
				}
			}
		case *pb.TagMatch_NotExists:
			if condition.NotExists {
				if _, exists := info.Tags[rule.Key]; exists {
					return false
				}
			}
		}
	}

	return true
}

// Helper methods for testing

// FindTargetMinions delegates to the minion registry for testing compatibility
func (s *Server) FindTargetMinions(req *pb.CommandRequest) []string {
	return s.minionRegistry.FindTargetMinions(req)
}

// GetMinionRegistryImpl returns the registry implementation for testing
func (s *Server) GetMinionRegistryImpl() *MinionRegistryImpl {
	return s.minionRegistry.(*MinionRegistryImpl)
}

// SendCommand dispatches a command to one or more minions in the ConsoleService.
// Commands can be targeted to specific minions by ID or selected using tag selectors.
// Returns a response indicating whether the command was accepted for execution.
func (s *Server) SendCommand(ctx context.Context, req *pb.CommandRequest) (*pb.CommandDispatchResponse, error) {
	logger, start := logging.FuncLogger(s.logger, "Nexus.SendCommand")
	defer logging.FuncExit(logger, start)

	// Validate the command first
	if err := s.validateCommand(req.Command); err != nil {
		logger.Warn("Invalid command rejected",
			zap.String("payload", req.Command.Payload))
		return &pb.CommandDispatchResponse{
			Accepted:  false,
			CommandId: "",
		}, fmt.Errorf("invalid command: %v", err)
	}

	targets := s.minionRegistry.FindTargetMinions(req)
	if len(targets) == 0 {
		logger.Warn("No target minions found for command",
			zap.Strings("requested_minion_ids", req.MinionIds),
			zap.String("payload", req.Command.Payload))
		return &pb.CommandDispatchResponse{
			Accepted:  false,
			CommandId: "",
		}, nil
	}

	// Generate command ID
	commandID := generateMinionID()
	req.Command.Id = commandID

	logger.Debug("Command prepared for dispatch",
		zap.String("command_id", commandID),
		zap.Int("target_count", len(targets)))

	// Store command in database for each target minion using database service
	if s.dbService != nil {
		for _, minionID := range targets {
			if err := s.dbService.StoreCommand(ctx, commandID, minionID, req.Command.Payload); err != nil {
				logger.Warn("Failed to store command in database",
					zap.String("command_id", commandID),
					zap.String("minion_id", minionID))
			}
		}
	}

	// Send command to target minions using registry
	minionRegistryImpl := s.minionRegistry.(*MinionRegistryImpl)
	for _, minionID := range targets {
		if conn, exists := minionRegistryImpl.GetConnectionImpl(minionID); exists {
			// PHASE 3.2: Removed sequence number tracking - stream ordering handles this
			select {
			case conn.CommandCh <- req.Command:
				logger.Info("Command sent to minion channel",
					zap.String("command_id", commandID),
					zap.String("minion_id", minionID),
					zap.String("payload", req.Command.Payload))
			default:
				logger.Warn("Command channel full, skipping minion",
					zap.String("command_id", commandID),
					zap.String("minion_id", minionID),
					zap.String("payload", req.Command.Payload),
					zap.Int("channel_len", len(conn.CommandCh)),
					zap.Int("channel_cap", cap(conn.CommandCh)))
			}
		} else {
			logger.Warn("Minion not found when sending command",
				zap.String("command_id", commandID),
				zap.String("minion_id", minionID),
				zap.String("payload", req.Command.Payload))
		}
	}

	logger.Debug("Command dispatched successfully",
		zap.String("command_id", commandID),
		zap.Int("target_count", len(targets)))

	return &pb.CommandDispatchResponse{
		Accepted:  true,
		CommandId: commandID,
	}, nil
}

// GetCommandResults retrieves the execution results for a specific command in the ConsoleService.
// Administrative clients use this method to check the status and results of previously
// dispatched commands across all target minions.
func (s *Server) GetCommandResults(ctx context.Context, req *pb.ResultRequest) (*pb.CommandResults, error) {
	logger, start := logging.FuncLogger(s.logger, "Nexus.GetCommandResults")
	defer logging.FuncExit(logger, start)

	logger.Info("Getting command results",
		zap.String("command_id", req.CommandId))

	if s.dbService == nil {
		logger.Error("Database service is nil, cannot retrieve command results",
			zap.String("command_id", req.CommandId))
		return &pb.CommandResults{}, nil
	}

	results, err := s.dbService.GetCommandResults(ctx, req.CommandId)
	if err != nil {
		logger.Error("Error getting command results from database",
			zap.String("command_id", req.CommandId),
			zap.Error(err))
		return nil, err
	}

	logger.Debug("Retrieved command results",
		zap.String("command_id", req.CommandId),
		zap.Int("result_count", len(results)))

	return &pb.CommandResults{Results: results}, nil
}
