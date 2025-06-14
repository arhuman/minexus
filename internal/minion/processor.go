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

// ProcessCommands handles the main command processing loop
func (cp *commandProcessor) ProcessCommands(ctx context.Context, stream pb.MinionService_GetCommandsClient, resultSender func(*pb.CommandResult) error) error {
	logger, start := logging.FuncLogger(cp.logger, "commandProcessor.ProcessCommands")
	defer logging.FuncExit(logger, start)

	logger.Debug("Starting command listening loop")

	for {
		loopStart := time.Now()
		logger.Debug("Waiting for next command on stream")

		// Use a goroutine to make stream.Recv() interruptible
		type recvResult struct {
			command *pb.Command
			err     error
		}

		recvCh := make(chan recvResult, 1)
		go func() {
			recvFuncName := "commandProcessor.streamReceiver"
			recvLogger, recvStart := logging.FuncLogger(cp.logger, recvFuncName)
			defer logging.FuncExit(recvLogger, recvStart)

			recvLogger.Debug("About to call stream.Recv()")
			cmd, err := stream.Recv()

			if err != nil {
				recvLogger.Debug("stream.Recv() returned with error",
					zap.Error(err))
			} else {
				recvLogger.Debug("stream.Recv() returned",
					zap.Bool("has_command", cmd != nil))
			}

			if cmd != nil {
				recvLogger.Debug("Received command details",
					zap.String("command_id", cmd.Id),
					zap.String("payload", cmd.Payload),
					zap.String("type", cmd.Type.String()))
			}
			recvCh <- recvResult{command: cmd, err: err}
		}()

		// Wait for command with timeout and cancellation support
		var command *pb.Command
		var err error

		select {
		case <-ctx.Done():
			logger.Debug("Context cancelled, stopping command loop")
			return ctx.Err()
		case result := <-recvCh:
			if result.command == nil && result.err == nil {
				logger.Debug("Received command from stream",
					zap.String("command_id", result.command.Id),
					zap.String("payload", result.command.Payload),
					zap.String("command_type", result.command.Type.String()))
			} else {
				logger.Debug("Received command with error")
			}
			command = result.command
			err = result.err
		case <-time.After(90 * time.Second):
			logger.Debug("stream.Recv() timeout after 90s, checking stream health")
			// Don't immediately disconnect - try a quick health check first
			select {
			case <-ctx.Done():
				logger.Debug("Context cancelled during health check")
				return ctx.Err()
			default:
				// Stream might still be healthy, just no commands pending
				logger.Debug("Stream timeout but context still active, continuing...")
				continue
			}
		}

		if err != nil {
			logger.Error("Error receiving command",
				zap.String("error_type", fmt.Sprintf("%T", err)))

			// Check if it's a context cancellation
			if ctx.Err() != nil {
				logger.Debug("Context cancelled, stopping command loop")
				return ctx.Err()
			}

			// Return error to trigger reconnection
			return err
		}

		// Extract sequence number from metadata if available and store in our tracking map
		seqNum := "unknown"
		if command.Metadata != nil {
			if seq, ok := command.Metadata["seq_num"]; ok {
				seqNum = seq
				// Store sequence number in our map for tracking
				cp.commandSeqMutex.Lock()
				cp.commandSeqNums[command.Id] = seq
				cp.commandSeqMutex.Unlock()
			}
		}

		logger.Debug("Received command",
			zap.String("command_id", command.Id),
			zap.String("payload", command.Payload),
			zap.String("command_type", command.Type.String()),
			zap.String("seq_num", seqNum))

		// Update command status through gRPC
		if err := cp.updateCommandStatus(ctx, command.Id, "RECEIVED"); err != nil {
			logger.Error("Failed to update command status to RECEIVED",
				zap.String("command_id", command.Id),
				zap.Error(err))
		}

		if err := cp.updateCommandStatus(ctx, command.Id, "EXECUTING"); err != nil {
			logger.Error("Failed to update command status to EXECUTING",
				zap.String("command_id", command.Id),
				zap.Error(err))
		}

		// Execute command
		result, err := cp.Execute(ctx, command)
		if err != nil {
			logger.Error("Error executing command",
				zap.String("command_id", command.Id),
				zap.String("payload", command.Payload),
				zap.Error(err))

			// Send failure result
			result.ExitCode = 1
			result.Stderr = err.Error()
		}

		// Retrieve the sequence number from our tracking map
		cp.commandSeqMutex.RLock()
		seqNum, ok := cp.commandSeqNums[command.Id]
		if !ok {
			seqNum = "unknown"
		}
		cp.commandSeqMutex.RUnlock()

		// Send command result using the provided sender function
		logger.Debug("About to send command result",
			zap.String("command_id", command.Id),
			zap.String("minion_id", cp.id),
			zap.Int32("exit_code", result.ExitCode),
			zap.String("stdout", result.Stdout),
			zap.String("seq_num", seqNum))

		if err := resultSender(result); err != nil {
			logger.Error("Error sending command result",
				zap.String("command_id", command.Id),
				zap.String("minion_id", cp.id))
		} else {
			logger.Info("Successfully sent command result using sender function",
				zap.String("command_id", command.Id),
				zap.String("minion_id", cp.id),
				zap.Int32("exit_code", result.ExitCode),
				zap.String("seq_num", seqNum))

			// Update final command status through gRPC
			status := "COMPLETED"
			if result.ExitCode != 0 {
				status = "FAILED"
			}
			if err := cp.updateCommandStatus(ctx, command.Id, status); err != nil {
				logger.Error("Failed to update final command status",
					zap.String("command_id", command.Id),
					zap.String("status", status),
					zap.Error(err))
			}
		}

		logger.Debug("Command processing loop iteration completed",
			zap.Duration("iteration_time", time.Since(loopStart)),
			zap.String("command_id", command.Id))
	}
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

// updateCommandStatus sends a command status update through gRPC
func (cp *commandProcessor) updateCommandStatus(ctx context.Context, commandID string, status string) error {
	update := &pb.CommandStatusUpdate{
		CommandId: commandID,
		MinionId:  cp.id,
		Status:    status,
		Timestamp: time.Now().Unix(),
	}

	_, err := cp.service.UpdateCommandStatus(ctx, update)
	return err
}
