package minion

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/arhuman/minexus/internal/command"
	"github.com/arhuman/minexus/internal/logging"
	pb "github.com/arhuman/minexus/protogen"

	"go.uber.org/zap"
	"google.golang.org/grpc/status"
)

// commandProcessor implements the CommandExecutor interface
type commandProcessor struct {
	id              string
	logger          *zap.Logger
	registry        *command.Registry
	atom            *zap.AtomicLevel
	commandSeqNums  map[string]string // Tracks command_id -> seq_num
	commandSeqMutex sync.RWMutex      // Protects the command sequence map
	service         pb.MinionServiceClient
	streamTimeout   time.Duration             // Configurable timeout for stream operations
	pendingResults  []*pb.CommandResult       // Buffer for results that couldn't be sent
	pendingStatuses []*pb.CommandStatusUpdate // Buffer for status updates that couldn't be sent
	pendingMutex    sync.RWMutex              // Protects pending buffers
}

// NewCommandProcessor creates a new command processor
func NewCommandProcessor(id string, registry *command.Registry, atom *zap.AtomicLevel, service pb.MinionServiceClient, streamTimeout time.Duration, logger *zap.Logger) *commandProcessor {
	logger, start := logging.FuncLogger(logger, "NewCommandProcessor")
	defer logging.FuncExit(logger, start)

	processor := &commandProcessor{
		id:              id,
		logger:          logger,
		registry:        registry,
		atom:            atom,
		commandSeqNums:  make(map[string]string),
		commandSeqMutex: sync.RWMutex{},
		service:         service,
		streamTimeout:   streamTimeout,
		pendingResults:  make([]*pb.CommandResult, 0),
		pendingStatuses: make([]*pb.CommandStatusUpdate, 0),
		pendingMutex:    sync.RWMutex{},
	}

	logger.Debug("Command processor created",
		zap.String("minion_id", id),
		zap.Duration("stream_timeout", streamTimeout))
	return processor
}

// Execute runs the specified command and returns the result
func (cp *commandProcessor) Execute(ctx context.Context, cmd *pb.Command) (*pb.CommandResult, error) {
	logger, start := logging.FuncLogger(cp.logger, "commandProcessor.Execute")
	defer logging.FuncExit(logger, start)

	// Extract sequence number for logging
	seqNum := "unknown"
	if cmd.Metadata != nil {
		if seq, ok := cmd.Metadata["seq_num"]; ok {
			seqNum = seq
		}
	}

	// Try registry-based execution first
	execCtx := command.NewExecutionContext(
		ctx,
		cp.logger,
		cp.atom,
		cp.id,
		cmd.Id,
	)

	logger.Debug("Attempting registry-based command execution",
		zap.String("command_id", cmd.Id),
		zap.String("payload", cmd.Payload),
		zap.String("seq_num", seqNum))

	result, err := cp.registry.Execute(execCtx, cmd)
	if err == nil {
		logger.Debug("Registry execution successful",
			zap.String("command_id", cmd.Id))
		return result, nil
	}

	// Command not found in registry - return error without fallback
	logger.Debug("Command not found in registry",
		zap.String("command_id", cmd.Id),
		zap.String("payload", cmd.Payload),
		zap.Error(err))

	// Store sequence number in our tracking map if available
	if cmd.Metadata != nil && cmd.Metadata["seq_num"] != "" {
		cp.commandSeqMutex.Lock()
		cp.commandSeqNums[cmd.Id] = cmd.Metadata["seq_num"]
		cp.commandSeqMutex.Unlock()
	}

	return &pb.CommandResult{
		CommandId: cmd.Id,
		MinionId:  cp.id,
		Timestamp: time.Now().Unix(),
		ExitCode:  1,
		Stderr:    fmt.Sprintf("Command not found: %s", cmd.Payload),
	}, fmt.Errorf("command not found: %s", cmd.Payload)
}

// CanHandle determines if this executor can handle the given command type
func (cp *commandProcessor) CanHandle(cmd *pb.Command) bool {
	logger, start := logging.FuncLogger(cp.logger, "commandProcessor.CanHandle")
	defer logging.FuncExit(logger, start)

	// This processor can handle all command types
	result := cmd != nil && cmd.Id != ""
	logger.Debug("Checking if processor can handle command",
		zap.Bool("can_handle", result),
		zap.String("command_id", func() string {
			if cmd != nil {
				return cmd.Id
			}
			return "nil"
		}()))
	return result
}

// ProcessCommands handles the main command processing loop using bidirectional streaming
func (cp *commandProcessor) ProcessCommands(ctx context.Context, stream pb.MinionService_StreamCommandsClient) error {
	logger, start := logging.FuncLogger(cp.logger, "commandProcessor.ProcessCommands")
	defer logging.FuncExit(logger, start)

	logger.Debug("Starting command listening loop")

	// Flush any pending results from previous stream disconnection
	if err := cp.flushPendingResults(stream); err != nil {
		logger.Warn("HARDENING: Failed to flush some pending results on stream reconnect",
			zap.Error(err))
		// Continue processing - don't fail on pending result flush errors
	}

	for {
		loopStart := time.Now()
		logger.Debug("Waiting for next command on stream")

		// Receive message from stream
		msg, err := cp.receiveMessageFromStream(ctx, stream, logger)
		if err != nil {
			return cp.handleStreamError(ctx, err, logger)
		}

		// Process the received message
		if err := cp.processReceivedMessage(ctx, msg, stream, logger, loopStart); err != nil {
			if err == errSkipMessage {
				continue
			}
			return err
		}
	}
}

// errSkipMessage is used to signal that a message should be skipped
var errSkipMessage = fmt.Errorf("skip message")

// recvResult represents the result of a stream receive operation
type recvResult struct {
	msg *pb.CommandStreamMessage
	err error
}

// receiveMessageFromStream receives a message from the stream with timeout and cancellation support
func (cp *commandProcessor) receiveMessageFromStream(ctx context.Context, stream pb.MinionService_StreamCommandsClient, logger *zap.Logger) (*pb.CommandStreamMessage, error) {
	recvCh := make(chan recvResult, 1)

	go func() {
		msg, err := cp.performStreamReceive(stream)
		recvCh <- recvResult{msg: msg, err: err}
	}()

	return cp.waitForStreamResult(ctx, recvCh, logger)
}

// performStreamReceive performs the actual stream receive operation
func (cp *commandProcessor) performStreamReceive(stream pb.MinionService_StreamCommandsClient) (*pb.CommandStreamMessage, error) {
	recvFuncName := "commandProcessor.streamReceiver"
	recvLogger, recvStart := logging.FuncLogger(cp.logger, recvFuncName)
	defer logging.FuncExit(recvLogger, recvStart)

	recvLogger.Debug("About to call stream.Recv()")
	msg, err := stream.Recv()

	cp.logStreamReceiveResult(recvLogger, msg, err)
	return msg, err
}

// logStreamReceiveResult logs the result of stream receive operation
func (cp *commandProcessor) logStreamReceiveResult(logger *zap.Logger, msg *pb.CommandStreamMessage, err error) {
	if err != nil {
		logger.Debug("stream.Recv() returned with error", zap.Error(err))
	} else if msg != nil && msg.GetCommand() != nil {
		cmd := msg.GetCommand()
		logger.Debug("Received command details",
			zap.String("command_id", cmd.Id),
			zap.String("payload", cmd.Payload),
			zap.String("type", cmd.Type.String()))
	}
}

// waitForStreamResult waits for stream result with timeout and cancellation support
func (cp *commandProcessor) waitForStreamResult(ctx context.Context, recvCh chan recvResult, logger *zap.Logger) (*pb.CommandStreamMessage, error) {
	select {
	case <-ctx.Done():
		logger.Debug("Context cancelled, stopping command loop")
		return nil, ctx.Err()
	case result := <-recvCh:
		return result.msg, result.err
	case <-time.After(cp.streamTimeout):
		return cp.handleStreamTimeout(ctx, logger)
	}
}

// handleStreamTimeout handles stream timeout scenarios
func (cp *commandProcessor) handleStreamTimeout(ctx context.Context, logger *zap.Logger) (*pb.CommandStreamMessage, error) {
	logger.Debug("stream.Recv() timeout, checking stream health",
		zap.Duration("timeout", cp.streamTimeout))

	select {
	case <-ctx.Done():
		logger.Debug("Context cancelled during health check")
		return nil, ctx.Err()
	default:
		logger.Debug("Stream timeout but context still active, continuing...")
		return nil, errSkipMessage
	}
}

// handleStreamError handles stream errors with appropriate logging and context checking
func (cp *commandProcessor) handleStreamError(ctx context.Context, err error, logger *zap.Logger) error {
	if err == errSkipMessage {
		return nil
	}

	// Buffer any pending results before stream disconnection
	cp.logPendingBufferState()

	// Enhanced error logging
	cp.logStreamError(err, logger)

	// Check context cancellation
	if ctx.Err() != nil {
		logger.Debug("Context cancelled, stopping command loop")
		return ctx.Err()
	}

	logger.Warn("HARDENING: Stream error will cause reconnection attempt",
		zap.String("minion_id", cp.id),
		zap.Error(err))
	return err
}

// logStreamError logs stream errors with appropriate detail level
func (cp *commandProcessor) logStreamError(err error, logger *zap.Logger) {
	if grpcErr, ok := err.(interface{ GRPCStatus() *status.Status }); ok {
		cp.logGRPCStreamError(grpcErr, err, logger)
	} else {
		cp.logNonGRPCStreamError(err, logger)
	}
}

// logGRPCStreamError logs gRPC-specific stream errors
func (cp *commandProcessor) logGRPCStreamError(grpcErr interface{ GRPCStatus() *status.Status }, err error, logger *zap.Logger) {
	grpcStatus := grpcErr.GRPCStatus()
	logger.Error("HARDENING: gRPC stream error - results may be buffered",
		zap.String("error_type", fmt.Sprintf("%T", err)),
		zap.String("grpc_code", grpcStatus.Code().String()),
		zap.String("grpc_message", grpcStatus.Message()),
		zap.Any("grpc_details", grpcStatus.Details()),
		zap.String("minion_id", cp.id),
		zap.Error(err))
}

// logNonGRPCStreamError logs non-gRPC stream errors
func (cp *commandProcessor) logNonGRPCStreamError(err error, logger *zap.Logger) {
	logger.Error("HARDENING: Non-gRPC stream error - results may be buffered",
		zap.String("error_type", fmt.Sprintf("%T", err)),
		zap.String("minion_id", cp.id),
		zap.Error(err))
}

// processReceivedMessage processes a received message from the stream
func (cp *commandProcessor) processReceivedMessage(ctx context.Context, msg *pb.CommandStreamMessage, stream pb.MinionService_StreamCommandsClient, logger *zap.Logger, loopStart time.Time) error {
	logger.Debug("Processing received message",
		zap.Any("message_type", fmt.Sprintf("%T", msg.Message)),
		zap.Bool("has_command", msg.GetCommand() != nil),
		zap.Bool("has_result", msg.GetResult() != nil),
		zap.Bool("has_status", msg.GetStatus() != nil))

	command := msg.GetCommand()
	if command == nil {
		logger.Warn("Received non-command message, skipping",
			zap.Any("message_type", fmt.Sprintf("%T", msg.Message)),
			zap.String("message_content", fmt.Sprintf("%+v", msg)))
		return errSkipMessage
	}

	// Extract and store sequence number
	seqNum := cp.extractAndStoreSequenceNumber(command)

	logger.Debug("Processing command",
		zap.String("command_id", command.Id),
		zap.String("payload", command.Payload),
		zap.String("command_type", command.Type.String()),
		zap.String("seq_num", seqNum))

	// Execute the command workflow
	return cp.executeCommandWorkflow(ctx, command, stream, logger, loopStart)
}

// extractAndStoreSequenceNumber extracts and stores the sequence number from command metadata
func (cp *commandProcessor) extractAndStoreSequenceNumber(command *pb.Command) string {
	seqNum := "unknown"
	if command.Metadata != nil {
		if seq, ok := command.Metadata["seq_num"]; ok {
			seqNum = seq
			cp.commandSeqMutex.Lock()
			cp.commandSeqNums[command.Id] = seq
			cp.commandSeqMutex.Unlock()
		}
	}
	return seqNum
}

// executeCommandWorkflow executes the complete command workflow
func (cp *commandProcessor) executeCommandWorkflow(ctx context.Context, command *pb.Command, stream pb.MinionService_StreamCommandsClient, logger *zap.Logger, loopStart time.Time) error {
	// Send status updates
	cp.sendStatusUpdates(stream, command.Id, logger)

	// Execute command
	result, err := cp.Execute(ctx, command)
	if err != nil {
		cp.handleCommandExecutionError(command.Id, err, result, logger)
	}

	// Send result and final status
	cp.sendCommandResultHelper(stream, result, logger)
	cp.sendFinalStatus(stream, command.Id, result, logger)

	logger.Debug("Command processing completed",
		zap.Duration("iteration_time", time.Since(loopStart)),
		zap.String("command_id", command.Id))

	return nil
}

// sendStatusUpdates sends the initial status updates for a command
func (cp *commandProcessor) sendStatusUpdates(stream pb.MinionService_StreamCommandsClient, commandID string, logger *zap.Logger) {
	if err := cp.sendStatusUpdateWithBuffer(stream, commandID, "RECEIVED"); err != nil {
		logger.Warn("HARDENING: Failed to send RECEIVED status - buffered for retry, continuing processing", zap.Error(err))
	}

	if err := cp.sendStatusUpdateWithBuffer(stream, commandID, "EXECUTING"); err != nil {
		logger.Warn("HARDENING: Failed to send EXECUTING status - buffered for retry, continuing processing", zap.Error(err))
	}
}

// handleCommandExecutionError handles errors from command execution
func (cp *commandProcessor) handleCommandExecutionError(commandID string, err error, result *pb.CommandResult, logger *zap.Logger) {
	logger.Error("Error executing command",
		zap.String("command_id", commandID),
		zap.Error(err))
	result.ExitCode = 1
	result.Stderr = err.Error()
}

// sendCommandResultHelper sends the command result through the stream
func (cp *commandProcessor) sendCommandResultHelper(stream pb.MinionService_StreamCommandsClient, result *pb.CommandResult, logger *zap.Logger) {
	if err := cp.sendCommandResultWithBuffer(stream, result); err != nil {
		logger.Warn("HARDENING: Failed to send command result - buffered for retry, continuing processing", zap.Error(err))
	}
}

// sendFinalStatus sends the final status update for a command
func (cp *commandProcessor) sendFinalStatus(stream pb.MinionService_StreamCommandsClient, commandID string, result *pb.CommandResult, logger *zap.Logger) {
	status := "COMPLETED"
	if result.ExitCode != 0 {
		status = "FAILED"
	}
	if err := cp.sendStatusUpdateWithBuffer(stream, commandID, status); err != nil {
		logger.Warn("HARDENING: Failed to send final status - buffered for retry, continuing processing", zap.Error(err))
	}
}

// sendStatusUpdate sends a status update through the stream
func (cp *commandProcessor) sendStatusUpdate(stream pb.MinionService_StreamCommandsClient, commandID, status string) error {
	update := &pb.CommandStatusUpdate{
		CommandId: commandID,
		MinionId:  cp.id,
		Status:    status,
		Timestamp: time.Now().Unix(),
	}

	msg := &pb.CommandStreamMessage{
		Message: &pb.CommandStreamMessage_Status{
			Status: update,
		},
	}

	return stream.Send(msg)
}

// sendCommandResult sends a command result through the stream
func (cp *commandProcessor) sendCommandResult(stream pb.MinionService_StreamCommandsClient, result *pb.CommandResult) error {
	msg := &pb.CommandStreamMessage{
		Message: &pb.CommandStreamMessage_Result{
			Result: result,
		},
	}

	return stream.Send(msg)
}

// flushPendingResults attempts to send all buffered results and statuses
func (cp *commandProcessor) flushPendingResults(stream pb.MinionService_StreamCommandsClient) error {
	cp.pendingMutex.Lock()
	defer cp.pendingMutex.Unlock()

	var flushErrors []string

	// Flush pending results
	for i, result := range cp.pendingResults {
		if err := cp.sendCommandResult(stream, result); err != nil {
			flushErrors = append(flushErrors, fmt.Sprintf("result %d: %v", i, err))
			continue
		}
		cp.logger.Info("HARDENING: Flushed pending result",
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId))
	}

	// Flush pending status updates
	for i, status := range cp.pendingStatuses {
		if err := cp.sendStatusUpdate(stream, status.CommandId, status.Status); err != nil {
			flushErrors = append(flushErrors, fmt.Sprintf("status %d: %v", i, err))
			continue
		}
		cp.logger.Debug("HARDENING: Flushed pending status",
			zap.String("command_id", status.CommandId),
			zap.String("status", status.Status))
	}

	// Clear successfully flushed items
	if len(flushErrors) == 0 {
		cp.pendingResults = make([]*pb.CommandResult, 0)
		cp.pendingStatuses = make([]*pb.CommandStatusUpdate, 0)
		cp.logger.Info("HARDENING: All pending results and statuses flushed successfully")
	} else {
		cp.logger.Warn("HARDENING: Some pending items failed to flush",
			zap.Strings("errors", flushErrors))
		return fmt.Errorf("failed to flush %d items: %s", len(flushErrors), strings.Join(flushErrors, "; "))
	}

	return nil
}

// logPendingBufferState logs the current state of pending buffers
func (cp *commandProcessor) logPendingBufferState() {
	cp.pendingMutex.RLock()
	defer cp.pendingMutex.RUnlock()

	cp.logger.Info("HARDENING: Current pending buffer state",
		zap.Int("pending_results", len(cp.pendingResults)),
		zap.Int("pending_statuses", len(cp.pendingStatuses)),
		zap.String("minion_id", cp.id))

	// Log details of pending items for debugging
	for i, result := range cp.pendingResults {
		cp.logger.Debug("HARDENING: Pending result details",
			zap.Int("index", i),
			zap.String("command_id", result.CommandId),
			zap.Int32("exit_code", result.ExitCode),
			zap.Int64("timestamp", result.Timestamp))
	}

	for i, status := range cp.pendingStatuses {
		cp.logger.Debug("HARDENING: Pending status details",
			zap.Int("index", i),
			zap.String("command_id", status.CommandId),
			zap.String("status", status.Status),
			zap.Int64("timestamp", status.Timestamp))
	}
}

// sendStatusUpdateWithBuffer sends a status update with buffering on failure
func (cp *commandProcessor) sendStatusUpdateWithBuffer(stream pb.MinionService_StreamCommandsClient, commandID, status string) error {
	update := &pb.CommandStatusUpdate{
		CommandId: commandID,
		MinionId:  cp.id,
		Status:    status,
		Timestamp: time.Now().Unix(),
	}

	// Try to send directly first
	if err := cp.sendStatusUpdate(stream, commandID, status); err != nil {
		// Buffer the status update for later retry
		cp.pendingMutex.Lock()
		cp.pendingStatuses = append(cp.pendingStatuses, update)
		cp.pendingMutex.Unlock()

		cp.logger.Warn("HARDENING: Status update failed, buffered for retry",
			zap.String("command_id", commandID),
			zap.String("status", status),
			zap.Error(err))
		return err
	}

	return nil
}

// sendCommandResultWithBuffer sends a command result with buffering on failure
func (cp *commandProcessor) sendCommandResultWithBuffer(stream pb.MinionService_StreamCommandsClient, result *pb.CommandResult) error {
	cp.logger.Info("DIAGNOSTIC: Attempting to send command result",
		zap.String("command_id", result.CommandId),
		zap.String("minion_id", result.MinionId),
		zap.Int32("exit_code", result.ExitCode))

	// Try to send directly first
	if err := cp.sendCommandResult(stream, result); err != nil {
		// Buffer the result for later retry
		cp.pendingMutex.Lock()
		cp.pendingResults = append(cp.pendingResults, result)
		cp.pendingMutex.Unlock()

		cp.logger.Error("HARDENING: Command result failed to send, buffered for retry",
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId),
			zap.Int32("exit_code", result.ExitCode),
			zap.Error(err))
		return err
	}

	cp.logger.Info("DIAGNOSTIC: Command result sent successfully",
		zap.String("command_id", result.CommandId),
		zap.String("minion_id", result.MinionId))
	return nil
}

// UpdateMinionID updates the minion ID used for command results
func (cp *commandProcessor) UpdateMinionID(newID string) {
	logger, start := logging.FuncLogger(cp.logger, "commandProcessor.UpdateMinionID")
	defer logging.FuncExit(logger, start)

	logger.Info("Updating minion ID",
		zap.String("old_id", cp.id),
		zap.String("new_id", newID))

	cp.id = newID
}
