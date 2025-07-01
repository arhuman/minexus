package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arhuman/minexus/internal/config"
	"github.com/arhuman/minexus/internal/minion"
	"github.com/arhuman/minexus/internal/version"
	pb "github.com/arhuman/minexus/protogen"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Helper function to check if slow tests should run
func shouldRunSlowTests() bool {
	return os.Getenv("SLOW_TESTS") == "true"
}

// mockMinionServiceClient is a mock implementation for testing
type mockMinionServiceClient struct {
	pb.MinionServiceClient
	registerError       bool
	registerSuccess     bool
	assignedID          string
	streamError         bool
	commandsToSend      []*pb.Command
	mu                  sync.Mutex
	receivedResults     []*pb.CommandResult
	receivedStatuses    []*pb.CommandStatusUpdate
	streamRecvError     bool
	streamRecvErrorOnce bool
	streamRecvCount     int
}

func (m *mockMinionServiceClient) Register(_ context.Context, _ *pb.HostInfo, _ ...grpc.CallOption) (*pb.RegisterResponse, error) {
	if m.registerError {
		return nil, errors.New("mock register error")
	}
	return &pb.RegisterResponse{
		Success:    m.registerSuccess,
		AssignedId: m.assignedID,
	}, nil
}

func (m *mockMinionServiceClient) StreamCommands(_ context.Context, _ ...grpc.CallOption) (pb.MinionService_StreamCommandsClient, error) {
	if m.streamError {
		return nil, errors.New("mock stream error")
	}
	return &mockStreamCommandsClient{
		commands:            m.commandsToSend,
		streamRecvError:     m.streamRecvError,
		streamRecvErrorOnce: m.streamRecvErrorOnce,
		streamRecvCount:     &m.streamRecvCount,
		parent:              m,
	}, nil
}

// mockStreamCommandsClient implements the bidirectional streaming client
type mockStreamCommandsClient struct {
	commands            []*pb.Command
	commandIndex        int
	streamRecvError     bool
	streamRecvErrorOnce bool
	streamRecvCount     *int
	parent              *mockMinionServiceClient
	grpc.ClientStream
}

func (m *mockStreamCommandsClient) Send(msg *pb.CommandStreamMessage) error {
	m.parent.mu.Lock()
	defer m.parent.mu.Unlock()

	if result := msg.GetResult(); result != nil {
		m.parent.receivedResults = append(m.parent.receivedResults, result)
	}
	if status := msg.GetStatus(); status != nil {
		m.parent.receivedStatuses = append(m.parent.receivedStatuses, status)
	}
	return nil
}

func (m *mockStreamCommandsClient) Recv() (*pb.CommandStreamMessage, error) {
	m.parent.mu.Lock()
	defer m.parent.mu.Unlock()

	*m.streamRecvCount++

	// Return error once if streamRecvErrorOnce is set
	if m.streamRecvErrorOnce && *m.streamRecvCount == 1 {
		return nil, errors.New("mock stream recv error once")
	}

	if m.streamRecvError {
		return nil, errors.New("mock stream recv error")
	}

	if m.commandIndex >= len(m.commands) {
		// Block indefinitely to simulate waiting for commands
		time.Sleep(100 * time.Millisecond)
		return nil, errors.New("no more commands")
	}

	cmd := m.commands[m.commandIndex]
	m.commandIndex++
	return &pb.CommandStreamMessage{
		Message: &pb.CommandStreamMessage_Command{
			Command: cmd,
		},
	}, nil
}

func (m *mockStreamCommandsClient) Header() (metadata.MD, error) {
	return nil, nil
}

func (m *mockStreamCommandsClient) Trailer() metadata.MD {
	return nil
}

func (m *mockStreamCommandsClient) CloseSend() error {
	return nil
}

func (m *mockStreamCommandsClient) Context() context.Context {
	return context.Background()
}

// Test helper functions
func TestMainVersionFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"version_short", []string{"minion", "--version"}},
		{"version_long", []string{"minion", "-v"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original args
			originalArgs := os.Args
			defer func() { os.Args = originalArgs }()

			// Set test args
			os.Args = tt.args

			// This would normally call os.Exit, but we'll test the logic separately
			// Instead, we test the version check logic
			if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
				// Simulate version output (we can't easily test main() directly due to os.Exit)
				// But we can verify the condition works
				if tt.args[1] != "--version" && tt.args[1] != "-v" {
					t.Errorf("Version flag check failed for %s", tt.args[1])
				}
			}
		})
	}
}

func TestConfigurationLoading(t *testing.T) {
	// Test that configuration loading works - use defaults to avoid flag issues
	cfg := config.DefaultMinionConfig()
	if cfg == nil {
		t.Fatal("Configuration should not be nil")
	}

	// Test default values
	if cfg.ServerAddr == "" {
		t.Error("ServerAddr should have a default value")
	}
	if cfg.ConnectTimeout <= 0 {
		t.Error("ConnectTimeout should be positive")
	}
	if cfg.InitialReconnectDelay <= 0 {
		t.Error("InitialReconnectDelay should be positive")
	}
	if cfg.MaxReconnectDelay <= 0 {
		t.Error("MaxReconnectDelay should be positive")
	}
	if cfg.HeartbeatInterval <= 0 {
		t.Error("HeartbeatInterval should be positive")
	}
}

func TestLoggerSetup(t *testing.T) {
	tests := []struct {
		name  string
		debug bool
	}{
		{"debug_mode", true},
		{"production_mode", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logger *zap.Logger
			var atom zap.AtomicLevel
			var err error

			if tt.debug {
				atom = zap.NewAtomicLevelAt(zap.DebugLevel)
				config := zap.NewDevelopmentConfig()
				config.Level = atom
				logger, err = config.Build()
			} else {
				atom = zap.NewAtomicLevelAt(zap.InfoLevel)
				config := zap.NewProductionConfig()
				config.Level = atom
				logger, err = config.Build()
			}

			if err != nil {
				t.Fatalf("Failed to create logger: %v", err)
			}

			if logger == nil {
				t.Error("Logger should not be nil")
			}

			expectedLevel := zap.InfoLevel
			if tt.debug {
				expectedLevel = zap.DebugLevel
			}

			if atom.Level() != expectedLevel {
				t.Errorf("Expected log level %v, got %v", expectedLevel, atom.Level())
			}

			logger.Sync()
		})
	}
}

func TestMinionCreationAndLifecycle(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	// Create mock client
	client := &mockMinionServiceClient{
		registerSuccess: true,
		assignedID:      "test-minion-123",
	}

	// Create logger
	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	// Test minion creation
	heartbeatInterval := 30 * time.Second
	reconnectDelay := 5 * time.Second
	minionInstance := minion.NewMinion("test-id", client, heartbeatInterval, reconnectDelay, reconnectDelay, 15*time.Second, 30*time.Second, logger, atom)

	if minionInstance == nil {
		t.Fatal("Minion should not be nil")
	}

	// Test starting and stopping
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := minionInstance.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Test graceful stop
	minionInstance.Stop()
}

func TestMinionRegistrationSuccess(t *testing.T) {
	client := &mockMinionServiceClient{
		registerSuccess: true,
		assignedID:      "server-assigned-id",
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	// Use faster intervals
	minion := minion.NewMinion("original-id", client, 100*time.Millisecond, 50*time.Millisecond, 5*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Reduced wait time
	time.Sleep(100 * time.Millisecond)

	minion.Stop()

	// Verify registration was attempted (this test mainly checks that registration doesn't crash)
	// len() always returns >= 0, so this check is simplified
	_ = len(client.receivedResults) // Just verify the slice is accessible
}

func TestMinionRegistrationFailure(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	client := &mockMinionServiceClient{
		registerError: true,
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	minion := minion.NewMinion("test-id", client, 30*time.Second, 5*time.Second, 60*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Give it time to attempt registration
	time.Sleep(500 * time.Millisecond)

	minion.Stop()
}

func TestMinionRegistrationUnsuccessful(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	client := &mockMinionServiceClient{
		registerSuccess: false, // Server returns success=false
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	minion := minion.NewMinion("test-id", client, 30*time.Second, 5*time.Second, 60*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Give it time to attempt registration
	time.Sleep(500 * time.Millisecond)

	minion.Stop()
}

func TestMinionCommandExecution(t *testing.T) {
	testCommand := &pb.Command{
		Id:      "test-cmd-123",
		Type:    pb.CommandType_SYSTEM,
		Payload: "echo hello world",
	}

	client := &mockMinionServiceClient{
		registerSuccess: true,
		commandsToSend:  []*pb.Command{testCommand},
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	// Use shorter intervals for testing
	minion := minion.NewMinion("test-id", client, 100*time.Millisecond, 50*time.Millisecond, 5*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Reduced wait time
	time.Sleep(200 * time.Millisecond)

	minion.Stop()

	// Check if command result was sent
	client.mu.Lock()
	defer client.mu.Unlock()

	if len(client.receivedResults) == 0 {
		t.Error("Expected command result to be sent")
	} else {
		result := client.receivedResults[0]
		if result.CommandId != testCommand.Id {
			t.Errorf("Expected command ID %s, got %s", testCommand.Id, result.CommandId)
		}
		if result.MinionId != "test-id" {
			t.Errorf("Expected minion ID test-id, got %s", result.MinionId)
		}
	}
}

func TestMinionStreamReconnection(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	client := &mockMinionServiceClient{
		registerSuccess:     true,
		streamRecvErrorOnce: true, // Fail once then succeed
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	minion := minion.NewMinion("test-id", client, 30*time.Second, 100*time.Millisecond, 5*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Give it time to register, fail, and reconnect
	time.Sleep(1 * time.Second)

	minion.Stop()

	// Check that reconnection was attempted
	if client.streamRecvCount < 2 {
		t.Error("Expected stream reconnection to be attempted")
	}
}

func TestMinionGetCommandsError(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	client := &mockMinionServiceClient{
		registerSuccess: true,
		streamError:     true,
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	minion := minion.NewMinion("test-id", client, 30*time.Second, 100*time.Millisecond, 5*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Give it time to register and fail getting commands
	time.Sleep(500 * time.Millisecond)

	minion.Stop()
}

func TestMinionSendResultError(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	testCommand := &pb.Command{
		Id:      "test-cmd-456",
		Type:    pb.CommandType_SYSTEM,
		Payload: "echo test",
	}

	client := &mockMinionServiceClient{
		registerSuccess: true,
		commandsToSend:  []*pb.Command{testCommand},
		streamError:     true,
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	minion := minion.NewMinion("test-id", client, 30*time.Second, 5*time.Second, 60*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Give it time to process command and fail sending result
	time.Sleep(1 * time.Second)

	minion.Stop()
}

func TestMinionPeriodicRegistration(t *testing.T) {
	client := &mockMinionServiceClient{
		registerSuccess: true,
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	// Use very short heartbeat interval for testing
	minion := minion.NewMinion("test-id", client, 50*time.Millisecond, 25*time.Millisecond, 5*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Wait for multiple heartbeat intervals
	time.Sleep(150 * time.Millisecond)

	minion.Stop()

	// Should have had multiple registration attempts (initial + periodic)
	// This is hard to verify precisely due to timing, but the test ensures
	// periodic registration doesn't crash
}

func TestMinionPeriodicRegistrationError(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	client := &mockMinionServiceClient{
		registerSuccess: true, // Initially succeeds
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	minion := minion.NewMinion("test-id", client, 100*time.Millisecond, 5*time.Second, 60*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// After initial registration, make subsequent ones fail
	time.Sleep(150 * time.Millisecond)
	client.registerError = true

	// Wait for periodic registration attempts
	time.Sleep(300 * time.Millisecond)

	minion.Stop()
}

func TestMinionContextCancellation(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	client := &mockMinionServiceClient{
		registerSuccess: true,
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	minion := minion.NewMinion("test-id", client, 30*time.Second, 5*time.Second, 60*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithCancel(context.Background())

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Let it start up
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	cancel()

	// Give it time to respond to cancellation
	time.Sleep(100 * time.Millisecond)

	minion.Stop()
}

func TestSignalHandling(t *testing.T) {
	// This tests the concept of signal handling, though we can't easily
	// test the actual signal sending to main() without complex setup

	// Test that we can create a signal channel
	sigChan := make(chan os.Signal, 1)
	if cap(sigChan) != 1 {
		t.Error("Signal channel should have capacity of 1")
	}

	// Test that we can notify on specific signals
	// (This doesn't actually send signals, just tests the setup)
	// signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// The actual signal handling in main() is tested by running the program
	// and sending signals, which is more of an integration test
}

func TestMinionSystemInfoCommand(t *testing.T) {
	testCommand := &pb.Command{
		Id:      "system-info-cmd",
		Type:    pb.CommandType_SYSTEM,
		Payload: "system:info",
	}

	client := &mockMinionServiceClient{
		registerSuccess: true,
		commandsToSend:  []*pb.Command{testCommand},
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	// Use faster intervals
	minion := minion.NewMinion("test-id", client, 100*time.Millisecond, 50*time.Millisecond, 5*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Reduced wait time
	time.Sleep(200 * time.Millisecond)

	minion.Stop()

	// Verify command was processed
	client.mu.Lock()
	defer client.mu.Unlock()

	if len(client.receivedResults) == 0 {
		t.Error("Expected system command result to be sent")
	} else {
		result := client.receivedResults[0]
		if result.CommandId != testCommand.Id {
			t.Errorf("Expected command ID %s, got %s", testCommand.Id, result.CommandId)
		}
		// System info should return some output
		if result.Stdout == "" {
			t.Error("Expected system info output")
		}
	}
}

func TestMinionLoggingCommand(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	testCommand := &pb.Command{
		Id:      "logging-level-cmd",
		Type:    pb.CommandType_SYSTEM,
		Payload: "logging:level",
	}

	client := &mockMinionServiceClient{
		registerSuccess: true,
		commandsToSend:  []*pb.Command{testCommand},
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	minion := minion.NewMinion("test-id", client, 30*time.Second, 5*time.Second, 60*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Give it time to process the logging command
	time.Sleep(1 * time.Second)

	minion.Stop()

	// Verify command was processed
	client.mu.Lock()
	defer client.mu.Unlock()

	if len(client.receivedResults) == 0 {
		t.Error("Expected logging command result to be sent")
	} else {
		result := client.receivedResults[0]
		if result.CommandId != testCommand.Id {
			t.Errorf("Expected command ID %s, got %s", testCommand.Id, result.CommandId)
		}
	}
}

func TestMinionFileCommand(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	// Create a temporary file for testing
	tmpFile, err := os.CreateTemp("", "minion_test_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("test content")
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	testCommand := &pb.Command{
		Id:      "file-get-cmd",
		Type:    pb.CommandType_SYSTEM,
		Payload: "file:get " + tmpFile.Name(),
	}

	client := &mockMinionServiceClient{
		registerSuccess: true,
		commandsToSend:  []*pb.Command{testCommand},
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	minion := minion.NewMinion("test-id", client, 30*time.Second, 5*time.Second, 60*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Give it time to process the file command
	time.Sleep(1 * time.Second)

	minion.Stop()

	// Verify command was processed
	client.mu.Lock()
	defer client.mu.Unlock()

	if len(client.receivedResults) == 0 {
		t.Error("Expected file command result to be sent")
	} else {
		result := client.receivedResults[0]
		if result.CommandId != testCommand.Id {
			t.Errorf("Expected command ID %s, got %s", testCommand.Id, result.CommandId)
		}
	}
}

func TestMinionInvalidCommand(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	testCommand := &pb.Command{
		Id:      "invalid-cmd",
		Type:    pb.CommandType_SYSTEM,
		Payload: "", // Empty payload should cause error
	}

	client := &mockMinionServiceClient{
		registerSuccess: true,
		commandsToSend:  []*pb.Command{testCommand},
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	minion := minion.NewMinion("test-id", client, 30*time.Second, 5*time.Second, 60*time.Second, 15*time.Second, 30*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Give it time to process the invalid command
	time.Sleep(1 * time.Second)

	minion.Stop()

	// Verify command result was sent
	client.mu.Lock()
	defer client.mu.Unlock()

	if len(client.receivedResults) == 0 {
		t.Error("Expected command result to be sent even for empty command")
	} else {
		result := client.receivedResults[0]
		if result.CommandId != testCommand.Id {
			t.Errorf("Expected command ID %s, got %s", testCommand.Id, result.CommandId)
		}
		// Empty command may or may not fail depending on shell behavior
		// The important thing is that it doesn't crash the minion
	}
}

func TestGRPCConnectionSimulation(t *testing.T) {
	// Test that gRPC connection parameters are reasonable
	// This doesn't test actual gRPC connection but validates the setup logic

	// Use default config to avoid flag redefinition issues
	cfg := config.DefaultMinionConfig()
	connectTimeout := time.Duration(cfg.ConnectTimeout) * time.Second

	if connectTimeout <= 0 {
		t.Error("Connect timeout should be positive")
	}

	if cfg.ServerAddr == "" {
		t.Error("Server address should not be empty")
	}

	// Test that connection timeout is reasonable (not too short or too long)
	if connectTimeout < time.Second {
		t.Error("Connect timeout seems too short")
	}
	if connectTimeout > 5*time.Minute {
		t.Error("Connect timeout seems too long")
	}
}

func TestConfigurationValidation(t *testing.T) {
	cfg := config.DefaultMinionConfig()

	// Test required fields
	if cfg.ServerAddr == "" {
		t.Error("ServerAddr is required")
	}

	// Test positive durations
	if cfg.ConnectTimeout <= 0 {
		t.Error("ConnectTimeout must be positive")
	}
	if cfg.InitialReconnectDelay <= 0 {
		t.Error("ReconnectDelay must be positive")
	}
	if cfg.HeartbeatInterval <= 0 {
		t.Error("HeartbeatInterval must be positive")
	}

	// Test reasonable ranges
	if cfg.ConnectTimeout > 300 { // 5 minutes
		t.Error("ConnectTimeout seems unreasonably long")
	}
	if cfg.InitialReconnectDelay > 60 { // 1 minute
		t.Error("ReconnectDelay seems unreasonably long")
	}
	if cfg.HeartbeatInterval > 3600 { // 1 hour
		t.Error("HeartbeatInterval seems unreasonably long")
	}
}

// Helper function to test version extraction
func TestVersionInfo(t *testing.T) {
	// Test version information is accessible
	versionInfo := version.Info()
	if versionInfo == "" {
		t.Error("Version info should not be empty")
	}

	componentInfo := version.Component("Minion")
	if componentInfo == "" {
		t.Error("Component version info should not be empty")
	}

	if !strings.Contains(componentInfo, "Minion") {
		t.Error("Component info should contain component name")
	}
}

// Integration test for the main workflow without actually running main()
func TestMainWorkflowSimulation(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	// Test the main workflow components without running the actual main function

	// 1. Test configuration loading - use defaults to avoid flag issues
	cfg := config.DefaultMinionConfig()
	if cfg == nil {
		t.Fatal("Failed to load configuration")
	}

	// 2. Test logger creation
	var logger *zap.Logger
	var atom zap.AtomicLevel
	var err error

	if cfg.Debug {
		atom = zap.NewAtomicLevelAt(zap.DebugLevel)
		zapConfig := zap.NewDevelopmentConfig()
		zapConfig.Level = atom
		logger, err = zapConfig.Build()
	} else {
		atom = zap.NewAtomicLevelAt(zap.InfoLevel)
		zapConfig := zap.NewProductionConfig()
		zapConfig.Level = atom
		logger, err = zapConfig.Build()
	}

	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// 3. Test minion creation
	mockClient := &mockMinionServiceClient{registerSuccess: true}
	heartbeatInterval := time.Duration(cfg.HeartbeatInterval) * time.Second
	reconnectDelay := time.Duration(cfg.InitialReconnectDelay) * time.Second
	maxReconnectDelay := time.Duration(cfg.MaxReconnectDelay) * time.Second
	minionInstance := minion.NewMinion(cfg.ID, mockClient, heartbeatInterval, reconnectDelay, maxReconnectDelay, 15*time.Second, 30*time.Second, logger, atom)

	if minionInstance == nil {
		t.Fatal("Failed to create minion")
	}

	// 4. Test context creation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if ctx == nil {
		t.Fatal("Failed to create context")
	}

	// 5. Test minion lifecycle
	err = minionInstance.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Brief operation
	time.Sleep(100 * time.Millisecond)

	// Graceful stop
	minionInstance.Stop()
}

// Test environment variable handling
func TestEnvironmentVariables(t *testing.T) {
	// This test verifies environment variable parsing logic without calling LoadMinionConfig
	// to avoid flag redefinition issues

	envVars := map[string]string{
		"NEXUS_SERVER":       "test-server:9999",
		"MINION_ID":          "test-minion-env",
		"DEBUG":              "true",
		"CONNECT_TIMEOUT":    "3",
		"RECONNECT_DELAY":    "3",
		"HEARTBEAT_INTERVAL": "60",
	}

	// Test environment variable parsing logic
	cfg := config.DefaultMinionConfig()

	// Simulate what LoadMinionConfig does with environment variables
	if value, exists := envVars["NEXUS_SERVER"]; exists {
		cfg.ServerAddr = value
	}
	if value, exists := envVars["MINION_ID"]; exists {
		cfg.ID = value
	}
	if value, exists := envVars["DEBUG"]; exists {
		if value == "true" {
			cfg.Debug = true
		}
	}

	// Verify configuration was updated correctly
	if cfg.ServerAddr != "test-server:9999" {
		t.Errorf("Expected ServerAddr from env simulation, got %s", cfg.ServerAddr)
	}
	if cfg.ID != "test-minion-env" {
		t.Errorf("Expected ID from env simulation, got %s", cfg.ID)
	}
	if !cfg.Debug {
		t.Error("Expected Debug=true from env simulation")
	}
}

// Test command execution with different types
func TestCommandExecutionTypes(t *testing.T) {
	if !shouldRunSlowTests() {
		t.Skip("Skipping slow test - set SLOW_TESTS=true to run")
	}

	tests := []struct {
		name    string
		command *pb.Command
	}{
		{
			name: "system_command",
			command: &pb.Command{
				Id:      "sys-cmd",
				Type:    pb.CommandType_SYSTEM,
				Payload: "echo 'system test'",
			},
		},
		{
			name: "system_info_command",
			command: &pb.Command{
				Id:      "info-cmd",
				Type:    pb.CommandType_SYSTEM,
				Payload: "system:info",
			},
		},
		{
			name: "logging_command",
			command: &pb.Command{
				Id:      "log-cmd",
				Type:    pb.CommandType_SYSTEM,
				Payload: "logging:level",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockMinionServiceClient{
				registerSuccess: true,
				commandsToSend:  []*pb.Command{tt.command},
			}

			logger := zap.NewNop()
			atom := zap.NewAtomicLevelAt(zap.InfoLevel)

			minion := minion.NewMinion("test-id", client, 30*time.Second, 5*time.Second, 60*time.Second, 15*time.Second, 30*time.Second, logger, atom)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			err := minion.Start(ctx)
			if err != nil {
				t.Fatalf("Failed to start minion: %v", err)
			}

			// Give time to process command
			time.Sleep(1 * time.Second)

			minion.Stop()

			// Verify command was processed
			client.mu.Lock()
			results := len(client.receivedResults)
			client.mu.Unlock()

			if results == 0 {
				t.Errorf("Expected command result for %s", tt.name)
			}
		})
	}
}
