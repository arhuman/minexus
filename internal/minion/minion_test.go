package minion

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	pb "minexus/protogen"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Mock implementation of MinionServiceClient
type mockMinionServiceClient struct {
	registerFunc          func(ctx context.Context, in *pb.HostInfo, opts ...grpc.CallOption) (*pb.RegisterResponse, error)
	getCommandsFunc       func(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (pb.MinionService_GetCommandsClient, error)
	sendCommandResultFunc func(ctx context.Context, in *pb.CommandResult, opts ...grpc.CallOption) (*pb.Ack, error)
	updateStatusFunc      func(ctx context.Context, in *pb.CommandStatusUpdate, opts ...grpc.CallOption) (*pb.Ack, error)
}

func (m *mockMinionServiceClient) Register(ctx context.Context, in *pb.HostInfo, opts ...grpc.CallOption) (*pb.RegisterResponse, error) {
	if m.registerFunc != nil {
		return m.registerFunc(ctx, in, opts...)
	}
	return &pb.RegisterResponse{Success: true, AssignedId: in.Id}, nil
}

func (m *mockMinionServiceClient) GetCommands(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (pb.MinionService_GetCommandsClient, error) {
	if m.getCommandsFunc != nil {
		return m.getCommandsFunc(ctx, in, opts...)
	}
	return &mockGetCommandsClient{}, nil
}

func (m *mockMinionServiceClient) SendCommandResult(ctx context.Context, in *pb.CommandResult, opts ...grpc.CallOption) (*pb.Ack, error) {
	if m.sendCommandResultFunc != nil {
		return m.sendCommandResultFunc(ctx, in, opts...)
	}
	return &pb.Ack{Success: true}, nil
}

func (m *mockMinionServiceClient) UpdateCommandStatus(ctx context.Context, in *pb.CommandStatusUpdate, opts ...grpc.CallOption) (*pb.Ack, error) {
	if m.updateStatusFunc != nil {
		return m.updateStatusFunc(ctx, in, opts...)
	}
	return &pb.Ack{Success: true}, nil
}

// mockGetCommandsClientWithCommand implements pb.MinionService_GetCommandsClient with a single command
type mockGetCommandsClientWithCommand struct {
	mockGetCommandsClient
	command *pb.Command
	sent    bool
}

func (m *mockGetCommandsClientWithCommand) Recv() (*pb.Command, error) {
	if !m.sent {
		m.sent = true
		return m.command, nil
	}
	return nil, io.EOF
}

// Mock implementation of GetCommands stream client
type mockGetCommandsClient struct {
	commands []*pb.Command
	index    int
	closed   bool
}

func (m *mockGetCommandsClient) Recv() (*pb.Command, error) {
	if m.closed || m.index >= len(m.commands) {
		return nil, io.EOF
	}
	cmd := m.commands[m.index]
	m.index++
	return cmd, nil
}

func (m *mockGetCommandsClient) Header() (metadata.MD, error) {
	return metadata.MD{}, nil
}

func (m *mockGetCommandsClient) Trailer() metadata.MD {
	return metadata.MD{}
}

func (m *mockGetCommandsClient) CloseSend() error {
	m.closed = true
	return nil
}

func (m *mockGetCommandsClient) Context() context.Context {
	return context.Background()
}

func (m *mockGetCommandsClient) SendMsg(msg interface{}) error {
	return nil
}

func (m *mockGetCommandsClient) RecvMsg(msg interface{}) error {
	return nil
}

func TestNewMinion(t *testing.T) {
	mockClient := &mockMinionServiceClient{}
	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)
	minion := NewMinion("test-id", mockClient, 30*time.Second, 5*time.Second, 60*time.Second, logger, atom)

	if minion.id != "test-id" {
		t.Errorf("Expected minion ID to be 'test-id', got '%s'", minion.id)
	}

	if minion.heartbeatInterval != 30*time.Second {
		t.Errorf("Expected heartbeat interval to be 30s, got %v", minion.heartbeatInterval)
	}

	// Check that reconnection manager is initialized
	if minion.reconnectMgr == nil {
		t.Error("Expected reconnection manager to be initialized")
	}
}

func TestMinionRegistration(t *testing.T) {
	mockClient := &mockMinionServiceClient{
		registerFunc: func(ctx context.Context, in *pb.HostInfo, opts ...grpc.CallOption) (*pb.RegisterResponse, error) {
			// Accept any initial ID since it can be "test-minion" or "assigned-id"
			return &pb.RegisterResponse{Success: true, AssignedId: "assigned-id"}, nil
		},
		getCommandsFunc: func(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (pb.MinionService_GetCommandsClient, error) {
			// Check if metadata contains minion ID
			md, ok := metadata.FromOutgoingContext(ctx)
			if !ok {
				t.Error("Expected metadata in context")
			}
			minionIDs := md.Get("minion-id")
			if len(minionIDs) == 0 {
				t.Error("Expected minion-id in metadata")
			}
			// The minion ID should be either the original or the assigned one
			return &mockGetCommandsClient{closed: true}, nil
		},
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)
	minion := NewMinion("test-minion", mockClient, 100*time.Millisecond, 100*time.Millisecond, 5*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Give some time for registration to complete
	time.Sleep(200 * time.Millisecond)

	minion.Stop()

	// Check if minion ID was updated
	if minion.id != "assigned-id" {
		t.Errorf("Expected minion ID to be updated to 'assigned-id', got '%s'", minion.id)
	}
}

func TestMinionRegistrationFailure(t *testing.T) {
	mockClient := &mockMinionServiceClient{
		registerFunc: func(ctx context.Context, in *pb.HostInfo, opts ...grpc.CallOption) (*pb.RegisterResponse, error) {
			return nil, errors.New("registration failed")
		},
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)
	minion := NewMinion("test-minion", mockClient, 100*time.Millisecond, 100*time.Millisecond, 5*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Give some time for registration attempt
	time.Sleep(200 * time.Millisecond)

	minion.Stop()
}

func TestCommandExecution(t *testing.T) {
	testCases := []struct {
		name        string
		command     *pb.Command
		expectError bool
	}{
		{
			name: "System command success",
			command: &pb.Command{
				Id:      "cmd-1",
				Type:    pb.CommandType_SYSTEM,
				Payload: "echo hello",
			},
			expectError: false,
		},
		{
			name: "Internal command",
			command: &pb.Command{
				Id:      "cmd-2",
				Type:    pb.CommandType_INTERNAL,
				Payload: "echo world",
			},
			expectError: false,
		},
		{
			name: "Logging level command",
			command: &pb.Command{
				Id:      "cmd-3",
				Type:    pb.CommandType_INTERNAL,
				Payload: "logging:level",
			},
			expectError: false,
		},
		{
			name: "Logging increase command",
			command: &pb.Command{
				Id:      "cmd-4",
				Type:    pb.CommandType_INTERNAL,
				Payload: "logging:increase",
			},
			expectError: false,
		},
		{
			name: "Logging decrease command",
			command: &pb.Command{
				Id:      "cmd-5",
				Type:    pb.CommandType_INTERNAL,
				Payload: "logging:decrease",
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &mockMinionServiceClient{}
			logger := zap.NewNop()
			atom := zap.NewAtomicLevelAt(zap.InfoLevel)
			minion := NewMinion("test-minion", mockClient, time.Hour, time.Hour, time.Hour, logger, atom)

			result, err := minion.executeCommand(context.Background(), tc.command)

			if tc.expectError && err == nil {
				t.Error("Expected error but got none")
			} else if !tc.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if result != nil {
				if result.CommandId != tc.command.Id {
					t.Errorf("Expected command ID '%s', got '%s'", tc.command.Id, result.CommandId)
				}
				if result.MinionId != "test-minion" {
					t.Errorf("Expected minion ID 'test-minion', got '%s'", result.MinionId)
				}
			}
		})
	}
}

func TestCommandExecutionInvalidCommand(t *testing.T) {
	mockClient := &mockMinionServiceClient{}
	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)
	minion := NewMinion("test-minion", mockClient, time.Hour, time.Hour, time.Hour, logger, atom)

	command := &pb.Command{
		Id:      "cmd-invalid",
		Type:    pb.CommandType_INTERNAL,
		Payload: "", // Empty payload should cause error
	}

	result, err := minion.executeCommand(context.Background(), command)

	if err == nil {
		t.Error("Expected error for invalid command but got none")
	}

	if result == nil {
		t.Error("Expected result even on error")
	} else if result.ExitCode == 0 {
		t.Error("Expected non-zero exit code for failed command")
	}
}

func TestCommandReceiving(t *testing.T) {
	commands := []*pb.Command{
		{Id: "cmd-1", Type: pb.CommandType_SYSTEM, Payload: "echo test1"},
		{Id: "cmd-2", Type: pb.CommandType_SYSTEM, Payload: "echo test2"},
	}

	var receivedResults []*pb.CommandResult
	commandsSent := false

	mockClient := &mockMinionServiceClient{
		registerFunc: func(ctx context.Context, in *pb.HostInfo, opts ...grpc.CallOption) (*pb.RegisterResponse, error) {
			return &pb.RegisterResponse{Success: true, AssignedId: in.Id}, nil
		},
		getCommandsFunc: func(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (pb.MinionService_GetCommandsClient, error) {
			if !commandsSent {
				commandsSent = true
				return &mockGetCommandsClient{commands: commands}, nil
			}
			// Return a client that immediately closes to prevent infinite reconnection
			return &mockGetCommandsClient{closed: true}, nil
		},
		sendCommandResultFunc: func(ctx context.Context, in *pb.CommandResult, opts ...grpc.CallOption) (*pb.Ack, error) {
			receivedResults = append(receivedResults, in)
			return &pb.Ack{Success: true}, nil
		},
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)
	minion := NewMinion("test-minion", mockClient, 100*time.Millisecond, 50*time.Millisecond, 5*time.Second, logger, atom)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := minion.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start minion: %v", err)
	}

	// Give some time for commands to be processed
	time.Sleep(300 * time.Millisecond)

	minion.Stop()

	if len(receivedResults) != len(commands) {
		t.Errorf("Expected %d command results, got %d", len(commands), len(receivedResults))
	}

	for i, result := range receivedResults {
		if result.CommandId != commands[i].Id {
			t.Errorf("Expected command ID '%s', got '%s'", commands[i].Id, result.CommandId)
		}
	}
}

func TestLoggingCommands(t *testing.T) {
	mockClient := &mockMinionServiceClient{}
	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)
	minion := NewMinion("test-minion", mockClient, time.Hour, time.Hour, time.Hour, logger, atom)

	testCases := []struct {
		name            string
		command         *pb.Command
		expectError     bool
		expectedContain string
	}{
		{
			name: "Get logging level",
			command: &pb.Command{
				Id:      "logging-1",
				Type:    pb.CommandType_INTERNAL,
				Payload: "logging:level",
			},
			expectError:     false,
			expectedContain: "Current logging level: info",
		},
		{
			name: "Increase logging level",
			command: &pb.Command{
				Id:      "logging-2",
				Type:    pb.CommandType_INTERNAL,
				Payload: "logging:increase",
			},
			expectError:     false,
			expectedContain: "increased from info to debug",
		},
		{
			name: "Decrease logging level",
			command: &pb.Command{
				Id:      "logging-3",
				Type:    pb.CommandType_INTERNAL,
				Payload: "logging:decrease",
			},
			expectError:     false,
			expectedContain: "decreased from debug to info",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := minion.executeCommand(context.Background(), tc.command)

			if tc.expectError && err == nil {
				t.Error("Expected error but got none")
			} else if !tc.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if result != nil {
				if result.CommandId != tc.command.Id {
					t.Errorf("Expected command ID '%s', got '%s'", tc.command.Id, result.CommandId)
				}
				if result.MinionId != "test-minion" {
					t.Errorf("Expected minion ID 'test-minion', got '%s'", result.MinionId)
				}
				if tc.expectedContain != "" && !strings.Contains(result.Stdout, tc.expectedContain) {
					t.Errorf("Expected output to contain '%s', got '%s'", tc.expectedContain, result.Stdout)
				}
			}
		})
	}
}

func TestLoggingCommandsIntegration(t *testing.T) {
	mockClient := &mockMinionServiceClient{}
	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)
	minion := NewMinion("test-minion", mockClient, time.Hour, time.Hour, time.Hour, logger, atom)

	// Test full sequence: get level -> increase -> get level -> decrease -> get level

	// 1. Get initial level
	result, err := minion.executeCommand(context.Background(), &pb.Command{
		Id: "seq-1", Type: pb.CommandType_INTERNAL, Payload: "logging:level",
	})
	if err != nil {
		t.Fatalf("Failed to get initial level: %v", err)
	}
	if !strings.Contains(result.Stdout, "info") {
		t.Errorf("Expected initial level to be info, got: %s", result.Stdout)
	}

	// 2. Increase level (info -> debug)
	result, err = minion.executeCommand(context.Background(), &pb.Command{
		Id: "seq-2", Type: pb.CommandType_INTERNAL, Payload: "logging:increase",
	})
	if err != nil {
		t.Fatalf("Failed to increase level: %v", err)
	}
	if !strings.Contains(result.Stdout, "info to debug") {
		t.Errorf("Expected increase from info to debug, got: %s", result.Stdout)
	}

	// 3. Verify level is now debug
	result, err = minion.executeCommand(context.Background(), &pb.Command{
		Id: "seq-3", Type: pb.CommandType_INTERNAL, Payload: "logging:level",
	})
	if err != nil {
		t.Fatalf("Failed to get level after increase: %v", err)
	}
	if !strings.Contains(result.Stdout, "debug") {
		t.Errorf("Expected level to be debug after increase, got: %s", result.Stdout)
	}

	// 4. Decrease level (debug -> info)
	result, err = minion.executeCommand(context.Background(), &pb.Command{
		Id: "seq-4", Type: pb.CommandType_INTERNAL, Payload: "logging:decrease",
	})
	if err != nil {
		t.Fatalf("Failed to decrease level: %v", err)
	}
	if !strings.Contains(result.Stdout, "debug to info") {
		t.Errorf("Expected decrease from debug to info, got: %s", result.Stdout)
	}

	// 5. Verify level is back to info
	result, err = minion.executeCommand(context.Background(), &pb.Command{
		Id: "seq-5", Type: pb.CommandType_INTERNAL, Payload: "logging:level",
	})
	if err != nil {
		t.Fatalf("Failed to get final level: %v", err)
	}
	if !strings.Contains(result.Stdout, "info") {
		t.Errorf("Expected final level to be info, got: %s", result.Stdout)
	}
}

func TestGetHostname(t *testing.T) {
	hostname := getHostname()
	if hostname == "" {
		t.Error("Expected non-empty hostname")
	}
}

func TestGetIPAddress(t *testing.T) {
	ip := getIPAddress()
	if ip != "127.0.0.1" {
		t.Errorf("Expected IP '127.0.0.1', got '%s'", ip)
	}
}

func TestCommandStatusUpdates(t *testing.T) {
	testCases := []struct {
		name           string
		command        *pb.Command
		expectStatuses []string
		expectExitCode int32
	}{
		{
			name: "Successful command",
			command: &pb.Command{
				Id:      "test-cmd-1",
				Type:    pb.CommandType_SYSTEM,
				Payload: "echo test",
			},
			expectStatuses: []string{"RECEIVED", "EXECUTING", "COMPLETED"},
			expectExitCode: 0,
		},
		{
			name: "Failed command",
			command: &pb.Command{
				Id:      "test-cmd-2",
				Type:    pb.CommandType_SYSTEM,
				Payload: "nonexistentcommand",
			},
			expectStatuses: []string{"RECEIVED", "EXECUTING", "FAILED"},
			expectExitCode: 127,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var statusUpdates []*pb.CommandStatusUpdate
			var resultSent *pb.CommandResult

			mockClient := &mockMinionServiceClient{
				updateStatusFunc: func(ctx context.Context, in *pb.CommandStatusUpdate, opts ...grpc.CallOption) (*pb.Ack, error) {
					statusUpdates = append(statusUpdates, in)
					return &pb.Ack{Success: true}, nil
				},
				getCommandsFunc: func(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (pb.MinionService_GetCommandsClient, error) {
					return &mockGetCommandsClientWithCommand{command: tc.command}, nil
				},
			}

			logger := zap.NewNop()
			atom := zap.NewAtomicLevelAt(zap.InfoLevel)
			minion := NewMinion("test-minion", mockClient, time.Hour, time.Hour, time.Hour, logger, atom)

			// Start command processing
			processor := minion.commandProcessor.(*commandProcessor)
			stream, _ := mockClient.GetCommands(context.Background(), &pb.Empty{})

			err := processor.ProcessCommands(context.Background(), stream, func(result *pb.CommandResult) error {
				resultSent = result
				return nil
			})

			if err != nil && err != io.EOF {
				t.Errorf("Unexpected error: %v", err)
			}

			// Verify status updates
			if len(statusUpdates) != len(tc.expectStatuses) {
				t.Errorf("Expected %d status updates, got %d", len(tc.expectStatuses), len(statusUpdates))
			} else {
				for i, expectedStatus := range tc.expectStatuses {
					if statusUpdates[i].Status != expectedStatus {
						t.Errorf("Expected status %s at position %d, got %s",
							expectedStatus, i, statusUpdates[i].Status)
					}
					if statusUpdates[i].CommandId != tc.command.Id {
						t.Errorf("Expected command ID %s, got %s",
							tc.command.Id, statusUpdates[i].CommandId)
					}
					if statusUpdates[i].MinionId != "test-minion" {
						t.Errorf("Expected minion ID test-minion, got %s",
							statusUpdates[i].MinionId)
					}
				}
			}

			// Verify command result
			if resultSent == nil {
				t.Error("Expected command result but got none")
			} else {
				if resultSent.CommandId != tc.command.Id {
					t.Errorf("Expected result command ID %s, got %s",
						tc.command.Id, resultSent.CommandId)
				}
				if resultSent.ExitCode != tc.expectExitCode {
					t.Errorf("Expected exit code %d, got %d",
						tc.expectExitCode, resultSent.ExitCode)
				}
			}

			// Verify timestamps
			startTime := statusUpdates[0].Timestamp
			for i := 1; i < len(statusUpdates); i++ {
				if statusUpdates[i].Timestamp < startTime {
					t.Errorf("Status update timestamps not in chronological order: update %d (%d) < update %d (%d)",
						i, statusUpdates[i].Timestamp, i-1, statusUpdates[i-1].Timestamp)
				}
				startTime = statusUpdates[i].Timestamp
			}
		})
	}
}

func TestCommandStatusUpdateRPCFailure(t *testing.T) {
	var resultSent *pb.CommandResult
	command := &pb.Command{
		Id:      "test-cmd",
		Type:    pb.CommandType_SYSTEM,
		Payload: "echo test",
	}

	mockClient := &mockMinionServiceClient{
		updateStatusFunc: func(ctx context.Context, in *pb.CommandStatusUpdate, opts ...grpc.CallOption) (*pb.Ack, error) {
			return nil, errors.New("RPC failed")
		},
		getCommandsFunc: func(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (pb.MinionService_GetCommandsClient, error) {
			return &mockGetCommandsClientWithCommand{command: command}, nil
		},
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)
	minion := NewMinion("test-minion", mockClient, time.Hour, time.Hour, time.Hour, logger, atom)

	// Start command processing
	processor := minion.commandProcessor.(*commandProcessor)
	stream, _ := mockClient.GetCommands(context.Background(), &pb.Empty{})

	err := processor.ProcessCommands(context.Background(), stream, func(result *pb.CommandResult) error {
		resultSent = result
		return nil
	})

	// Command processing should complete even if status updates fail
	if err != nil && err != io.EOF {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify command result
	if resultSent == nil {
		t.Error("Expected command result but got none")
	} else {
		if resultSent.CommandId != command.Id {
			t.Errorf("Expected result command ID %s, got %s", command.Id, resultSent.CommandId)
		}
		if resultSent.ExitCode != 0 {
			t.Errorf("Expected exit code 0 for successful command, got %d", resultSent.ExitCode)
		}
	}
}

// Benchmark tests
func BenchmarkCommandExecution(b *testing.B) {
	mockClient := &mockMinionServiceClient{}
	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)
	minion := NewMinion("bench-minion", mockClient, time.Hour, time.Hour, time.Hour, logger, atom)

	command := &pb.Command{
		Id:      "bench-cmd",
		Type:    pb.CommandType_SYSTEM,
		Payload: "echo benchmark",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := minion.executeCommand(context.Background(), command)
		if err != nil {
			b.Fatalf("Command execution failed: %v", err)
		}
	}
}
