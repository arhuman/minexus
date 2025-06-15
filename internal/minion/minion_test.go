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
	registerFunc       func(ctx context.Context, in *pb.HostInfo, opts ...grpc.CallOption) (*pb.RegisterResponse, error)
	streamCommandsFunc func(ctx context.Context, opts ...grpc.CallOption) (pb.MinionService_StreamCommandsClient, error)
}

func (m *mockMinionServiceClient) Register(ctx context.Context, in *pb.HostInfo, opts ...grpc.CallOption) (*pb.RegisterResponse, error) {
	if m.registerFunc != nil {
		return m.registerFunc(ctx, in, opts...)
	}
	return &pb.RegisterResponse{Success: true, AssignedId: in.Id}, nil
}

func (m *mockMinionServiceClient) StreamCommands(ctx context.Context, opts ...grpc.CallOption) (pb.MinionService_StreamCommandsClient, error) {
	if m.streamCommandsFunc != nil {
		return m.streamCommandsFunc(ctx, opts...)
	}
	return &mockStreamCommandsClient{}, nil
}

// Mock implementation of StreamCommands stream client
type mockStreamCommandsClient struct {
	commands     []*pb.Command
	index        int
	closed       bool
	recvMsgs     []*pb.CommandStreamMessage
	sendMsgs     []*pb.CommandStreamMessage
	recvCallback func(*pb.CommandStreamMessage) error
	sendCallback func(*pb.CommandStreamMessage) error
}

func (m *mockStreamCommandsClient) Recv() (*pb.CommandStreamMessage, error) {
	if m.closed || m.index >= len(m.commands) {
		return nil, io.EOF
	}
	msg := &pb.CommandStreamMessage{
		Message: &pb.CommandStreamMessage_Command{
			Command: m.commands[m.index],
		},
	}
	m.index++
	if m.recvCallback != nil {
		if err := m.recvCallback(msg); err != nil {
			return nil, err
		}
	}
	m.recvMsgs = append(m.recvMsgs, msg)
	return msg, nil
}

func (m *mockStreamCommandsClient) Send(msg *pb.CommandStreamMessage) error {
	if m.closed {
		return errors.New("stream closed")
	}
	if m.sendCallback != nil {
		if err := m.sendCallback(msg); err != nil {
			return err
		}
	}
	m.sendMsgs = append(m.sendMsgs, msg)
	return nil
}

func (m *mockStreamCommandsClient) Header() (metadata.MD, error) {
	return metadata.MD{}, nil
}

func (m *mockStreamCommandsClient) Trailer() metadata.MD {
	return metadata.MD{}
}

func (m *mockStreamCommandsClient) CloseSend() error {
	m.closed = true
	return nil
}

func (m *mockStreamCommandsClient) Context() context.Context {
	return context.Background()
}

func (m *mockStreamCommandsClient) SendMsg(msg interface{}) error {
	return nil
}

func (m *mockStreamCommandsClient) RecvMsg(msg interface{}) error {
	return nil
}

// mockStreamCommandsClientWithCommand implements pb.MinionService_StreamCommandsClient with a single command
type mockStreamCommandsClientWithCommand struct {
	mockStreamCommandsClient
	command *pb.Command
	sent    bool
}

func (m *mockStreamCommandsClientWithCommand) Recv() (*pb.CommandStreamMessage, error) {
	if !m.sent {
		m.sent = true
		return &pb.CommandStreamMessage{
			Message: &pb.CommandStreamMessage_Command{
				Command: m.command,
			},
		}, nil
	}
	return nil, io.EOF
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
		streamCommandsFunc: func(ctx context.Context, opts ...grpc.CallOption) (pb.MinionService_StreamCommandsClient, error) {
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
			return &mockStreamCommandsClient{closed: true}, nil
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
		streamCommandsFunc: func(ctx context.Context, opts ...grpc.CallOption) (pb.MinionService_StreamCommandsClient, error) {
			if !commandsSent {
				commandsSent = true
				client := &mockStreamCommandsClient{commands: commands}
				client.sendCallback = func(msg *pb.CommandStreamMessage) error {
					if result := msg.GetResult(); result != nil {
						receivedResults = append(receivedResults, result)
					}
					return nil
				}
				return client, nil
			}
			// Return a client that immediately closes to prevent infinite reconnection
			return &mockStreamCommandsClient{closed: true}, nil
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

// mockConnectionManager implements ConnectionManager interface for testing
type mockConnectionManager struct {
	connected bool
}

func (m *mockConnectionManager) Connect(ctx context.Context) error { return nil }
func (m *mockConnectionManager) Disconnect() error                 { return nil }
func (m *mockConnectionManager) IsConnected() bool                 { return m.connected }
func (m *mockConnectionManager) Stream() (pb.MinionService_StreamCommandsClient, error) {
	return nil, nil
}
func (m *mockConnectionManager) HandleReconnection(ctx context.Context) error { return nil }

func TestGetIPAddress(t *testing.T) {
	logger := zap.NewNop()
	mockClient := &mockMinionServiceClient{}
	mockConn := &mockConnectionManager{connected: true}
	rm := NewRegistrationManager("test-id", mockClient, mockConn, logger)

	ip := rm.getIPAddress()
	if ip == "unknown" {
		t.Error("Expected valid IP address, got 'unknown'")
	}
	if ip == "" {
		t.Error("Expected non-empty IP address")
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
				streamCommandsFunc: func(ctx context.Context, opts ...grpc.CallOption) (pb.MinionService_StreamCommandsClient, error) {
					client := &mockStreamCommandsClientWithCommand{command: tc.command}
					client.sendCallback = func(msg *pb.CommandStreamMessage) error {
						switch m := msg.Message.(type) {
						case *pb.CommandStreamMessage_Status:
							statusUpdates = append(statusUpdates, m.Status)
						case *pb.CommandStreamMessage_Result:
							resultSent = m.Result
						}
						return nil
					}
					return client, nil
				},
			}

			logger := zap.NewNop()
			atom := zap.NewAtomicLevelAt(zap.InfoLevel)
			minion := NewMinion("test-minion", mockClient, time.Hour, time.Hour, time.Hour, logger, atom)

			// Start command processing
			processor := minion.commandProcessor.(*commandProcessor)
			stream, _ := mockClient.StreamCommands(context.Background())
			err := processor.ProcessCommands(context.Background(), stream)

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

	// Create a channel to track message sending
	msgCh := make(chan *pb.CommandStreamMessage, 10)

	mockClient := &mockMinionServiceClient{
		streamCommandsFunc: func(ctx context.Context, opts ...grpc.CallOption) (pb.MinionService_StreamCommandsClient, error) {
			client := &mockStreamCommandsClientWithCommand{
				command: command,
				mockStreamCommandsClient: mockStreamCommandsClient{
					sendCallback: func(msg *pb.CommandStreamMessage) error {
						// Send all messages to channel for inspection
						msgCh <- msg

						switch msg.Message.(type) {
						case *pb.CommandStreamMessage_Status:
							// Just track the message
							return nil
						case *pb.CommandStreamMessage_Result:
							resultSent = msg.GetResult()
							return nil
						}
						return nil
					},
				},
			}
			return client, nil
		},
	}

	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)
	minion := NewMinion("test-minion", mockClient, time.Hour, time.Hour, time.Hour, logger, atom)

	// Start command processing
	processor := minion.commandProcessor.(*commandProcessor)
	stream, _ := mockClient.StreamCommands(context.Background())
	err := processor.ProcessCommands(context.Background(), stream)

	// Command processing should complete
	if err != nil && err != io.EOF {
		t.Errorf("Unexpected error: %v", err)
	}

	// Collect all messages sent
	close(msgCh)
	var statusMsgs, resultMsgs int
	for msg := range msgCh {
		switch msg.Message.(type) {
		case *pb.CommandStreamMessage_Status:
			statusMsgs++
		case *pb.CommandStreamMessage_Result:
			resultMsgs++
		}
	}

	// Verify messages were sent
	if statusMsgs == 0 {
		t.Error("Expected at least one status update to be attempted")
	}
	if resultMsgs != 1 {
		t.Errorf("Expected exactly one result message, got %d", resultMsgs)
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
