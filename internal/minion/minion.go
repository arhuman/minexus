package minion

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pb "minexus/protogen"

	"go.uber.org/zap"

	"minexus/internal/command"
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
	defer m.wg.Done()

	m.logger.Debug("Starting minion run() method")

	// Step 1: Perform initial registration with retries
	var resp *pb.RegisterResponse
	var err error
	for attempt := 1; attempt <= 5; attempt++ {
		resp, err = m.registrationMgr.Register(ctx, nil)
		if err == nil && resp.Success {
			break
		}

		if attempt < 5 {
			delay := time.Duration(attempt) * time.Second
			m.logger.Warn("Initial registration failed, retrying...",
				zap.Error(err),
				zap.Int("attempt", attempt),
				zap.Duration("retry_delay", delay))

			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
				continue
			}
		} else {
			m.logger.Error("Failed to register minion after all retries", zap.Error(err))
			return
		}
	}

	if !resp.Success {
		m.logger.Error("Registration unsuccessful after retries")
		return
	}

	// Update ID if server assigned a new one
	if resp.AssignedId != "" && resp.AssignedId != m.id {
		m.id = resp.AssignedId
		m.updateComponentsWithNewID(resp.AssignedId)
		m.logger.Info("Using server-assigned ID", zap.String("id", m.id))
	}

	// Step 2: Main command processing loop with reconnection handling
	for {
		select {
		case <-ctx.Done():
			m.logger.Debug("Context cancelled, stopping command loop")
			return
		case <-m.done:
			m.logger.Debug("Minion done signal received, stopping command loop")
			return
		default:
		}

		// Try to establish connection
		if !m.connectionMgr.IsConnected() {
			m.logger.Info("RACE CONDITION FIX: Connection not established, ensuring registration before connecting",
				zap.String("minion_id", m.id))

			// Re-register before attempting connection
			// This ensures the nexus knows about this minion before StreamCommands is called
			resp, err := m.registrationMgr.Register(ctx, nil)
			if err != nil {
				m.logger.Error("Re-registration failed during reconnection",
					zap.String("minion_id", m.id),
					zap.Error(err))
				// Wait before retrying to avoid tight loop
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
					continue
				}
			}

			if !resp.Success {
				m.logger.Warn("RACE CONDITION FIX: Re-registration unsuccessful during reconnection",
					zap.String("minion_id", m.id),
					zap.String("error", resp.ErrorMessage))
				// Wait before retrying
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
					continue
				}
			}

			m.logger.Info("RACE CONDITION FIX: Re-registration successful, now attempting connection",
				zap.String("minion_id", m.id))

			if err := m.connectionMgr.Connect(ctx); err != nil {
				m.logger.Warn("RACE CONDITION FIX: Connect() failed after re-registration, calling HandleReconnection()",
					zap.String("minion_id", m.id),
					zap.Error(err),
					zap.String("error_type", fmt.Sprintf("%T", err)))
				if err := m.connectionMgr.HandleReconnection(ctx); err != nil {
					m.logger.Error("RACE CONDITION FIX: HandleReconnection() also failed",
						zap.String("minion_id", m.id),
						zap.Error(err))
					if ctx.Err() != nil {
						return
					}
					continue
				}
			}
		}

		// Get stream and process commands
		stream, err := m.connectionMgr.Stream()
		if err != nil {
			m.logger.Error("Failed to get stream", zap.Error(err))
			m.connectionMgr.Disconnect() // Ensure clean state for retry
			continue
		}

		// Process commands until error or disconnection
		m.logger.Debug("Starting command processing loop",
			zap.String("minion_id", m.id))
		err = m.commandProcessor.(*commandProcessor).ProcessCommands(ctx, stream)

		if err != nil && ctx.Err() == nil {
			m.logger.Error("Command processing ended with error, will reconnect",
				zap.Error(err),
				zap.String("error_type", fmt.Sprintf("%T", err)),
				zap.String("minion_id", m.id))
			m.connectionMgr.Disconnect()

			// Add backoff delay to prevent tight reconnection loops
			// causing concurrent stream establishment attempts
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				// Continue with reconnection after delay
			}
		} else if err != nil {
			m.logger.Debug("Command processing ended due to context cancellation",
				zap.Error(err),
				zap.String("minion_id", m.id))
		}
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
		m.logger.Error("Periodic registration ended with error", zap.Error(err))
	}
}

// executeCommand handles the execution of a single command
func (m *Minion) executeCommand(ctx context.Context, cmd *pb.Command) (*pb.CommandResult, error) {
	return m.commandProcessor.Execute(ctx, cmd)
}

// ErrorInvalidCommand is returned when a command is not properly formatted
var ErrorInvalidCommand = errors.New("invalid command format")
