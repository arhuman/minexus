package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"minexus/internal/command"
	pb "minexus/protogen"

	"github.com/chzyer/readline"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// mockConsoleServiceClient is a mock implementation for testing
type mockConsoleServiceClient struct {
	pb.ConsoleServiceClient
	returnError     bool
	minions         []*pb.HostInfo
	tags            []string
	commandAccepted bool
	commandId       string
	results         []*pb.CommandResult
	tagSuccess      bool
}

func (m *mockConsoleServiceClient) ListMinions(ctx context.Context, req *pb.Empty, opts ...grpc.CallOption) (*pb.MinionList, error) {
	if m.returnError {
		return nil, errors.New("mock error")
	}
	return &pb.MinionList{Minions: m.minions}, nil
}

func (m *mockConsoleServiceClient) ListTags(ctx context.Context, req *pb.Empty, opts ...grpc.CallOption) (*pb.TagList, error) {
	if m.returnError {
		return nil, errors.New("mock error")
	}
	return &pb.TagList{Tags: m.tags}, nil
}

func (m *mockConsoleServiceClient) SendCommand(ctx context.Context, req *pb.CommandRequest, opts ...grpc.CallOption) (*pb.CommandDispatchResponse, error) {
	if m.returnError {
		return nil, errors.New("mock error")
	}
	return &pb.CommandDispatchResponse{Accepted: m.commandAccepted, CommandId: m.commandId}, nil
}

func (m *mockConsoleServiceClient) GetCommandResults(ctx context.Context, req *pb.ResultRequest, opts ...grpc.CallOption) (*pb.CommandResults, error) {
	if m.returnError {
		return nil, errors.New("mock error")
	}
	return &pb.CommandResults{Results: m.results}, nil
}

func (m *mockConsoleServiceClient) SetTags(ctx context.Context, req *pb.SetTagsRequest, opts ...grpc.CallOption) (*pb.Ack, error) {
	if m.returnError {
		return nil, errors.New("mock error")
	}
	return &pb.Ack{Success: m.tagSuccess}, nil
}

func (m *mockConsoleServiceClient) UpdateTags(ctx context.Context, req *pb.UpdateTagsRequest, opts ...grpc.CallOption) (*pb.Ack, error) {
	if m.returnError {
		return nil, errors.New("mock error")
	}
	return &pb.Ack{Success: m.tagSuccess}, nil
}

// Helper function to capture stdout
func captureOutput(f func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// Helper to create console with mock client
func createMockConsole(mockClient *mockConsoleServiceClient) *Console {
	logger := zap.NewNop()
	grpcClient := &GRPCClient{client: mockClient}

	console := &Console{
		client: mockClient,
		grpc:   grpcClient,
		logger: logger,
	}

	// Initialize UI and parser manually for testing
	registry := command.SetupCommands()
	console.ui = NewUIManager(logger, registry)
	console.parser = NewCommandParser()

	return console
}

func TestNewConsole(t *testing.T) {
	mockClient := &mockConsoleServiceClient{}
	console := createMockConsole(mockClient)
	defer console.Shutdown()

	if console == nil {
		t.Fatal("NewConsole returned nil")
	}

	if console.client == nil {
		t.Error("Console client not set")
	}

	if console.logger == nil {
		t.Error("Console logger not set correctly")
	}

	if console.Registry() == nil {
		t.Error("Console registry not initialized")
	}

	if console.RL() == nil {
		t.Error("Readline instance not initialized")
	}
}

func TestSetupReadline(t *testing.T) {
	mockClient := &mockConsoleServiceClient{}
	logger := zap.NewNop()
	grpcClient := &GRPCClient{client: mockClient}

	console := &Console{
		client: mockClient,
		grpc:   grpcClient,
		logger: logger,
	}

	// Test setupReadline
	console.setupReadline()
	defer console.Shutdown()

	// In test environments without TTY, the readline instance might be nil
	// This is acceptable and the UI manager should handle it gracefully
	rlInstance := console.RL()
	if rlInstance != nil {
		t.Log("Readline instance created successfully")
	} else {
		t.Log("Readline instance is nil (expected in test environment without TTY)")
	}

	// Test that the setup doesn't panic regardless of TTY availability
	// The main thing is that setupReadline() completes without error
}

func TestCreateCompleter(t *testing.T) {
	mockClient := &mockConsoleServiceClient{}
	console := createMockConsole(mockClient)
	defer console.Shutdown()

	completer := console.createCompleter()
	if completer == nil {
		t.Fatal("Completer not created")
	}

	// Test that completer has expected commands
	// This is a bit tricky since readline.PrefixCompleter doesn't expose
	// its internal structure easily, but we can verify it was created
}

func TestFilterInput(t *testing.T) {
	tests := []struct {
		name     string
		input    rune
		expected rune
		allowed  bool
	}{
		{
			name:     "normal character",
			input:    'a',
			expected: 'a',
			allowed:  true,
		},
		{
			name:     "ctrl+z blocked",
			input:    readline.CharCtrlZ,
			expected: readline.CharCtrlZ,
			allowed:  false,
		},
		{
			name:     "ctrl+c allowed",
			input:    '\x03', // Ctrl+C character
			expected: '\x03',
			allowed:  true,
		},
		{
			name:     "space allowed",
			input:    ' ',
			expected: ' ',
			allowed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, allowed := filterInput(tt.input)

			if result != tt.expected {
				t.Errorf("Expected rune %v, got %v", tt.expected, result)
			}

			if allowed != tt.allowed {
				t.Errorf("Expected allowed %v, got %v", tt.allowed, allowed)
			}
		})
	}
}

func TestShowHistory(t *testing.T) {
	mockClient := &mockConsoleServiceClient{}
	console := createMockConsole(mockClient)
	defer console.Shutdown()

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	console.showHistory()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify expected content in output
	expectedStrings := []string{
		"Command history is available",
		"↑ (Up Arrow)",
		"↓ (Down Arrow)",
		"Ctrl+R",
		"~/.minexus_history",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain '%s', got: %s", expected, output)
		}
	}
}

func TestShutdown(t *testing.T) {
	mockClient := &mockConsoleServiceClient{}
	console := createMockConsole(mockClient)

	// Verify readline is initialized
	if console.RL() == nil {
		t.Fatal("Readline not initialized")
	}

	// Test shutdown
	console.Shutdown()

	// Test that shutdown can be called multiple times without panic
	console.Shutdown()
}

func TestTabCompletionCommands(t *testing.T) {
	mockClient := &mockConsoleServiceClient{}
	console := createMockConsole(mockClient)
	defer console.Shutdown()

	// We can't easily test the actual completion behavior without complex
	// readline interaction, but we can verify the completer was created
	// with the expected structure
	completer := console.createCompleter()
	if completer == nil {
		t.Error("Tab completion not properly configured")
	}

	// Test that createCompleter doesn't panic and returns valid completer
	if completer == nil {
		t.Error("Completer should not be nil")
	}
}

func TestHandleCommand(t *testing.T) {
	mockClient := &mockConsoleServiceClient{
		minions:         []*pb.HostInfo{{Id: "test123", Hostname: "testhost", Ip: "192.168.1.1", Os: "linux"}},
		tags:            []string{"env=prod", "role=web"},
		commandAccepted: true,
		commandId:       "cmd-123",
		tagSuccess:      true,
	}
	console := createMockConsole(mockClient)
	defer console.Shutdown()

	tests := []struct {
		name        string
		command     string
		args        []string
		expectError bool
	}{
		{"help", "help", []string{}, false},
		{"help_alias", "h", []string{}, false},
		{"help_with_command", "help", []string{"version"}, false},
		{"version", "version", []string{}, false},
		{"version_alias", "v", []string{}, false},
		{"minion_list", "minion-list", []string{}, false},
		{"minion_list_alias", "lm", []string{}, false},
		{"tag_list", "tag-list", []string{}, false},
		{"tag_list_alias", "lt", []string{}, false},
		{"command_send", "command-send", []string{"all", "echo", "test"}, false},
		{"command_send_alias", "cmd", []string{"all", "echo", "test"}, false},
		{"result_get", "result-get", []string{"cmd-123"}, false},
		{"result_get_alias", "results", []string{"cmd-123"}, false},
		{"tag_set", "tag-set", []string{"minion123", "env=test"}, false},
		{"tag_update", "tag-update", []string{"minion123", "+env=staging"}, false},
		{"clear", "clear", []string{}, false},
		{"history", "history", []string{}, false},
		{"unknown", "unknown", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(func() {
				console.handleCommand(tt.command, tt.args)
			})

			// Basic validation that no panic occurred and some output was produced
			if tt.command == "unknown" {
				if !strings.Contains(output, "Unknown command") {
					t.Error("Expected unknown command message")
				}
			}
		})
	}
}

func TestListMinions(t *testing.T) {
	t.Run("with_minions", func(t *testing.T) {
		mockClient := &mockConsoleServiceClient{
			minions: []*pb.HostInfo{
				{
					Id:       "abc123",
					Hostname: "testhost",
					Ip:       "192.168.1.1",
					Os:       "linux",
					LastSeen: time.Now().Unix(),
					Tags:     map[string]string{"env": "prod", "role": "web"},
				},
			},
		}
		console := createMockConsole(mockClient)
		defer console.Shutdown()

		output := captureOutput(func() {
			console.listMinions(context.Background())
		})

		expectedStrings := []string{
			"Connected minions",
			"abc123",
			"testhost",
			"192.168.1.1",
			"linux",
			"env=prod",
		}

		for _, expected := range expectedStrings {
			if !strings.Contains(output, expected) {
				t.Errorf("Expected output to contain '%s', got: %s", expected, output)
			}
		}
	})

	t.Run("no_minions", func(t *testing.T) {
		mockClient := &mockConsoleServiceClient{minions: []*pb.HostInfo{}}
		console := createMockConsole(mockClient)
		defer console.Shutdown()

		output := captureOutput(func() {
			console.listMinions(context.Background())
		})

		if !strings.Contains(output, "No minions connected") {
			t.Error("Expected 'No minions connected' message")
		}
	})

	t.Run("error", func(t *testing.T) {
		mockClient := &mockConsoleServiceClient{returnError: true}
		console := createMockConsole(mockClient)
		defer console.Shutdown()

		output := captureOutput(func() {
			console.listMinions(context.Background())
		})

		if !strings.Contains(output, "Error listing minions") {
			t.Error("Expected error message")
		}
	})
}

func TestSendCommand(t *testing.T) {
	t.Run("no_args", func(t *testing.T) {
		mockClient := &mockConsoleServiceClient{}
		console := createMockConsole(mockClient)
		defer console.Shutdown()

		output := captureOutput(func() {
			console.sendCommand(context.Background(), []string{})
		})

		if !strings.Contains(output, "Usage:") {
			t.Error("Expected usage message when no args provided")
		}
	})

	t.Run("new_syntax_all", func(t *testing.T) {
		mockClient := &mockConsoleServiceClient{
			commandAccepted: true,
			commandId:       "cmd-123",
		}
		console := createMockConsole(mockClient)
		defer console.Shutdown()

		output := captureOutput(func() {
			console.sendCommand(context.Background(), []string{"all", "echo", "test"})
		})

		if !strings.Contains(output, "Command dispatched successfully") {
			t.Error("Expected success message")
		}
		if !strings.Contains(output, "cmd-123") {
			t.Error("Expected command ID in output")
		}
	})
}

func TestIsHexString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid_hex_lowercase", "abc123def456", true},
		{"valid_hex_uppercase", "ABC123DEF456", true},
		{"valid_hex_mixed", "AbC123DeF456", true},
		{"valid_hex_numbers_only", "123456789012", true},
		{"invalid_with_space", "abc 123", false},
		{"invalid_with_special_char", "abc-123", false},
		{"invalid_with_letters", "abcxyz", false},
		{"empty_string", "", true},
		{"single_char_valid", "a", true},
		{"single_char_invalid", "x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHexString(tt.input)
			if result != tt.expected {
				t.Errorf("isHexString(%s) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestClearScreen(t *testing.T) {
	mockClient := &mockConsoleServiceClient{}
	console := createMockConsole(mockClient)
	defer console.Shutdown()

	output := captureOutput(func() {
		console.clearScreen()
	})

	// Check for ANSI clear screen sequence
	if !strings.Contains(output, "\033[2J\033[H") {
		t.Error("Expected ANSI clear screen sequence")
	}
}

func TestAddToHistory(t *testing.T) {
	mockClient := &mockConsoleServiceClient{}
	console := createMockConsole(mockClient)
	defer console.Shutdown()

	// Test that addToHistory doesn't panic
	console.addToHistory("test command")

	// Test with nil readline instance (simulate shutdown)
	console.Shutdown()
	console.addToHistory("test command") // Should not panic
}

func TestFormatTags(t *testing.T) {
	tests := []struct {
		name     string
		tags     map[string]string
		expected string
	}{
		{
			name:     "empty_tags",
			tags:     map[string]string{},
			expected: "-",
		},
		{
			name:     "single_tag",
			tags:     map[string]string{"env": "prod"},
			expected: "env=prod",
		},
		{
			name: "multiple_tags",
			tags: map[string]string{"env": "prod", "role": "web"},
			// Note: map iteration order is not guaranteed, so we test both possibilities
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTags(tt.tags)

			if tt.name == "empty_tags" {
				if result != tt.expected {
					t.Errorf("Expected '%s', got '%s'", tt.expected, result)
				}
			} else if tt.name == "single_tag" {
				if result != tt.expected {
					t.Errorf("Expected '%s', got '%s'", tt.expected, result)
				}
			} else if tt.name == "multiple_tags" {
				// Check that both tags are present
				if !strings.Contains(result, "env=prod") || !strings.Contains(result, "role=web") {
					t.Errorf("Expected both tags in result, got '%s'", result)
				}
			}
		})
	}
}

func TestFormatLastSeen(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		timestamp int64
		expected  string
	}{
		{
			name:      "never_seen",
			timestamp: 0,
			expected:  "Never",
		},
		{
			name:      "just_now",
			timestamp: now.Unix(),
			expected:  "Just now",
		},
		{
			name:      "minutes_ago",
			timestamp: now.Add(-5 * time.Minute).Unix(),
			expected:  "5m ago",
		},
		{
			name:      "hours_ago",
			timestamp: now.Add(-2 * time.Hour).Unix(),
			expected:  "2h ago",
		},
		{
			name:      "one_day_ago",
			timestamp: now.Add(-24 * time.Hour).Unix(),
			expected:  "1 day ago",
		},
		{
			name:      "days_ago",
			timestamp: now.Add(-3 * 24 * time.Hour).Unix(),
			expected:  "3d ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatLastSeen(tt.timestamp)

			if !strings.Contains(result, tt.expected) {
				t.Errorf("Expected result to contain '%s', got '%s'", tt.expected, result)
			}
		})
	}
}
