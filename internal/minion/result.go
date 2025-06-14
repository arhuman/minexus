package minion

import (
	"context"
	"fmt"
	"time"

	pb "minexus/protogen"

	"go.uber.org/zap"
)

// resultSender implements the ResultSender interface
type resultSender struct {
	service pb.MinionServiceClient
	logger  *zap.Logger
}

// NewResultSender creates a new result sender
func NewResultSender(service pb.MinionServiceClient, logger *zap.Logger) *resultSender {
	return &resultSender{
		service: service,
		logger:  logger,
	}
}

// Send transmits a command result to the nexus server
func (rs *resultSender) Send(ctx context.Context, result *pb.CommandResult) error {
	// Get sequence number from command ID - check if it's in our command tracking map
	// We'll assume the sequence number is unavailable at this point and rely on the
	// processor logs for tracking
	seqNum := "unknown"

	rs.logger.Info("DIAGNOSTIC: Preparing to send command result to nexus",
		zap.String("command_id", result.CommandId),
		zap.String("minion_id", result.MinionId),
		zap.Int32("exit_code", result.ExitCode),
		zap.String("stdout", result.Stdout),
		zap.String("stderr", result.Stderr),
		zap.Int64("timestamp", result.Timestamp),
		zap.String("seq_num", seqNum))

	// Validate the result data before sending
	if result.CommandId == "" {
		rs.logger.Error("DIAGNOSTIC: Cannot send result with empty command ID",
			zap.String("minion_id", result.MinionId))
		return fmt.Errorf("empty command ID in result")
	}

	if result.MinionId == "" {
		rs.logger.Error("DIAGNOSTIC: Cannot send result with empty minion ID",
			zap.String("command_id", result.CommandId))
		return fmt.Errorf("empty minion ID in result")
	}

	// Log the RPC client state
	if rs.service == nil {
		rs.logger.Error("DIAGNOSTIC: MinionServiceClient is nil")
		return fmt.Errorf("minion service client is nil")
	}

	// Perform the RPC call with a timeout context
	rs.logger.Info("DIAGNOSTIC: Executing RPC call SendCommandResult",
		zap.String("command_id", result.CommandId),
		zap.String("minion_id", result.MinionId))

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ack, err := rs.service.SendCommandResult(timeoutCtx, result)
	if err != nil {
		rs.logger.Error("DIAGNOSTIC: Error sending command result via RPC",
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId),
			zap.Error(err),
			zap.String("error_type", fmt.Sprintf("%T", err)))
		return err
	}

	rs.logger.Info("DIAGNOSTIC: Successfully sent command result via RPC",
		zap.String("command_id", result.CommandId),
		zap.String("minion_id", result.MinionId),
		zap.Int32("exit_code", result.ExitCode),
		zap.Bool("ack_success", ack.Success),
		zap.String("seq_num", seqNum))

	return nil
}
