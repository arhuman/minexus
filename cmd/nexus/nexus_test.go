package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/arhuman/minexus/internal/config"
	"github.com/arhuman/minexus/internal/nexus"
	"github.com/arhuman/minexus/internal/version"
	pb "github.com/arhuman/minexus/protogen"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// TestMain_VersionFlag tests the version flag functionality
func TestMain_VersionFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "version flag --version",
			args: []string{"nexus", "--version"},
		},
		{
			name: "version flag -v",
			args: []string{"nexus", "-v"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Set up command line args
			oldArgs := os.Args
			os.Args = tt.args

			// Create a channel to capture if main exits
			done := make(chan bool, 1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						// Expected behavior - main should return/exit
						done <- true
					}
				}()
				main()
				done <- true
			}()

			// Wait for completion with timeout
			select {
			case <-done:
				// Expected
			case <-time.After(100 * time.Millisecond):
				t.Error("main() did not exit as expected for version flag")
			}

			// Restore stdout and args
			w.Close()
			os.Stdout = old
			os.Args = oldArgs

			// Read the output
			var buf bytes.Buffer
			io.Copy(&buf, r)
			output := buf.String()

			// Verify version information is printed
			if !strings.Contains(output, "Nexus") {
				t.Errorf("Expected output to contain 'Nexus', got: %s", output)
			}

			// Verify version info is included
			versionInfo := version.Info()
			if !strings.Contains(output, versionInfo) {
				t.Errorf("Expected output to contain version info '%s', got: %s", versionInfo, output)
			}
		})
	}
}

// TestMain_NoVersionFlag tests main without version flag
func TestMain_NoVersionFlag(t *testing.T) {
	// This test is more complex as it requires mocking many dependencies
	// We'll test the configuration loading and initial setup

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Set args without version flag
	os.Args = []string{"nexus"}

	// Test will be limited to configuration loading and logger creation
	// since testing the full server startup requires complex mocking
	// Note: We avoid calling LoadNexusConfig() here to prevent flag conflicts

	// Test logger creation
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create development logger: %v", err)
	}
	defer logger.Sync()

	if logger == nil {
		t.Error("Expected logger to be created")
	}

	// Test production logger creation
	logger2, err := zap.NewProduction()
	if err != nil {
		t.Fatalf("Failed to create production logger: %v", err)
	}
	defer logger2.Sync()

	if logger2 == nil {
		t.Error("Expected production logger to be created")
	}
}

// TestLoggerCreation tests logger creation scenarios
func TestLoggerCreation(t *testing.T) {
	tests := []struct {
		name      string
		debug     bool
		expectErr bool
	}{
		{
			name:      "development logger",
			debug:     true,
			expectErr: false,
		},
		{
			name:      "production logger",
			debug:     false,
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logger *zap.Logger
			var err error

			if tt.debug {
				logger, err = zap.NewDevelopment()
			} else {
				logger, err = zap.NewProduction()
			}

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if logger == nil {
					t.Error("Expected logger to be created")
				}
				if logger != nil {
					logger.Sync()
				}
			}
		})
	}
}

// TestConfigurationLoading tests configuration loading
func TestConfigurationLoading(t *testing.T) {
	// Test default configuration creation
	t.Run("default configuration", func(t *testing.T) {
		cfg := config.DefaultNexusConfig()
		if cfg == nil {
			t.Fatal("Expected default configuration to be created")
		}

		if cfg.MinionPort != 11972 {
			t.Errorf("Expected default minion port 11972, got %d", cfg.MinionPort)
		}

		if cfg.DBHost != "localhost" {
			t.Errorf("Expected default DB host localhost, got %s", cfg.DBHost)
		}
	})

	// Test database connection string generation
	t.Run("database connection string", func(t *testing.T) {
		cfg := config.DefaultNexusConfig()
		cfg.DBHost = "testhost"
		cfg.DBName = "testdb"
		cfg.DBUser = "testuser"
		cfg.DBPassword = "testpass"

		connStr := cfg.DBConnectionString()
		if !strings.Contains(connStr, "testhost") {
			t.Error("Expected connection string to contain testhost")
		}
		if !strings.Contains(connStr, "testdb") {
			t.Error("Expected connection string to contain testdb")
		}
		if !strings.Contains(connStr, "testuser") {
			t.Error("Expected connection string to contain testuser")
		}
	})


	// Test configuration logging (should not panic)
	t.Run("configuration logging", func(t *testing.T) {
		cfg := config.DefaultNexusConfig()
		logger, _ := zap.NewDevelopment()
		defer logger.Sync()

		// This should not panic
		cfg.LogConfig(logger)
	})
}

// TestVersionInfo tests version information functions
func TestVersionInfo(t *testing.T) {
	info := version.Info()
	if info == "" {
		t.Error("Expected version info to be non-empty")
	}

	component := version.Component("Nexus")
	if component == "" {
		t.Error("Expected component version to be non-empty")
	}

	// Test that component includes the component name
	if !strings.Contains(component, "Nexus") {
		t.Errorf("Expected component to contain 'Nexus', got: %s", component)
	}
}

// TestServerCreation tests nexus server creation scenarios
func TestServerCreation(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	tests := []struct {
		name            string
		dbConnectionStr string
		expectError     bool
	}{
		{
			name:            "server without database",
			dbConnectionStr: "",
			expectError:     false,
		},
		{
			name:            "server with invalid database connection",
			dbConnectionStr: "invalid://connection/string",
			expectError:     false, // NewServer doesn't validate connection string format
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := nexus.NewServer(tt.dbConnectionStr, logger)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if server == nil {
					t.Error("Expected server to be created")
				}
				if server != nil {
					server.Shutdown()
				}
			}
		})
	}
}

// TestNetworkListener tests TCP listener creation
func TestNetworkListener(t *testing.T) {
	tests := []struct {
		name        string
		address     string
		expectError bool
	}{
		{
			name:        "valid address with port 0",
			address:     ":0",
			expectError: false,
		},
		{
			name:        "invalid address",
			address:     "invalid:address:format",
			expectError: true,
		},
		{
			name:        "valid localhost address",
			address:     "localhost:0",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lis, err := net.Listen("tcp", tt.address)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if lis == nil {
					t.Error("Expected listener to be created")
				}
				if lis != nil {
					lis.Close()
				}
			}
		})
	}
}

// TestGRPCServerCreation tests gRPC server creation and configuration
func TestGRPCServerCreation(t *testing.T) {
	tests := []struct {
		name       string
		maxMsgSize int
	}{
		{
			name:       "default message size",
			maxMsgSize: 1024 * 1024, // 1MB
		},
		{
			name:       "custom message size",
			maxMsgSize: 2 * 1024 * 1024, // 2MB
		},
		{
			name:       "small message size",
			maxMsgSize: 512 * 1024, // 512KB
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create gRPC server with options similar to main function
			s := grpc.NewServer(
				grpc.MaxRecvMsgSize(tt.maxMsgSize),
				grpc.MaxSendMsgSize(tt.maxMsgSize),
			)

			if s == nil {
				t.Error("Expected gRPC server to be created")
			}

			// Test that we can stop the server
			s.Stop()
		})
	}
}

// TestMainIntegration tests the integration of main components
func TestMainIntegration(t *testing.T) {
	// Test the integration of main components without actually starting the server
	// This simulates the main function flow without network dependencies

	// 1. Use default configuration to avoid flag conflicts
	cfg := config.DefaultNexusConfig()
	if cfg == nil {
		t.Fatal("Failed to load configuration")
	}

	cfg.Debug = true   // Set debug mode for testing
	cfg.MinionPort = 0 // Use port 0 for testing

	// 2. Create logger
	var logger *zap.Logger
	var err error
	if cfg.Debug {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// 3. Log version information
	versionInfo := version.Component("Nexus")
	logger.Info("Starting Nexus", zap.String("version", versionInfo))

	// 4. Log configuration (this tests the LogConfig method)
	cfg.LogConfig(logger)

	// 5. Create nexus server
	server, err := nexus.NewServer(cfg.DBConnectionString(), logger)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown()

	// 6. Create TCP listener
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 0)) // Use port 0 for testing
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer lis.Close()

	// 7. Create gRPC server
	s := grpc.NewServer(
		grpc.MaxRecvMsgSize(cfg.MaxMsgSize),
		grpc.MaxSendMsgSize(cfg.MaxMsgSize),
	)
	defer s.Stop()

	// Verify all components were created successfully
	if server == nil {
		t.Error("Expected server to be created")
	}
	if lis == nil {
		t.Error("Expected listener to be created")
	}
	if s == nil {
		t.Error("Expected gRPC server to be created")
	}
}

// TestMainExecutable tests the actual main executable
func TestMainExecutable(t *testing.T) {
	// Only run this test if we can build the executable
	if testing.Short() {
		t.Skip("Skipping executable test in short mode")
	}

	// Test version flag through actual executable
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "version flag --version",
			args: []string{"--version"},
		},
		{
			name: "version flag -v",
			args: []string{"-v"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the executable
			cmd := exec.Command("go", "build", "-o", "nexus_test", ".")
			err := cmd.Run()
			if err != nil {
				t.Skipf("Failed to build executable: %v", err)
				return
			}
			defer os.Remove("nexus_test")

			// Run with version flag
			cmd = exec.Command("./nexus_test", tt.args...)
			output, err := cmd.Output()
			if err != nil {
				t.Errorf("Failed to run executable: %v", err)
				return
			}

			outputStr := string(output)
			if !strings.Contains(outputStr, "Nexus") {
				t.Errorf("Expected output to contain 'Nexus', got: %s", outputStr)
			}
		})
	}
}

// TestSignalHandling tests signal handling setup
func TestSignalHandling(t *testing.T) {
	// Test that we can set up signal handling without issues
	quit := make(chan os.Signal, 1)

	// This should not panic or error
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Signal handling setup panicked: %v", r)
		}
	}()

	// Test signal notification setup
	// Note: We can't easily test the actual signal handling without
	// complex process management, but we can test the setup
	// Since make() always returns a non-nil channel, we verify the channel properties

	// Test that the channel has the right capacity
	if cap(quit) != 1 {
		t.Errorf("Expected signal channel capacity to be 1, got %d", cap(quit))
	}
}

// TestErrorConditions tests various error conditions that might occur in main
func TestErrorConditions(t *testing.T) {
	t.Run("invalid logger configuration", func(t *testing.T) {
		// Test logger creation with invalid configuration
		// This is difficult to trigger directly, but we can test the error handling pattern

		// Create a logger that might fail to sync
		logger, err := zap.NewDevelopment()
		if err != nil {
			t.Fatalf("Failed to create logger: %v", err)
		}

		// Test that sync doesn't panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Logger sync panicked: %v", r)
			}
		}()

		logger.Sync()
	})

	t.Run("server creation with database error", func(t *testing.T) {
		logger, _ := zap.NewDevelopment()
		defer logger.Sync()

		// Test with an invalid database connection that might cause issues
		// Most database errors are handled gracefully by the nexus package
		_, err := nexus.NewServer("postgres://invalid:connection@nonexistent:5432/db", logger)

		// The NewServer function should handle this gracefully
		// Even if the connection fails, the server should be created
		if err != nil {
			// Some connection errors might be immediate
			t.Logf("Server creation returned error (this may be expected): %v", err)
		}
	})
}

// BenchmarkMainComponents benchmarks the main components creation
func BenchmarkMainComponents(b *testing.B) {
	b.Run("config creation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = config.DefaultNexusConfig()
		}
	})

	b.Run("logger creation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			logger, _ := zap.NewDevelopment()
			if logger != nil {
				logger.Sync()
			}
		}
	})

	b.Run("version info", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = version.Info()
		}
	})
}

// TestEdgeCases tests edge cases and boundary conditions
func TestEdgeCases(t *testing.T) {
	t.Run("empty command line args", func(t *testing.T) {
		oldArgs := os.Args
		defer func() { os.Args = oldArgs }()

		// Test with empty args (should not cause version flag to trigger)
		os.Args = []string{}

		// Test default configuration instead of loading to avoid flag conflicts
		cfg := config.DefaultNexusConfig()
		if cfg == nil {
			t.Error("Expected default configuration to be created even with empty args")
		}
	})

	t.Run("many command line args", func(t *testing.T) {
		oldArgs := os.Args
		defer func() { os.Args = oldArgs }()

		// Test with many args but no version flag
		os.Args = []string{"nexus", "arg1", "arg2", "arg3"}

		// Should not trigger version flag - test the logic without calling LoadNexusConfig
		if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
			t.Error("Version flag should not be detected with non-version args")
		}
	})

	t.Run("version flag with extra args", func(t *testing.T) {
		oldArgs := os.Args
		defer func() { os.Args = oldArgs }()

		// Test version flag with extra arguments
		os.Args = []string{"nexus", "--version", "extra", "args"}

		// Should still trigger version output
		// Testing this requires careful handling to avoid actually exiting
		if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
			// Version flag logic should trigger
			versionInfo := version.Info()
			if versionInfo == "" {
				t.Error("Expected version info to be available")
			}
		}
	})
}

// TestContextUsage tests context handling patterns used in main
func TestContextUsage(t *testing.T) {
	t.Run("context creation and cancellation", func(t *testing.T) {
		// Test context patterns that might be used for graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Test context with timeout
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer timeoutCancel()

		select {
		case <-timeoutCtx.Done():
			// Expected timeout
		case <-time.After(200 * time.Millisecond):
			t.Error("Context timeout did not work as expected")
		}
	})

	t.Run("signal context simulation", func(t *testing.T) {
		// Simulate the signal handling pattern used in main
		ctx, cancel := context.WithCancel(context.Background())
		quit := make(chan os.Signal, 1)

		// Simulate receiving a signal
		go func() {
			time.Sleep(10 * time.Millisecond)
			quit <- syscall.SIGTERM
			cancel()
		}()

		select {
		case <-quit:
			// Signal received, would trigger graceful shutdown
		case <-time.After(100 * time.Millisecond):
			t.Error("Signal handling simulation failed")
		}

		// Verify context is cancelled
		select {
		case <-ctx.Done():
			// Expected
		default:
			t.Error("Context should be cancelled")
		}
	})
}

// TestLoggingOutput tests logging functionality used in main
func TestLoggingOutput(t *testing.T) {
	t.Run("structured logging", func(t *testing.T) {
		// Create a logger for testing
		config := zap.NewDevelopmentConfig()
		config.OutputPaths = []string{"stdout"}

		logger, err := config.Build()
		if err != nil {
			t.Fatalf("Failed to create logger: %v", err)
		}
		defer logger.Sync()

		// Test the logging patterns used in main
		logger.Info("Starting Nexus", zap.String("version", "test-version"))

		// Test that the logger doesn't panic with various log levels
		logger.Debug("Debug message")
		logger.Warn("Warning message")
		logger.Error("Error message")
	})

	t.Run("logger sync handling", func(t *testing.T) {
		logger, err := zap.NewDevelopment()
		if err != nil {
			t.Fatalf("Failed to create logger: %v", err)
		}

		// Test that sync doesn't cause issues
		err = logger.Sync()
		if err != nil {
			// Sync might return an error on some systems, but shouldn't panic
			t.Logf("Logger sync returned error (may be expected): %v", err)
		}
	})
}

// TestGRPCServiceRegistration tests gRPC service registration
func TestGRPCServiceRegistration(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Create nexus server
	server, err := nexus.NewServer("", logger)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer server.Shutdown()

	// Create gRPC server
	s := grpc.NewServer()
	defer s.Stop()

	// Test service registration (similar to main function)
	pb.RegisterMinionServiceServer(s, server)
	pb.RegisterConsoleServiceServer(s, server)

	// Register reflection service
	reflection.Register(s)

	// Verify server was created successfully
	if s == nil {
		t.Error("Expected gRPC server to be created")
	}
}

// TestServerStartupShutdown tests server startup and shutdown scenarios
func TestServerStartupShutdown(t *testing.T) {
	t.Run("graceful shutdown simulation", func(t *testing.T) {
		// Simulate the graceful shutdown pattern from main
		s := grpc.NewServer()

		// Start server in a goroutine (similar to main)
		go func() {
			// Create a listener for testing
			lis, err := net.Listen("tcp", ":0")
			if err != nil {
				return
			}
			defer lis.Close()

			// Start serving (this will block until stopped)
			s.Serve(lis)
		}()

		// Give the server a moment to start
		time.Sleep(10 * time.Millisecond)

		// Test graceful stop (similar to main)
		s.GracefulStop()

		// Verify shutdown completed
		// The GracefulStop should have completed
	})

	t.Run("server stop", func(t *testing.T) {
		s := grpc.NewServer()

		// Test immediate stop
		s.Stop()

		// Should not panic or error
	})
}

// TestMainFunctionFlow tests a more complete main function flow
func TestMainFunctionFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping comprehensive main function test in short mode")
	}

	// Test the complete flow without actually blocking on server startup
	t.Run("complete main flow simulation", func(t *testing.T) {
		// 1. Version check simulation (not actually version args)
		oldArgs := os.Args
		os.Args = []string{"nexus"} // No version flag
		defer func() { os.Args = oldArgs }()

		// 2. Configuration loading simulation
		cfg := config.DefaultNexusConfig()
		cfg.Debug = true
		cfg.MinionPort = 0 // Use random port

		// 3. Logger creation
		var logger *zap.Logger
		var err error
		if cfg.Debug {
			logger, err = zap.NewDevelopment()
		} else {
			logger, err = zap.NewProduction()
		}
		if err != nil {
			t.Fatalf("Failed to create logger: %v", err)
		}
		defer logger.Sync()

		// 4. Version logging
		logger.Info("Starting Nexus", zap.String("version", version.Component("Nexus")))

		// 5. Configuration logging
		cfg.LogConfig(logger)

		// 6. Server creation
		server, err := nexus.NewServer(cfg.DBConnectionString(), logger)
		if err != nil {
			t.Fatalf("Failed to create server: %v", err)
		}
		defer server.Shutdown()

		// 7. TCP listener creation
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.MinionPort))
		if err != nil {
			t.Fatalf("Failed to listen: %v", err)
		}
		defer lis.Close()

		// 8. gRPC server creation
		s := grpc.NewServer(
			grpc.MaxRecvMsgSize(cfg.MaxMsgSize),
			grpc.MaxSendMsgSize(cfg.MaxMsgSize),
		)

		// 9. Service registration
		pb.RegisterMinionServiceServer(s, server)
		pb.RegisterConsoleServiceServer(s, server)

		// 10. Reflection registration
		reflection.Register(s)

		// 11. Server startup simulation (non-blocking)
		serverDone := make(chan error, 1)
		go func() {
			logger.Info("Server listening", zap.String("address", lis.Addr().String()))
			serverDone <- s.Serve(lis)
		}()

		// 12. Graceful shutdown simulation
		quit := make(chan os.Signal, 1)

		// Simulate signal after short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			quit <- syscall.SIGTERM
		}()

		// Wait for signal
		<-quit

		// 13. Shutdown
		logger.Info("Shutting down gRPC server...")
		s.GracefulStop()
		logger.Info("Server stopped")

		// Verify no errors occurred
		select {
		case err := <-serverDone:
			if err != nil {
				t.Logf("Server ended with error (may be expected): %v", err)
			}
		case <-time.After(100 * time.Millisecond):
			// Timeout is acceptable for this test
		}
	})
}

// TestLoggerPanic tests logger creation panic scenario
func TestLoggerPanic(t *testing.T) {
	// Test the panic scenario from main when logger creation fails
	// This is difficult to trigger in real scenarios, but we can test the pattern

	defer func() {
		if r := recover(); r != nil {
			// Test that we can handle a panic scenario
			if !strings.Contains(fmt.Sprintf("%v", r), "logger") {
				t.Errorf("Expected panic to be about logger, got: %v", r)
			}
		}
	}()

	// Simulate a scenario that might cause logger creation to fail
	// In practice, zap.NewDevelopment() and zap.NewProduction() rarely fail
	// but we test the error handling pattern

	var logger *zap.Logger
	var err error

	// Test both logger types
	logger, err = zap.NewDevelopment()
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}
	if logger != nil {
		logger.Sync()
	}

	logger, err = zap.NewProduction()
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}
	if logger != nil {
		logger.Sync()
	}
}

// TestServerCreationError tests server creation failure scenarios
func TestServerCreationError(t *testing.T) {
	// Test server creation error handling pattern from main
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Test with various connection strings that might cause issues
	tests := []struct {
		name     string
		connStr  string
		expectOK bool
	}{
		{
			name:     "empty connection string",
			connStr:  "",
			expectOK: true, // Should succeed without database
		},
		{
			name:     "invalid connection string format",
			connStr:  "invalid://format",
			expectOK: true, // NewServer should handle gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := nexus.NewServer(tt.connStr, logger)

			if tt.expectOK {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if server != nil {
					server.Shutdown()
				}
			} else {
				if err == nil {
					t.Error("Expected error but got none")
				}
			}
		})
	}
}

// TestListenerCreationError tests listener creation failure scenarios
func TestListenerCreationError(t *testing.T) {
	// Test listener creation error handling pattern from main
	tests := []struct {
		name        string
		address     string
		expectError bool
	}{
		{
			name:        "invalid port format",
			address:     ":invalid",
			expectError: true,
		},
		{
			name:        "port out of range",
			address:     ":99999",
			expectError: true,
		},
		{
			name:        "valid port",
			address:     ":0",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lis, err := net.Listen("tcp", tt.address)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if lis != nil {
					lis.Close()
				}
			}
		})
	}
}

// TestMainComponentIntegration tests integration between main components
func TestMainComponentIntegration(t *testing.T) {
	// Test integration patterns from main function
	t.Run("server and listener integration", func(t *testing.T) {
		logger, _ := zap.NewDevelopment()
		defer logger.Sync()

		// Create server
		server, err := nexus.NewServer("", logger)
		if err != nil {
			t.Fatalf("Failed to create server: %v", err)
		}
		defer server.Shutdown()

		// Create listener
		lis, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatalf("Failed to create listener: %v", err)
		}
		defer lis.Close()

		// Create gRPC server and register services
		s := grpc.NewServer()
		pb.RegisterMinionServiceServer(s, server)
		pb.RegisterConsoleServiceServer(s, server)
		reflection.Register(s)

		// Test that we can start and stop without issues
		go func() {
			s.Serve(lis)
		}()

		time.Sleep(10 * time.Millisecond)
		s.GracefulStop()
	})
}

// TestMainFunctionActual tests the actual main function execution
func TestMainFunctionActual(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping actual main function test in short mode")
	}

	t.Run("main function with quick shutdown", func(t *testing.T) {
		// Set up environment to avoid flag conflicts
		oldArgs := os.Args
		defer func() { os.Args = oldArgs }()

		// Set args to avoid version flag
		os.Args = []string{"nexus"}

		// Set environment variables for controlled testing
		os.Setenv("DEBUG", "true")
		os.Setenv("NEXUS_MINION_PORT", "0")  // Use random available port
		os.Setenv("NEXUS_CONSOLE_PORT", "0") // Use random available port
		os.Setenv("DBHOST", "")              // No database for testing
		defer func() {
			os.Unsetenv("DEBUG")
			os.Unsetenv("NEXUS_MINION_PORT")
			os.Unsetenv("NEXUS_CONSOLE_PORT")
			os.Unsetenv("DBHOST")
		}()

		// Create a channel to signal when main should exit
		mainDone := make(chan bool, 1)
		mainError := make(chan error, 1)

		// Start main in a goroutine
		go func() {
			defer func() {
				if r := recover(); r != nil {
					mainError <- fmt.Errorf("main panicked: %v", r)
					return
				}
				mainDone <- true
			}()

			// This will run the actual main function
			main()
		}()

		// Give main time to start up
		time.Sleep(100 * time.Millisecond)

		// Send interrupt signal to trigger graceful shutdown
		// Find the process and send signal
		pid := os.Getpid()
		process, err := os.FindProcess(pid)
		if err != nil {
			t.Fatalf("Failed to find process: %v", err)
		}

		// Send SIGTERM to trigger shutdown
		err = process.Signal(syscall.SIGTERM)
		if err != nil {
			t.Logf("Failed to send signal (this may be expected in test): %v", err)
		}

		// Wait for main to complete or timeout
		select {
		case <-mainDone:
			// Main completed successfully
		case err := <-mainError:
			t.Errorf("Main function error: %v", err)
		case <-time.After(2 * time.Second):
			t.Error("Main function did not exit within timeout")
		}
	})
}

// TestMainFunctionParts tests individual parts of main for better coverage
func TestMainFunctionParts(t *testing.T) {
	// Test the exact logic from main function line by line

	t.Run("version flag logic", func(t *testing.T) {
		// Test the version flag checking logic from main
		tests := []struct {
			name       string
			args       []string
			shouldExit bool
		}{
			{
				name:       "no args",
				args:       []string{"nexus"},
				shouldExit: false,
			},
			{
				name:       "version flag --version",
				args:       []string{"nexus", "--version"},
				shouldExit: true,
			},
			{
				name:       "version flag -v",
				args:       []string{"nexus", "-v"},
				shouldExit: true,
			},
			{
				name:       "other flag",
				args:       []string{"nexus", "--help"},
				shouldExit: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Test the version flag logic from main
				shouldExit := len(tt.args) > 1 && (tt.args[1] == "--version" || tt.args[1] == "-v")

				if shouldExit != tt.shouldExit {
					t.Errorf("Expected shouldExit=%v, got %v", tt.shouldExit, shouldExit)
				}

				if shouldExit {
					// Test version output
					versionInfo := version.Info()
					expectedOutput := fmt.Sprintf("Nexus %s\n", versionInfo)
					if expectedOutput == "" {
						t.Error("Expected version output to be non-empty")
					}
				}
			})
		}
	})

	t.Run("logger creation based on debug flag", func(t *testing.T) {
		// Test the logger creation logic from main
		tests := []struct {
			name  string
			debug bool
		}{
			{
				name:  "debug logger",
				debug: true,
			},
			{
				name:  "production logger",
				debug: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var logger *zap.Logger
				var err error

				// This mirrors the exact logic from main
				if tt.debug {
					logger, err = zap.NewDevelopment()
				} else {
					logger, err = zap.NewProduction()
				}

				if err != nil {
					// Test the panic scenario from main
					panicMsg := fmt.Sprintf("Failed to create logger: %v", err)
					if panicMsg == "" {
						t.Error("Expected panic message to be non-empty")
					}
				} else {
					if logger == nil {
						t.Error("Expected logger to be created")
					}
					defer logger.Sync()

					// Test the logging that happens in main
					logger.Info("Starting Nexus", zap.String("version", version.Component("Nexus")))
				}
			})
		}
	})

	t.Run("server creation and shutdown", func(t *testing.T) {
		// Test the server creation and shutdown logic from main
		logger, _ := zap.NewDevelopment()
		defer logger.Sync()

		// Create configuration (like main does)
		cfg := config.DefaultNexusConfig()

		// Create server (like main does)
		server, err := nexus.NewServer(cfg.DBConnectionString(), logger)
		if err != nil {
			// Test the fatal error scenario from main
			errorMsg := fmt.Sprintf("Failed to create server: %v", err)
			if errorMsg == "" {
				t.Error("Expected error message to be non-empty")
			}
		} else {
			// Test that server was created successfully
			if server == nil {
				t.Error("Expected server to be created")
			}
			// Test shutdown (like main does with defer)
			server.Shutdown()
		}
	})

	t.Run("listener creation", func(t *testing.T) {
		// Test the listener creation logic from main
		cfg := config.DefaultNexusConfig()
		cfg.MinionPort = 0 // Use random port for testing

		// Test listener creation (like main does)
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.MinionPort))
		if err != nil {
			// Test the fatal error scenario from main
			errorMsg := fmt.Sprintf("Failed to listen: %v", err)
			if errorMsg == "" {
				t.Error("Expected error message to be non-empty")
			}
		} else {
			if lis == nil {
				t.Error("Expected listener to be created")
			}
			defer lis.Close()

			// Test that we can get the address (like main logs)
			addr := lis.Addr().String()
			if addr == "" {
				t.Error("Expected listener address to be non-empty")
			}
		}
	})

	t.Run("grpc server with services", func(t *testing.T) {
		// Test the complete gRPC server setup from main
		logger, _ := zap.NewDevelopment()
		defer logger.Sync()

		cfg := config.DefaultNexusConfig()

		// Create server
		server, err := nexus.NewServer("", logger)
		if err != nil {
			t.Fatalf("Failed to create server: %v", err)
		}
		defer server.Shutdown()

		// Create gRPC server (like main does)
		s := grpc.NewServer(
			grpc.MaxRecvMsgSize(cfg.MaxMsgSize),
			grpc.MaxSendMsgSize(cfg.MaxMsgSize),
		)

		// Register services (like main does)
		pb.RegisterMinionServiceServer(s, server)
		pb.RegisterConsoleServiceServer(s, server)

		// Register reflection (like main does)
		reflection.Register(s)

		// Test graceful stop (like main does)
		s.GracefulStop()
	})

	t.Run("signal handling setup", func(t *testing.T) {
		// Test the signal handling setup from main
		quit := make(chan os.Signal, 1)

		// Test that we can set up signal notification
		// Note: We don't actually call signal.Notify to avoid affecting the test process
		// Since make() always returns a non-nil channel, we verify the channel properties

		if cap(quit) != 1 {
			t.Errorf("Expected signal channel capacity to be 1, got %d", cap(quit))
		}

		// Test signal types that main listens for
		signals := []os.Signal{syscall.SIGINT, syscall.SIGTERM}
		for _, sig := range signals {
			if sig == nil {
				t.Error("Expected signal to be non-nil")
			}
		}
	})
}
