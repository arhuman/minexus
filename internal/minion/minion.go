package minion

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pb "github.com/arhuman/minexus/protogen"

	"go.uber.org/zap"

	"github.com/arhuman/minexus/internal/command"
	"github.com/arhuman/minexus/internal/logging"
)

// Minion represents a worker node that executes tasks
type Minion struct {
	id                string
	service           pb.MinionServiceClient
	done              chan struct{}
	wg                sync.WaitGroup
	heartbeatInterval time.Duration
	reconnectMgr      *ReconnectionManager
	logger            *zap.Logger
	Atom              zap.AtomicLevel
	registry          *command.Registry

	// New component interfaces
	connectionMgr    ConnectionManager
	commandProcessor CommandExecutor
	registrationMgr  RegistrationManager
}

// NewMinion creates a new minion instance
func NewMinion(id string, service pb.MinionServiceClient, heartbeatInterval time.Duration, initialReconnectDelay, maxReconnectDelay time.Duration, shellTimeout time.Duration, streamTimeout time.Duration, logger *zap.Logger, atom zap.AtomicLevel) *Minion {
	logger, start := logging.FuncLogger(logger, "NewMinion")
	defer logging.FuncExit(logger, start)

	reconnectMgr := NewReconnectionManager(initialReconnectDelay, maxReconnectDelay, logger)
	registry := command.SetupCommands(shellTimeout)

	// Create component instances
	connectionMgr := NewConnectionManager(id, service, reconnectMgr, logger)
	commandProcessor := NewCommandProcessor(id, registry, &atom, service, streamTimeout, logger)
	registrationMgr := NewRegistrationManager(id, service, connectionMgr, logger)

	return &Minion{
		id:                id,
		service:           service,
		done:              make(chan struct{}),
		heartbeatInterval: heartbeatInterval,
		reconnectMgr:      reconnectMgr,
		logger:            logger,
		Atom:              atom,
		registry:          registry,
		connectionMgr:     connectionMgr,
		commandProcessor:  commandProcessor,
		registrationMgr:   registrationMgr,
	}
}

// Start begins the minion's operation
func (m *Minion) Start(ctx context.Context) error {
	m.wg.Add(2) // One for command processing, one for periodic registration
	go m.run(ctx)
	go m.periodicRegistration(ctx)
	return nil
}

// Stop gracefully stops the minion
func (m *Minion) Stop() {
	close(m.done)
	m.wg.Wait()
}

// run is the main orchestration loop of the minion
func (m *Minion) run(ctx context.Context) {
	logger, start := logging.FuncLogger(m.logger, "Minion.run")
	defer logging.FuncExit(logger, start)
	defer m.wg.Done()

	// Step 1: Perform initial registration
	resp, err := m.performInitialRegistration(ctx)
	if err != nil {
		return
	}

	// Update ID if server assigned a new one
	m.handleIDUpdate(resp)

	// Step 2: Main command processing loop
	m.commandProcessingLoop(ctx)
}

// performInitialRegistration handles the initial registration with retries
func (m *Minion) performInitialRegistration(ctx context.Context) (*pb.RegisterResponse, error) {
	logger := m.logger.With(zap.String("method", "performInitialRegistration"))

	for attempt := 1; attempt <= 5; attempt++ {
		resp, err := m.registrationMgr.Register(ctx, nil)
		if err == nil && resp.Success {
			return resp, nil
		}

		if attempt < 5 {
			if !m.waitBetweenAttempts(ctx, attempt, err, logger) {
				return nil, ctx.Err()
			}
		} else {
			logger.Error("Failed to register minion after all retries", zap.Error(err))
			return nil, err
		}
	}
	return nil, fmt.Errorf("unexpected registration flow")
}

// waitBetweenAttempts handles the delay between registration attempts
func (m *Minion) waitBetweenAttempts(ctx context.Context, attempt int, err error, logger *zap.Logger) bool {
	delay := time.Duration(attempt) * time.Second
	logger.Warn("Initial registration failed, retrying...",
		zap.Error(err),
		zap.Int("attempt", attempt),
		zap.Duration("retry_delay", delay))

	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

// handleIDUpdate updates the minion ID if server assigned a new one
func (m *Minion) handleIDUpdate(resp *pb.RegisterResponse) {
	if resp.AssignedId != "" && resp.AssignedId != m.id {
		m.id = resp.AssignedId
		m.updateComponentsWithNewID(resp.AssignedId)
		m.logger.Info("Using server-assigned ID", zap.String("id", m.id))
	}
}

// commandProcessingLoop handles the main command processing loop
func (m *Minion) commandProcessingLoop(ctx context.Context) {
	logger := m.logger.With(zap.String("method", "commandProcessingLoop"))

	for {
		if m.shouldStop(ctx, logger) {
			return
		}

		if !m.ensureConnection(ctx) {
			continue
		}

		if !m.processCommandsFromStream(ctx, logger) {
			continue
		}
	}
}

// shouldStop checks if the minion should stop processing
func (m *Minion) shouldStop(ctx context.Context, logger *zap.Logger) bool {
	select {
	case <-ctx.Done():
		logger.Debug("Context cancelled, stopping command loop")
		return true
	case <-m.done:
		logger.Debug("Minion done signal received, stopping command loop")
		return true
	default:
		return false
	}
}

// ensureConnection ensures the connection is established
func (m *Minion) ensureConnection(ctx context.Context) bool {
	if m.connectionMgr.IsConnected() {
		return true
	}

	logger := m.logger.With(zap.String("method", "ensureConnection"))

	// Re-register and connect
	if !m.reregisterForConnection(ctx, logger) {
		return false
	}

	return m.establishConnection(ctx, logger)
}

// reregisterForConnection handles re-registration before connection
func (m *Minion) reregisterForConnection(ctx context.Context, logger *zap.Logger) bool {
	logger.Info("RACE CONDITION FIX: Connection not established, ensuring registration before connecting",
		zap.String("minion_id", m.id))

	resp, err := m.registrationMgr.Register(ctx, nil)
	if err != nil {
		logger.Error("Re-registration failed during reconnection",
			zap.String("minion_id", m.id),
			zap.Error(err))
		return m.waitBeforeRetry(ctx)
	}

	if !resp.Success {
		logger.Warn("RACE CONDITION FIX: Re-registration unsuccessful during reconnection",
			zap.String("minion_id", m.id),
			zap.String("error", resp.ErrorMessage))
		return m.waitBeforeRetry(ctx)
	}

	logger.Info("RACE CONDITION FIX: Re-registration successful, now attempting connection",
		zap.String("minion_id", m.id))
	return true
}

// establishConnection attempts to establish the connection
func (m *Minion) establishConnection(ctx context.Context, logger *zap.Logger) bool {
	if err := m.connectionMgr.Connect(ctx); err != nil {
		logger.Warn("RACE CONDITION FIX: Connect() failed after re-registration, calling HandleReconnection()",
			zap.String("minion_id", m.id),
			zap.Error(err),
			zap.String("error_type", fmt.Sprintf("%T", err)))

		if err := m.connectionMgr.HandleReconnection(ctx); err != nil {
			logger.Error("RACE CONDITION FIX: HandleReconnection() also failed",
				zap.String("minion_id", m.id),
				zap.Error(err))
			return ctx.Err() == nil
		}
	}
	return true
}

// processCommandsFromStream processes commands from the stream
func (m *Minion) processCommandsFromStream(ctx context.Context, logger *zap.Logger) bool {
	stream, err := m.connectionMgr.Stream()
	if err != nil {
		logger.Error("Failed to get stream", zap.Error(err))
		m.connectionMgr.Disconnect()
		return false
	}

	logger.Debug("Starting command processing loop", zap.String("minion_id", m.id))
	err = m.commandProcessor.(*commandProcessor).ProcessCommands(ctx, stream)

	return m.handleProcessingError(ctx, err, logger)
}

// handleProcessingError handles errors from command processing
func (m *Minion) handleProcessingError(ctx context.Context, err error, logger *zap.Logger) bool {
	if err != nil && ctx.Err() == nil {
		logger.Error("Command processing ended with error, will reconnect",
			zap.Error(err),
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.String("minion_id", m.id))
		m.connectionMgr.Disconnect()
		return m.waitBeforeRetry(ctx)
	} else if err != nil {
		logger.Debug("Command processing ended due to context cancellation",
			zap.Error(err),
			zap.String("minion_id", m.id))
	}
	return true
}

// waitBeforeRetry waits before retrying to avoid tight loops
func (m *Minion) waitBeforeRetry(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(time.Second):
		return false
	}
}

// updateComponentsWithNewID updates all components with the new minion ID
func (m *Minion) updateComponentsWithNewID(newID string) {
	m.connectionMgr.(*connectionManager).UpdateMinionID(newID)
	m.commandProcessor.(*commandProcessor).UpdateMinionID(newID)
	m.registrationMgr.(*registrationManager).UpdateMinionID(newID)
}

// periodicRegistration handles periodic registration with the nexus server
func (m *Minion) periodicRegistration(ctx context.Context) {
	logger, start := logging.FuncLogger(m.logger, "Minion.periodicRegistration")
	defer logging.FuncExit(logger, start)
	defer m.wg.Done()

	// Create a context that can be cancelled by the done channel
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start a goroutine to watch for the done signal
	go func() {
		select {
		case <-m.done:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Use the registration manager for periodic registration
	err := m.registrationMgr.PeriodicRegister(cancelCtx, m.heartbeatInterval)
	if err != nil && err != context.Canceled {
		logger.Error("Periodic registration ended with error", zap.Error(err))
	}
}

// executeCommand handles the execution of a single command
func (m *Minion) executeCommand(ctx context.Context, cmd *pb.Command) (*pb.CommandResult, error) {
	logger, start := logging.FuncLogger(m.logger, "Minion.executeCommand")
	defer logging.FuncExit(logger, start)

	return m.commandProcessor.Execute(ctx, cmd)
}

// ErrorInvalidCommand is returned when a command is not properly formatted
var ErrorInvalidCommand = errors.New("invalid command format")
