package minion

import (
	"context"
	"fmt"
	"sync"
	"time"

	"minexus/internal/command"
	"minexus/internal/logging"
	pb "minexus/protogen"

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
}

// NewCommandProcessor creates a new command processor
func NewCommandProcessor(id string, registry *command.Registry, atom *zap.AtomicLevel, service pb.MinionServiceClient, logger *zap.Logger) *commandProcessor {
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
	}

	logger.Debug("Command processor created", zap.String("minion_id", id))
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

	// Try to execute as shell command if not found in registry
	logger.Debug("Command not found in registry, trying as shell command",
		zap.String("command_id", cmd.Id),
		zap.Error(err))

	// Get the shell command from registry and execute directly
	if shellCmd, exists := cp.registry.GetCommand("shell"); exists {
		result, err = shellCmd.Execute(execCtx, cmd.Payload)
		if err == nil {
			// Check if the shell command failed (non-zero exit code)
			if result.ExitCode != 0 {
				// Convert shell command failure to error for empty/invalid commands
				if cmd.Payload == "" || len(cmd.Payload) == 0 {
					logger.Warn("Empty command received",
						zap.String("command_id", cmd.Id))
					return result, fmt.Errorf("empty command")
				}
			}
			logger.Debug("Shell command execution successful",
				zap.String("command_id", cmd.Id))
			return result, nil
		}
	}

	// If shell command also fails, return the original error
	logger.Error("Both registry and shell command execution failed",
		zap.String("command_id", cmd.Id))

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
		Stderr:    fmt.Sprintf("Command not found and shell execution failed: %v", err),
	}, err
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

	for {
		loopStart := time.Now()
		logger.Debug("Waiting for next command on stream")

		// Use a goroutine to make stream.Recv() interruptible
		type recvResult struct {
			msg *pb.CommandStreamMessage
			err error
		}

		recvCh := make(chan recvResult, 1)
		go func() {
			recvFuncName := "commandProcessor.streamReceiver"
			recvLogger, recvStart := logging.FuncLogger(cp.logger, recvFuncName)
			defer logging.FuncExit(recvLogger, recvStart)

			recvLogger.Debug("About to call stream.Recv()")
			msg, err := stream.Recv()

			if err != nil {
				recvLogger.Debug("stream.Recv() returned with error", zap.Error(err))
			} else if msg != nil && msg.GetCommand() != nil {
				cmd := msg.GetCommand()
				recvLogger.Debug("Received command details",
					zap.String("command_id", cmd.Id),
					zap.String("payload", cmd.Payload),
					zap.String("type", cmd.Type.String()))
			}
			recvCh <- recvResult{msg: msg, err: err}
		}()

		// Wait for command with timeout and cancellation support
		var msg *pb.CommandStreamMessage
		var err error

		select {
		case <-ctx.Done():
			logger.Debug("Context cancelled, stopping command loop")
			return ctx.Err()
		case result := <-recvCh:
			msg = result.msg
			err = result.err
		case <-time.After(90 * time.Second):
			logger.Debug("stream.Recv() timeout after 90s, checking stream health")
			select {
			case <-ctx.Done():
				logger.Debug("Context cancelled during health check")
				return ctx.Err()
			default:
				logger.Debug("Stream timeout but context still active, continuing...")
				continue
			}
		}

		if err != nil {
			// Enhanced gRPC error logging for diagnosis
			if grpcErr, ok := err.(interface{ GRPCStatus() *status.Status }); ok {
				grpcStatus := grpcErr.GRPCStatus()
				logger.Error("RACE CONDITION TRACKING: gRPC stream error receiving command",
					zap.String("error_type", fmt.Sprintf("%T", err)),
					zap.String("grpc_code", grpcStatus.Code().String()),
					zap.String("grpc_message", grpcStatus.Message()),
					zap.Any("grpc_details", grpcStatus.Details()),
					zap.String("minion_id", cp.id),
					zap.Error(err))
			} else {
				logger.Error("RACE CONDITION TRACKING: Non-gRPC error receiving command",
					zap.String("error_type", fmt.Sprintf("%T", err)),
					zap.String("minion_id", cp.id),
					zap.Error(err))
			}

			if ctx.Err() != nil {
				logger.Debug("Context cancelled, stopping command loop")
				return ctx.Err()
			}
			logger.Warn("RACE CONDITION TRACKING: Stream error will cause reconnection attempt",
				zap.String("minion_id", cp.id),
				zap.Error(err))
			return err
		}

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
			continue
		}

		// Extract sequence number from metadata
		seqNum := "unknown"
		if command.Metadata != nil {
			if seq, ok := command.Metadata["seq_num"]; ok {
				seqNum = seq
				cp.commandSeqMutex.Lock()
				cp.commandSeqNums[command.Id] = seq
				cp.commandSeqMutex.Unlock()
			}
		}

		logger.Debug("Processing command",
			zap.String("command_id", command.Id),
			zap.String("payload", command.Payload),
			zap.String("command_type", command.Type.String()),
			zap.String("seq_num", seqNum))

		// Send status updates through stream
		if err := cp.sendStatusUpdate(stream, command.Id, "RECEIVED"); err != nil {
			logger.Error("Failed to send RECEIVED status", zap.Error(err))
			return err
		}

		if err := cp.sendStatusUpdate(stream, command.Id, "EXECUTING"); err != nil {
			logger.Error("Failed to send EXECUTING status", zap.Error(err))
			return err
		}

		// Execute command
		result, err := cp.Execute(ctx, command)
		if err != nil {
			logger.Error("Error executing command",
				zap.String("command_id", command.Id),
				zap.Error(err))
			result.ExitCode = 1
			result.Stderr = err.Error()
		}

		// Send command result through stream
		if err := cp.sendCommandResult(stream, result); err != nil {
			logger.Error("Failed to send command result", zap.Error(err))
			return err
		}

		// Send final status
		status := "COMPLETED"
		if result.ExitCode != 0 {
			status = "FAILED"
		}
		if err := cp.sendStatusUpdate(stream, command.Id, status); err != nil {
			logger.Error("Failed to send final status", zap.Error(err))
			return err
		}

		logger.Debug("Command processing completed",
			zap.Duration("iteration_time", time.Since(loopStart)),
			zap.String("command_id", command.Id))
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

// UpdateMinionID updates the minion ID used for command results
func (cp *commandProcessor) UpdateMinionID(newID string) {
	logger, start := logging.FuncLogger(cp.logger, "commandProcessor.UpdateMinionID")
	defer logging.FuncExit(logger, start)

	logger.Info("Updating minion ID",
		zap.String("old_id", cp.id),
		zap.String("new_id", newID))

	cp.id = newID
}
