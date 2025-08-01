package minion

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/arhuman/minexus/protogen"

	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"

	"github.com/arhuman/minexus/internal/logging"
)

// connectionManager implements the ConnectionManager interface
type connectionManager struct {
	id           string
	service      pb.MinionServiceClient
	logger       *zap.Logger
	reconnectMgr *ReconnectionManager
	stream       pb.MinionService_StreamCommandsClient
	connected    bool
	connecting   bool
	stateMutex   sync.Mutex // protects connected, connecting, and stream fields
}

// NewConnectionManager creates a new connection manager
func NewConnectionManager(id string, service pb.MinionServiceClient, reconnectMgr *ReconnectionManager, logger *zap.Logger) *connectionManager {
	logger, start := logging.FuncLogger(logger, "NewConnectionManager")
	defer logging.FuncExit(logger, start)

	return &connectionManager{
		id:           id,
		service:      service,
		logger:       logger,
		reconnectMgr: reconnectMgr,
		connected:    false,
		connecting:   false,
	}
}

// Connect establishes a connection to the nexus server
func (cm *connectionManager) Connect(ctx context.Context) error {
	logger, start := logging.FuncLogger(cm.logger, "connectionManager.Connect")
	defer logging.FuncExit(logger, start)

	// RACE CONDITION DIAGNOSIS: Check for concurrent connection attempts
	cm.stateMutex.Lock()
	if cm.connecting {
		// Read values while holding the lock to avoid race conditions
		connected := cm.connected
		connecting := cm.connecting
		cm.stateMutex.Unlock()
		logger.Warn("RACE CONDITION DETECTED: Connect() called while already connecting",
			zap.String("minion_id", cm.id),
			zap.Bool("connected", connected),
			zap.Bool("connecting", connecting))
		return fmt.Errorf("connection attempt already in progress")
	}
	cm.connecting = true
	cm.stateMutex.Unlock()

	// Clean up connecting flag on exit
	defer func() {
		cm.stateMutex.Lock()
		cm.connecting = false
		cm.stateMutex.Unlock()
	}()

	logger.Debug("Attempting to get command stream",
		zap.String("minion_id", cm.id),
		zap.Bool("was_connected", cm.getConnectedState()))
	ctxWithMetadata := metadata.AppendToOutgoingContext(ctx, "minion-id", cm.id)

	// RACE CONDITION DIAGNOSIS: Log each StreamCommands call attempt
	logger.Info("RACE CONDITION DIAGNOSIS: About to call StreamCommands",
		zap.String("minion_id", cm.id),
		zap.Time("timestamp", time.Now()),
		zap.Bool("was_connected", cm.getConnectedState()))

	stream, err := cm.service.StreamCommands(ctxWithMetadata)
	if err != nil {
		logger.Error("Error getting command stream",
			zap.Error(err),
			zap.String("error_type", fmt.Sprintf("%T", err)))
		cm.stateMutex.Lock()
		cm.connected = false
		cm.stateMutex.Unlock()
		return err
	}

	cm.stateMutex.Lock()
	cm.stream = stream
	cm.connected = true
	cm.stateMutex.Unlock()
	logger.Info("Successfully obtained command stream",
		zap.String("minion_id", cm.id),
		zap.String("stream_ptr", fmt.Sprintf("%p", stream)))
	cm.reconnectMgr.ResetDelay() // Reset delay on successful connection
	return nil
}

// Disconnect closes the connection to the nexus server
func (cm *connectionManager) Disconnect() error {
	logger, start := logging.FuncLogger(cm.logger, "connectionManager.Disconnect")
	defer logging.FuncExit(logger, start)

	cm.stateMutex.Lock()
	defer cm.stateMutex.Unlock()

	if cm.stream != nil {
		logger.Info("Closing command stream",
			zap.String("minion_id", cm.id),
			zap.String("stream_ptr", fmt.Sprintf("%p", cm.stream)))
		err := cm.stream.CloseSend()
		cm.stream = nil
		cm.connected = false
		if err != nil {
			logger.Error("Error closing stream", zap.Error(err))
		} else {
			logger.Debug("Stream closed successfully")
		}
		return err
	}
	cm.connected = false
	logger.Debug("Disconnect called but no active stream")
	return nil
}

// IsConnected returns true if the minion is currently connected to the nexus server
func (cm *connectionManager) IsConnected() bool {
	cm.stateMutex.Lock()
	defer cm.stateMutex.Unlock()
	return cm.connected && cm.stream != nil
}

// Stream returns the active command stream client for receiving commands from the nexus
func (cm *connectionManager) Stream() (pb.MinionService_StreamCommandsClient, error) {
	if !cm.IsConnected() {
		return nil, fmt.Errorf("not connected to nexus server")
	}
	return cm.stream, nil
}

// HandleReconnection manages reconnection logic with exponential backoff
func (cm *connectionManager) HandleReconnection(ctx context.Context) error {
	logger, start := logging.FuncLogger(cm.logger, "connectionManager.HandleReconnection")
	defer logging.FuncExit(logger, start)

	// RACE CONDITION DIAGNOSIS: Check for concurrent connection attempts
	cm.stateMutex.Lock()
	if cm.connecting {
		// Read values while holding the lock to avoid race conditions
		connected := cm.connected
		connecting := cm.connecting
		cm.stateMutex.Unlock()
		logger.Warn("RACE CONDITION DETECTED: HandleReconnection() called while already connecting",
			zap.String("minion_id", cm.id),
			zap.Bool("connected", connected),
			zap.Bool("connecting", connecting))
		return fmt.Errorf("connection attempt already in progress")
	}
	cm.connecting = true
	cm.stateMutex.Unlock()

	// Clean up connecting flag on exit
	defer func() {
		cm.stateMutex.Lock()
		cm.connecting = false
		cm.stateMutex.Unlock()
	}()

	logger.Info("Stream connection lost, attempting to reconnect",
		zap.String("minion_id", cm.id),
		zap.Bool("was_connected", cm.getConnectedState()))

	// Check for cancellation before reconnecting
	select {
	case <-ctx.Done():
		logger.Debug("Context cancelled, stopping reconnection")
		return ctx.Err()
	default:
	}

	// Try to reconnect with exponential backoff
	delay := cm.reconnectMgr.GetNextDelay()
	logger.Info("Attempting reconnection after delay",
		zap.Duration("delay", delay),
		zap.String("minion_id", cm.id))
	time.Sleep(delay)

	ctxWithMetadata := metadata.AppendToOutgoingContext(ctx, "minion-id", cm.id)

	// RACE CONDITION DIAGNOSIS: Log reconnection StreamCommands call
	logger.Info("RACE CONDITION DIAGNOSIS: RECONNECTION - About to call StreamCommands",
		zap.String("minion_id", cm.id),
		zap.Time("timestamp", time.Now()),
		zap.Duration("delay_used", delay))

	stream, err := cm.service.StreamCommands(ctxWithMetadata)
	if err != nil {
		logger.Error("Error reconnecting to command stream",
			zap.Error(err),
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.String("minion_id", cm.id))
		cm.stateMutex.Lock()
		cm.stream = nil
		cm.connected = false
		cm.stateMutex.Unlock()
		return err
	}

	cm.stateMutex.Lock()
	cm.stream = stream
	cm.connected = true
	cm.stateMutex.Unlock()
	logger.Info("Successfully reconnected to command stream",
		zap.String("minion_id", cm.id),
		zap.String("new_stream_ptr", fmt.Sprintf("%p", stream)))
	cm.reconnectMgr.ResetDelay() // Reset delay on successful reconnection
	return nil
}

// UpdateMinionID updates the minion ID used for connections
func (cm *connectionManager) UpdateMinionID(newID string) {
	cm.id = newID
}

// getConnectedState safely returns the current connection state for logging
func (cm *connectionManager) getConnectedState() bool {
	cm.stateMutex.Lock()
	defer cm.stateMutex.Unlock()
	return cm.connected
}
