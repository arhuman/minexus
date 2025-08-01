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
	"time"

	"github.com/arhuman/minexus/internal/command"
	"github.com/arhuman/minexus/internal/logging"
	pb "github.com/arhuman/minexus/protogen"

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

	// DIAGNOSIS: Log database connection attempt details
	logger.Info("DIAGNOSIS: Database service initialization",
		zap.String("connection_string_provided", fmt.Sprintf("%t", dbConnectionString != "")),
		zap.String("connection_string_length", fmt.Sprintf("%d", len(dbConnectionString))))

	// Initialize database connection if needed
	if dbConnectionString != "" {
		logger.Info("DIAGNOSIS: Attempting to create database connection",
			zap.String("connection_string", dbConnectionString))

		db, err := sql.Open("postgres", dbConnectionString)
		if err != nil {
			logger.Error("DIAGNOSIS: Failed to create database connection - database service will be nil",
				zap.String("connection_string", dbConnectionString),
				zap.Error(err))
			return nil, err
		}

		// Test the database connection but don't fail server creation
		// This allows graceful degradation when database is unavailable
		if err := db.Ping(); err != nil {
			logger.Warn("DIAGNOSIS: Database ping failed - operations will degrade gracefully",
				zap.String("connection_string", dbConnectionString),
				zap.Error(err))
			// Still set the database connection; individual operations will handle errors
		} else {
			logger.Info("DIAGNOSIS: Database connection successful")
		}

		dbService = NewDatabaseService(db, logger)
		logger.Info("DIAGNOSIS: Database service created successfully")
	} else {
		logger.Warn("DIAGNOSIS: No database connection string provided - database service will be nil")
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
		commandRegistry: command.SetupCommands(15 * time.Second), // Default timeout for nexus command registry
	}

	// DIAGNOSIS: Log final server state
	logger.Info("DIAGNOSIS: Server created with database service state",
		zap.Bool("database_service_available", dbService != nil))
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

	// Validate and extract minion ID
	minionID, err := s.validateAndExtractMinionID(stream, logger)
	if err != nil {
		return err
	}

	// Find minion connection with retry logic
	conn, err := s.findMinionConnectionWithRetry(minionID, logger, start)
	if err != nil {
		return err
	}

	// Setup connection and start message handling
	s.setupConnection(minionID, logger)
	errCh := s.startMessageReceiver(stream, logger)

	// Run main command dispatch loop
	return s.runCommandDispatchLoop(stream, conn, errCh, minionID, logger)
}

// validateAndExtractMinionID validates and extracts the minion ID from the stream context
func (s *Server) validateAndExtractMinionID(stream pb.MinionService_StreamCommandsServer, logger *zap.Logger) (string, error) {
	minionID := GetMinionIDFromContext(stream.Context())
	if minionID == "" {
		logger.Error("Minion ID not provided")
		return "", status.Error(codes.Unauthenticated, "minion ID not provided")
	}

	// RACE CONDITION DIAGNOSIS: Log concurrent StreamCommands attempts
	logger.Info("RACE CONDITION DIAGNOSIS: StreamCommands called",
		zap.String("minion_id", minionID),
		zap.String("stream_ptr", fmt.Sprintf("%p", stream)),
		zap.Time("timestamp", time.Now()))

	return minionID, nil
}

// findMinionConnectionWithRetry finds the minion connection using retry logic for race condition handling
func (s *Server) findMinionConnectionWithRetry(minionID string, logger *zap.Logger, start time.Time) (*MinionConnectionImpl, error) {
	minionRegistryImpl := s.minionRegistry.(*MinionRegistryImpl)

	// Log current registry state for diagnosis
	s.logRegistryState(minionRegistryImpl, minionID, logger)

	// Attempt to find connection with retry
	conn, exists := s.retryFindConnection(minionRegistryImpl, minionID, logger, start)
	if !exists {
		logger.Error("Minion not found after all retries",
			zap.String("minion_id", minionID),
			zap.Duration("total_retry_time", time.Since(start)))
		return nil, status.Error(codes.NotFound, "minion not found")
	}

	return conn, nil
}

// logRegistryState logs the current state of the minion registry for diagnosis
func (s *Server) logRegistryState(registry *MinionRegistryImpl, minionID string, logger *zap.Logger) {
	allMinions := registry.ListMinions()
	logger.Info("RACE CONDITION DIAGNOSIS: Registry state",
		zap.String("minion_id", minionID),
		zap.Int("total_minions", len(allMinions)),
		zap.Strings("minion_ids", s.extractMinionIDs(allMinions)))
}

// extractMinionIDs extracts minion IDs from a list of host info
func (s *Server) extractMinionIDs(hostInfos []*pb.HostInfo) []string {
	ids := make([]string, len(hostInfos))
	for i, h := range hostInfos {
		ids[i] = h.Id
	}
	return ids
}

// retryFindConnection attempts to find a minion connection with exponential backoff
func (s *Server) retryFindConnection(registry *MinionRegistryImpl, minionID string, logger *zap.Logger, start time.Time) (*MinionConnectionImpl, bool) {
	maxAttempts := 3
	baseDelay := 10 * time.Millisecond

	for attempt := 0; attempt < maxAttempts; attempt++ {
		conn, exists := registry.GetConnectionImpl(minionID)
		if exists {
			logger.Info("RACE CONDITION FIX: Connection found",
				zap.String("minion_id", minionID),
				zap.Int("attempt", attempt+1),
				zap.Duration("total_retry_time", time.Since(start)))
			return conn, true
		}

		if attempt < maxAttempts-1 {
			s.waitWithBackoff(attempt, baseDelay, minionID, logger, start)
		}
	}

	return nil, false
}

// waitWithBackoff waits with exponential backoff between retry attempts
func (s *Server) waitWithBackoff(attempt int, baseDelay time.Duration, minionID string, logger *zap.Logger, start time.Time) {
	backoffDelay := time.Duration((attempt*attempt + 1)) * baseDelay
	logger.Warn("Minion not found, retrying",
		zap.String("minion_id", minionID),
		zap.Int("attempt", attempt+1),
		zap.Duration("backoff_delay", backoffDelay),
		zap.Duration("elapsed_time", time.Since(start)))
	time.Sleep(backoffDelay)
}

// setupConnection sets up the connection for the minion
func (s *Server) setupConnection(minionID string, logger *zap.Logger) {
	logger.Debug("Minion connected to command stream", zap.String("minion_id", minionID))
	minionRegistryImpl := s.minionRegistry.(*MinionRegistryImpl)
	minionRegistryImpl.UpdateLastSeen(minionID)
}

// startMessageReceiver starts a goroutine to receive messages from the minion
func (s *Server) startMessageReceiver(stream pb.MinionService_StreamCommandsServer, logger *zap.Logger) chan error {
	errCh := make(chan error, 1)

	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}

			s.handleReceivedMessage(stream, msg, logger)
		}
	}()

	return errCh
}

// handleReceivedMessage handles different types of messages received from minions
func (s *Server) handleReceivedMessage(stream pb.MinionService_StreamCommandsServer, msg *pb.CommandStreamMessage, logger *zap.Logger) {
	switch m := msg.Message.(type) {
	case *pb.CommandStreamMessage_Result:
		s.handleCommandResult(stream, m.Result, logger)
	case *pb.CommandStreamMessage_Status:
		s.handleStatusUpdate(stream, m.Status, logger)
	}
}

// handleCommandResult handles command result messages
func (s *Server) handleCommandResult(stream pb.MinionService_StreamCommandsServer, result *pb.CommandResult, logger *zap.Logger) {
	logger.Info("COMMAND_FLOW_MONITORING: Command result received from minion",
		zap.String("stage", "RESULT_RECEIVED"),
		zap.String("command_id", result.CommandId),
		zap.String("minion_id", result.MinionId),
		zap.Int32("exit_code", result.ExitCode),
		zap.Time("timestamp", time.Now()))

	if s.dbService != nil {
		s.storeCommandResult(stream, result, logger)
	} else {
		s.logSkippedResultStorage(result, logger)
	}
}

// storeCommandResult stores the command result in the database
func (s *Server) storeCommandResult(stream pb.MinionService_StreamCommandsServer, result *pb.CommandResult, logger *zap.Logger) {
	if err := s.dbService.StoreCommandResult(stream.Context(), result); err != nil {
		logger.Error("COMMAND_FLOW_MONITORING: Result storage failed",
			zap.String("stage", "RESULT_STORAGE_FAILED"),
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId),
			zap.Error(err),
			zap.Time("timestamp", time.Now()))
	} else {
		logger.Info("COMMAND_FLOW_MONITORING: Result stored successfully",
			zap.String("stage", "RESULT_STORAGE_SUCCESS"),
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId),
			zap.Time("timestamp", time.Now()))
	}
}

// logSkippedResultStorage logs when result storage is skipped due to unavailable database
func (s *Server) logSkippedResultStorage(result *pb.CommandResult, logger *zap.Logger) {
	logger.Warn("COMMAND_FLOW_MONITORING: Database unavailable - result not persisted",
		zap.String("stage", "RESULT_STORAGE_SKIPPED"),
		zap.String("command_id", result.CommandId),
		zap.String("minion_id", result.MinionId),
		zap.Time("timestamp", time.Now()))
}

// handleStatusUpdate handles status update messages
func (s *Server) handleStatusUpdate(stream pb.MinionService_StreamCommandsServer, statusUpdate *pb.CommandStatusUpdate, logger *zap.Logger) {
	logger.Debug("COMMAND_FLOW_MONITORING: Status update received",
		zap.String("stage", "STATUS_UPDATE_RECEIVED"),
		zap.String("command_id", statusUpdate.CommandId),
		zap.String("minion_id", statusUpdate.MinionId),
		zap.String("status", statusUpdate.Status),
		zap.Time("timestamp", time.Now()))

	if s.dbService != nil {
		s.updateCommandStatus(stream, statusUpdate, logger)
	} else {
		s.logSkippedStatusUpdate(statusUpdate, logger)
	}
}

// updateCommandStatus updates the command status in the database
func (s *Server) updateCommandStatus(stream pb.MinionService_StreamCommandsServer, statusUpdate *pb.CommandStatusUpdate, logger *zap.Logger) {
	if err := s.dbService.UpdateCommandStatus(stream.Context(), statusUpdate.CommandId, statusUpdate.Status); err != nil {
		logger.Error("COMMAND_FLOW_MONITORING: Status update failed",
			zap.String("stage", "STATUS_UPDATE_FAILED"),
			zap.String("command_id", statusUpdate.CommandId),
			zap.String("minion_id", statusUpdate.MinionId),
			zap.String("status", statusUpdate.Status),
			zap.Error(err),
			zap.Time("timestamp", time.Now()))
	} else {
		logger.Debug("COMMAND_FLOW_MONITORING: Status updated successfully",
			zap.String("stage", "STATUS_UPDATE_SUCCESS"),
			zap.String("command_id", statusUpdate.CommandId),
			zap.String("status", statusUpdate.Status),
			zap.Time("timestamp", time.Now()))
	}
}

// logSkippedStatusUpdate logs when status update is skipped due to unavailable database
func (s *Server) logSkippedStatusUpdate(statusUpdate *pb.CommandStatusUpdate, logger *zap.Logger) {
	logger.Warn("COMMAND_FLOW_MONITORING: Database unavailable - status not updated",
		zap.String("stage", "STATUS_UPDATE_SKIPPED"),
		zap.String("command_id", statusUpdate.CommandId),
		zap.String("status", statusUpdate.Status),
		zap.Time("timestamp", time.Now()))
}

// runCommandDispatchLoop runs the main loop for dispatching commands to minions
func (s *Server) runCommandDispatchLoop(stream pb.MinionService_StreamCommandsServer, conn *MinionConnectionImpl, errCh chan error, minionID string, logger *zap.Logger) error {
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

			if err := s.sendCommandToMinion(stream, cmd, minionID, logger); err != nil {
				return err
			}
		}
	}
}

// sendCommandToMinion sends a command to the specified minion
func (s *Server) sendCommandToMinion(stream pb.MinionService_StreamCommandsServer, cmd *pb.Command, minionID string, logger *zap.Logger) error {
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
	return nil
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

	// DIAGNOSIS: Log all command validation attempts
	if cmd == nil {
		logger.Error("DIAGNOSIS: Command validation failed - command is nil")
		return fmt.Errorf("command is nil")
	}

	logger.Info("DIAGNOSIS: Validating command",
		zap.String("command_id", cmd.Id),
		zap.String("payload", cmd.Payload),
		zap.String("type", cmd.Type.String()))

	if cmd.Payload == "" {
		logger.Error("DIAGNOSIS: Command validation failed - payload is empty",
			zap.String("command_id", cmd.Id))
		return fmt.Errorf("command payload is empty")
	}

	// For system commands, check if they are registered
	if cmd.Type == pb.CommandType_SYSTEM {
		payload := strings.TrimSpace(cmd.Payload)

		// Check if it's a known command in the registry
		if strings.HasPrefix(payload, "system:") || strings.HasPrefix(payload, "file:") {
			// Extract the command name (everything before the first space or the whole string)
			cmdName := strings.Fields(payload)[0]
			logger.Info("DIAGNOSIS: Checking system command in registry",
				zap.String("command_name", cmdName),
				zap.String("full_payload", payload))

			if _, exists := s.commandRegistry.GetCommand(cmdName); !exists {
				logger.Error("DIAGNOSIS: Unknown command - not found in registry",
					zap.String("command", cmdName),
					zap.String("full_payload", payload))
				return fmt.Errorf("unknown command: %s", cmdName)
			} else {
				logger.Info("DIAGNOSIS: System command found in registry",
					zap.String("command_name", cmdName))
			}
		} else {
			logger.Info("DIAGNOSIS: Non-prefixed system command - allowing through",
				zap.String("payload", payload))
		}
		// For other system commands (shell commands), we allow them through
	}

	logger.Debug("DIAGNOSIS: Command validated successfully",
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

	logger.Info("COMMAND_FLOW_MONITORING: Command dispatch initiated",
		zap.String("stage", "DISPATCH_START"),
		zap.Strings("requested_minion_ids", req.MinionIds),
		zap.String("command_payload", req.Command.Payload),
		zap.String("command_type", req.Command.Type.String()),
		zap.Time("timestamp", time.Now()))

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
		logger.Warn("COMMAND_FLOW_MONITORING: No target minions found",
			zap.String("stage", "TARGET_RESOLUTION_FAILED"),
			zap.Strings("requested_minion_ids", req.MinionIds),
			zap.String("payload", req.Command.Payload),
			zap.Time("timestamp", time.Now()))
		return &pb.CommandDispatchResponse{
			Accepted:  false,
			CommandId: "",
		}, nil
	}

	// Generate command ID
	commandID := generateMinionID()
	req.Command.Id = commandID

	logger.Info("COMMAND_FLOW_MONITORING: Target minions resolved",
		zap.String("stage", "TARGET_RESOLUTION_SUCCESS"),
		zap.String("command_id", commandID),
		zap.Int("target_count", len(targets)),
		zap.Strings("target_minion_ids", targets),
		zap.Time("timestamp", time.Now()))

	// Store command in database for each target minion using database service
	var dbErrors []string
	if s.dbService != nil {
		for _, minionID := range targets {
			if err := s.dbService.StoreCommand(ctx, commandID, minionID, req.Command.Payload); err != nil {
				errMsg := fmt.Sprintf("minion %s: %v", minionID, err)
				dbErrors = append(dbErrors, errMsg)
				logger.Error("HARDENING: Failed to store command in database - persistence at risk",
					zap.String("command_id", commandID),
					zap.String("minion_id", minionID),
					zap.Error(err))
			} else {
				logger.Debug("HARDENING: Command stored successfully in database",
					zap.String("command_id", commandID),
					zap.String("minion_id", minionID))
			}
		}

		// Log database storage issues but don't fail dispatch
		if len(dbErrors) > 0 {
			logger.Warn("Some commands failed to persist - may cause result retrieval issues",
				zap.String("command_id", commandID),
				zap.Int("failed_storage_count", len(dbErrors)),
				zap.Strings("storage_errors", dbErrors))
		}
	} else {
		logger.Warn("HARDENING: Database service unavailable - commands not persisted",
			zap.String("command_id", commandID),
			zap.Int("target_count", len(targets)))
	}

	// Send command to target minions using registry
	minionRegistryImpl := s.minionRegistry.(*MinionRegistryImpl)
	var dispatchErrors []string
	successfulDispatches := 0

	for _, minionID := range targets {
		if conn, exists := minionRegistryImpl.GetConnectionImpl(minionID); exists {
			// Replace non-blocking select with timeout-based blocking
			// This prevents silent command dropping and ensures proper error handling
			timeout := 100 * time.Millisecond // Optimized: reduced from 1s to 100ms for faster dispatch
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			select {
			case conn.CommandCh <- req.Command:
				logger.Info("COMMAND_FLOW_MONITORING: Command delivered to channel",
					zap.String("stage", "CHANNEL_DELIVERY_SUCCESS"),
					zap.String("command_id", commandID),
					zap.String("minion_id", minionID),
					zap.String("payload", req.Command.Payload),
					zap.Int("channel_len", len(conn.CommandCh)),
					zap.Int("channel_cap", cap(conn.CommandCh)),
					zap.Time("timestamp", time.Now()))
				successfulDispatches++
			case <-ctx.Done():
				errMsg := fmt.Sprintf("Command dispatch timeout for minion %s: channel full or unresponsive", minionID)
				dispatchErrors = append(dispatchErrors, errMsg)
				logger.Error("COMMAND_FLOW_MONITORING: Channel delivery failed",
					zap.String("stage", "CHANNEL_DELIVERY_TIMEOUT"),
					zap.String("command_id", commandID),
					zap.String("minion_id", minionID),
					zap.String("payload", req.Command.Payload),
					zap.Int("channel_len", len(conn.CommandCh)),
					zap.Int("channel_cap", cap(conn.CommandCh)),
					zap.String("error", errMsg),
					zap.Time("timestamp", time.Now()))
			}
		} else {
			errMsg := fmt.Sprintf("Minion %s not found when dispatching command", minionID)
			dispatchErrors = append(dispatchErrors, errMsg)
			logger.Warn("COMMAND_FLOW_MONITORING: Minion connection not found",
				zap.String("stage", "CHANNEL_DELIVERY_NO_CONNECTION"),
				zap.String("command_id", commandID),
				zap.String("minion_id", minionID),
				zap.String("payload", req.Command.Payload),
				zap.String("error", errMsg),
				zap.Time("timestamp", time.Now()))
		}
	}

	// Commands are accepted if stored in database, regardless of channel delivery
	// Channel delivery failures (like full channels) should not cause command rejection
	if successfulDispatches == 0 {
		logger.Warn("COMMAND_FLOW_MONITORING: All channel deliveries failed",
			zap.String("stage", "DISPATCH_CHANNEL_FAILURES"),
			zap.String("command_id", commandID),
			zap.Int("target_count", len(targets)),
			zap.Strings("errors", dispatchErrors),
			zap.Time("timestamp", time.Now()))
	} else {
		// Log partial failures for monitoring
		if len(dispatchErrors) > 0 {
			logger.Warn("COMMAND_FLOW_MONITORING: Partial dispatch failure",
				zap.String("stage", "DISPATCH_PARTIAL_FAILURE"),
				zap.String("command_id", commandID),
				zap.Int("successful_dispatches", successfulDispatches),
				zap.Int("failed_dispatches", len(dispatchErrors)),
				zap.Strings("errors", dispatchErrors),
				zap.Time("timestamp", time.Now()))
		}
	}

	logger.Info("COMMAND_FLOW_MONITORING: Command dispatch completed",
		zap.String("stage", "DISPATCH_SUCCESS"),
		zap.String("command_id", commandID),
		zap.Int("target_count", len(targets)),
		zap.Int("successful_dispatches", successfulDispatches),
		zap.Duration("dispatch_duration", time.Since(start)),
		zap.Time("timestamp", time.Now()))

	// Commands are accepted if they passed validation and had targets, regardless of channel delivery status
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
