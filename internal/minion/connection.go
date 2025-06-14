package minion

import (
	"context"
	"fmt"
	"time"

	pb "minexus/protogen"

	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"
)

// connectionManager implements the ConnectionManager interface
type connectionManager struct {
	id           string
	service      pb.MinionServiceClient
	logger       *zap.Logger
	reconnectMgr *ReconnectionManager
	stream       pb.MinionService_GetCommandsClient
	connected    bool
}

// NewConnectionManager creates a new connection manager
func NewConnectionManager(id string, service pb.MinionServiceClient, reconnectMgr *ReconnectionManager, logger *zap.Logger) *connectionManager {
	return &connectionManager{
		id:           id,
		service:      service,
		logger:       logger,
		reconnectMgr: reconnectMgr,
		connected:    false,
	}
}

// Connect establishes a connection to the nexus server
func (cm *connectionManager) Connect(ctx context.Context) error {
	cm.logger.Debug("Attempting to get command stream", zap.String("minion_id", cm.id))
	ctxWithMetadata := metadata.AppendToOutgoingContext(ctx, "minion-id", cm.id)

	cm.logger.Debug("Calling GetCommands gRPC method")
	stream, err := cm.service.GetCommands(ctxWithMetadata, &pb.Empty{})
	if err != nil {
		cm.logger.Error("Error getting command stream", zap.Error(err))
		cm.connected = false
		return err
	}

	cm.stream = stream
	cm.connected = true
	cm.logger.Debug("Successfully obtained command stream")
	cm.reconnectMgr.ResetDelay() // Reset delay on successful connection
	return nil
}

// Disconnect closes the connection to the nexus server
func (cm *connectionManager) Disconnect() error {
	if cm.stream != nil {
		err := cm.stream.CloseSend()
		cm.stream = nil
		cm.connected = false
		return err
	}
	cm.connected = false
	return nil
}

// IsConnected returns true if the minion is currently connected to the nexus server
func (cm *connectionManager) IsConnected() bool {
	return cm.connected && cm.stream != nil
}

// Stream returns the active command stream client for receiving commands from the nexus
func (cm *connectionManager) Stream() (pb.MinionService_GetCommandsClient, error) {
	if !cm.IsConnected() {
		return nil, fmt.Errorf("not connected to nexus server")
	}
	return cm.stream, nil
}

// HandleReconnection manages reconnection logic with exponential backoff
func (cm *connectionManager) HandleReconnection(ctx context.Context) error {
	cm.logger.Debug("Stream is nil, attempting to reconnect")

	// Check for cancellation before reconnecting
	select {
	case <-ctx.Done():
		cm.logger.Debug("Context cancelled, stopping reconnection")
		return ctx.Err()
	default:
	}

	// Try to reconnect with exponential backoff
	delay := cm.reconnectMgr.GetNextDelay()
	cm.logger.Debug("Attempting reconnection after delay", zap.Duration("delay", delay))
	time.Sleep(delay)

	ctxWithMetadata := metadata.AppendToOutgoingContext(ctx, "minion-id", cm.id)
	stream, err := cm.service.GetCommands(ctxWithMetadata, &pb.Empty{})
	if err != nil {
		cm.logger.Error("Error reconnecting to command stream", zap.Error(err))
		cm.stream = nil
		cm.connected = false
		return err
	}

	cm.stream = stream
	cm.connected = true
	cm.logger.Debug("Successfully reconnected to command stream")
	cm.reconnectMgr.ResetDelay() // Reset delay on successful reconnection
	return nil
}

// UpdateMinionID updates the minion ID used for connections
func (cm *connectionManager) UpdateMinionID(newID string) {
	cm.id = newID
}
