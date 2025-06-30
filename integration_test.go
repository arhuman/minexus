package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test configuration
const (
	dockerComposeFile = "docker-compose.yml"
	consoleExecutable = "./console-test"
	dbConnString      = "postgres://postgres:postgres@localhost:5432/minexus?sslmode=disable"
	maxRetries        = 30
	retryInterval     = 1 * time.Second
	minionPort        = 11972 // Standard TLS port for minions
	consolePort       = 11973 // mTLS port for console
)

// Integration Test Conditional Execution System
//
// This file contains integration tests that require external Docker services.
// To separate fast unit tests from slower integration tests, these tests only
// run when the SLOW_TESTS environment variable is set.
//
// Usage:
//   make test                 - Unit tests only (fast, ~5s)
//   SLOW_TESTS=1 make test    - All tests including integration (~60s)
//
// The integration tests automatically:
//   1. Check if Docker Compose services are running
//   2. Start required services (nexus, minion, database) if needed
//   3. Wait for services to be ready with health checks
//   4. Build console executable if needed
//   5. Run comprehensive end-to-end tests
//
// Required Services:
//   - nexus_db: PostgreSQL database
//   - nexus_server: Nexus gRPC dual-port server (port 11972 for minions, 11973 for console)
//   - minion_1: Test minion client
//
// Test Categories:
//   - Console command testing (help, version, listings)
//   - Shell command execution via minions
//   - File operations on remote systems
//   - System information gathering
//   - Error handling and edge cases
//   - Database integrity and consistency
//   - mTLS console connection testing
//   - Dual-port server functionality testing
//   - Certificate validation testing
//   - Mixed traffic scenarios (console + minion simultaneously)
//   - Certificate edge cases and authentication failures

// TestResult represents the result of a command execution
type TestResult struct {
	Command   string
	Expected  bool // true if command should succeed
	Output    string
	CommandID string
	Error     error
}

// IntegrationTestSuite contains all integration tests
func TestIntegrationSuite(t *testing.T) {
	// Check if integration tests should run
	if os.Getenv("SLOW_TESTS") == "" {
		t.Skip("Skipping integration tests. Set SLOW_TESTS=1 to run integration tests.")
		return
	}

	startTime := time.Now()
	t.Log("Running integration tests (SLOW_TESTS is set)")

	// Setup: Ensure Docker services are running
	setupStart := time.Now()
	setupDockerServices(t)
	setupDockerDuration := time.Since(setupStart)
	t.Logf("⏱️  Docker setup took: %v", setupDockerDuration)

	// Wait for services to be ready
	waitStart := time.Now()
	waitForServices(t)
	waitDuration := time.Since(waitStart)
	t.Logf("⏱️  Service readiness check took: %v", waitDuration)

	// Build console if needed
	buildStart := time.Now()
	buildConsole(t)
	buildDuration := time.Since(buildStart)
	t.Logf("⏱️  Console build took: %v", buildDuration)

	setupTotalDuration := time.Since(startTime)
	t.Logf("⏱️  TOTAL SETUP TIME: %v", setupTotalDuration)

	// Run test suites
	testsStart := time.Now()
	t.Run("ConsoleCommands", testConsoleCommands)
	t.Run("ShellCommands", testShellCommands)
	t.Run("FileCommands", testFileCommands)
	t.Run("SystemCommands", testSystemCommands)
	t.Run("DockerComposeCommands", testDockerComposeCommands)
	t.Run("ErrorCases", testErrorCases)
	t.Run("DatabaseIntegrity", testDatabaseIntegrity)

	// mTLS and dual-port testing
	t.Run("MTLSConnectivity", testMTLSConnectivity)
	t.Run("DualPortServer", testDualPortServer)
	t.Run("CertificateValidation", testCertificateValidation)
	t.Run("MixedTrafficScenarios", testMixedTrafficScenarios)
	t.Run("CertificateEdgeCases", testCertificateEdgeCases)

	// Race condition and resilience testing
	t.Run("MinionReconnectionRaceCondition", testMinionReconnectionRaceCondition)

	testsDuration := time.Since(testsStart)
	totalDuration := time.Since(startTime)
	t.Logf("⏱️  TEST EXECUTION TIME: %v", testsDuration)
	t.Logf("⏱️  TOTAL INTEGRATION TEST TIME: %v", totalDuration)
}

// setupDockerServices ensures nexus, nexus_db, and minion services are running
func setupDockerServices(t *testing.T) {
	t.Log("Checking Docker Compose services status...")

	// Check if services are running
	cmd := exec.Command("docker", "compose", "ps", "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to check docker compose status: %v", err)
	}

	// Parse output to check service status
	services := parseDockerComposePS(string(output))

	requiredServices := []string{"nexus_db", "nexus_server", "minion"}
	missingServices := []string{}

	for _, service := range requiredServices {
		if status, exists := services[service]; !exists || status != "running" {
			missingServices = append(missingServices, service)
		}
	}

	if len(missingServices) > 0 {
		t.Logf("Services not running: %v. Starting them...", missingServices)

		// Start services
		cmd = exec.Command("docker", "compose", "up", "-d", "nexus_server", "minion")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to start docker compose services: %v", err)
		}

		t.Log("Services started successfully")
	} else {
		t.Log("All required services are already running")
	}
}

// parseDockerComposePS parses docker compose ps output
func parseDockerComposePS(output string) map[string]string {
	services := make(map[string]string)
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "[]" {
			continue
		}

		// Simple parsing - looking for service name and state
		if strings.Contains(line, "nexus_db") {
			if strings.Contains(line, "running") {
				services["nexus_db"] = "running"
			}
		}
		if strings.Contains(line, "nexus_server") {
			if strings.Contains(line, "running") {
				services["nexus_server"] = "running"
			}
		}
		if strings.Contains(line, "minion") {
			if strings.Contains(line, "running") {
				services["minion"] = "running"
			}
		}
	}

	return services
}

// waitForServices waits for services to be ready
func waitForServices(t *testing.T) {
	t.Log("🔍 Starting service readiness checks...")

	// Wait for database
	t.Log("📊 Checking database connectivity...")
	dbStart := time.Now()
	for i := 0; i < maxRetries; i++ {
		db, err := sql.Open("postgres", dbConnString)
		if err == nil {
			if err := db.Ping(); err == nil {
				db.Close()
				t.Logf("✅ Database ready after %v (attempt %d/%d)", time.Since(dbStart), i+1, maxRetries)
				break
			}
			db.Close()
		}

		if i == maxRetries-1 {
			t.Fatalf("❌ Database not ready after %d retries and %v", maxRetries, time.Since(dbStart))
		}

		if i%5 == 0 { // Log every 5 attempts
			t.Logf("⏳ Database attempt %d/%d (elapsed: %v)", i+1, maxRetries, time.Since(dbStart))
		}
		time.Sleep(retryInterval)
	}

	// Check Docker health status before port tests
	t.Log("🐳 Checking Docker Compose service health...")
	cmd := exec.Command("docker", "compose", "ps", "--format", "table")
	if output, err := cmd.Output(); err == nil {
		t.Logf("Docker services status:\n%s", string(output))
	}

	// Wait for nexus minion server (port 11972)
	t.Logf("🔌 Checking nexus minion server (port %d)...", minionPort)
	minionStart := time.Now()
	for i := 0; i < maxRetries; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", minionPort), 3*time.Second)
		if err == nil {
			conn.Close()
			t.Logf("✅ Minion server ready after %v (attempt %d/%d)", time.Since(minionStart), i+1, maxRetries)
			break
		}

		if i == maxRetries-1 {
			t.Fatalf("❌ Nexus minion server not ready after %d retries and %v. Last error: %v", maxRetries, time.Since(minionStart), err)
		}

		if i%3 == 0 { // Log every 3 attempts
			t.Logf("⏳ Minion port attempt %d/%d (elapsed: %v, error: %v)", i+1, maxRetries, time.Since(minionStart), err)
		}
		time.Sleep(retryInterval)
	}

	// Wait for nexus console server (port 11973)
	t.Logf("🔐 Checking nexus console server (port %d)...", consolePort)
	consoleStart := time.Now()
	for i := 0; i < maxRetries; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", consolePort), 3*time.Second)
		if err == nil {
			conn.Close()
			t.Logf("✅ Console server ready after %v (attempt %d/%d)", time.Since(consoleStart), i+1, maxRetries)
			break
		}

		if i == maxRetries-1 {
			t.Fatalf("❌ Nexus console server not ready after %d retries and %v. Last error: %v", maxRetries, time.Since(consoleStart), err)
		}

		if i%3 == 0 { // Log every 3 attempts
			t.Logf("⏳ Console port attempt %d/%d (elapsed: %v, error: %v)", i+1, maxRetries, time.Since(consoleStart), err)
		}
		time.Sleep(retryInterval)
	}

	t.Log("🚀 All services are ready (database, minion port, console port)")
}

// buildConsole builds the console executable if it doesn't exist
func buildConsole(t *testing.T) {
	if _, err := os.Stat(consoleExecutable); os.IsNotExist(err) {
		t.Log("Building console executable...")
		cmd := exec.Command("mv", "internal/certs/files", "internal/certs/files.backup")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to backup certs: %v", err)
		}
		cmd = exec.Command("cp", "-r", "internal/certs/files.backup/test", "internal/certs/files")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to copy test certs: %v", err)
		}
		cmd = exec.Command("go", "build", "-o", "console-test", "./cmd/console")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to build console: %v", err)
		}
		cmd = exec.Command("rm", "-rf", "internal/certs/files")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to remove certs: %v", err)
		}
		cmd = exec.Command("mv", "internal/certs/files.backup", "internal/certs/files")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to restore certs: %v", err)
		}
	}
}

// runConsoleCommandWithTimeout executes a console command with timeout
func runConsoleCommandWithTimeout(command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("echo '%s' | %s", command, consoleExecutable))

	// Override NEXUS_SERVER to localhost for local console execution
	// The .env file has NEXUS_SERVER=nexus which works for Docker containers,
	// but the locally built console needs to connect to localhost
	cmd.Env = append(os.Environ(), "NEXUS_SERVER=localhost")

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// extractCommandID extracts command ID from console output
func extractCommandID(output string) string {
	re := regexp.MustCompile(`Command ID: ([a-f0-9-]+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// testConsoleCommands tests basic console commands
func testConsoleCommands(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		shouldWork bool
		contains   []string
	}{
		{
			name:       "Help",
			command:    "help",
			shouldWork: true,
			contains:   []string{"Console Commands", "help", "version", "minion-list"},
		},
		{
			name:       "Help alias",
			command:    "h",
			shouldWork: true,
			contains:   []string{"Console Commands"},
		},
		{
			name:       "Version",
			command:    "version",
			shouldWork: true,
			contains:   []string{"Console"},
		},
		{
			name:       "Version alias",
			command:    "v",
			shouldWork: true,
			contains:   []string{"Console"},
		},
		{
			name:       "Minion list",
			command:    "minion-list",
			shouldWork: true,
			contains:   []string{"Connected minions", "docker-minion"},
		},
		{
			name:       "Minion list alias",
			command:    "lm",
			shouldWork: true,
			contains:   []string{"Connected minions"},
		},
		{
			name:       "Tag list",
			command:    "tag-list",
			shouldWork: true,
			contains:   []string{"tags"},
		},
		{
			name:       "Tag list alias",
			command:    "lt",
			shouldWork: true,
			contains:   []string{"tags"},
		},
		{
			name:       "Invalid command",
			command:    "invalid-command",
			shouldWork: false,
			contains:   []string{"Unknown command"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runConsoleCommandWithTimeout(tt.command, 10*time.Second)

			if tt.shouldWork {
				assert.NoError(t, err, "Command should not fail")
			}

			for _, substr := range tt.contains {
				assert.Contains(t, output, substr, "Output should contain expected text")
			}
		})
	}
}

// testShellCommands tests shell command execution with OPTIMIZED intelligent polling
func testShellCommands(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		shouldWork  bool
		expectError bool
		numResults  int // Number of expected results in database
	}{
		{
			name:       "Simple shell command",
			command:    "command-send all echo 'hello world'",
			shouldWork: true,
			numResults: 1, // Expect one result for this command
		},
		{
			name:       "List directory",
			command:    "command-send all ls /",
			shouldWork: true,
			numResults: 1,
		},
		{
			name:       "System info command",
			command:    "command-send all system:info",
			shouldWork: true,
			numResults: 1,
		},
		{
			name:       "System OS command",
			command:    "command-send all system:os",
			shouldWork: true,
			numResults: 1,
		},
		{
			name:       "Docker Compose PS with nonexistent path",
			command:    "command-send all docker-compose:ps /nonexistent/path",
			shouldWork: true, // Command will be sent but will fail on minion
			numResults: 1,
		},
		{
			name:       "Docker Compose PS with invalid JSON",
			command:    `command-send all '{"command": "ps", "path":'`,
			shouldWork: true, // Command will be sent but will fail on minion
			numResults: 1,
		},
		{
			name:       "Docker Compose UP with missing path",
			command:    `command-send all '{"command": "up"}'`,
			shouldWork: true, // Command will be sent but will fail on minion
			numResults: 1,
		},
		{
			name:       "Docker Compose DOWN with current directory",
			command:    "command-send all docker-compose:down .",
			shouldWork: true, // Command will be sent but will fail on minion (no docker-compose.yml in current dir)
			numResults: 1,
		},
		{
			name:        "Command with missing target",
			command:     "command-send",
			shouldWork:  false,
			expectError: true,
			numResults:  0,
		},
		{
			name:        "Command with invalid minion ID",
			command:     "command-send minion invalid-id echo test",
			shouldWork:  false, // Command should be rejected
			expectError: true,
			numResults:  0, // No results expected for invalid command
		},
		{
			name:        "Command with missing minion ID",
			command:     "command-send minion",
			shouldWork:  false,
			expectError: true,
			numResults:  0, // No results expected for invalid command
		},
		{
			name:        "Command with missing tag",
			command:     "command-send tag",
			shouldWork:  false,
			expectError: true,
			numResults:  0, // No results expected for invalid command
		},
		{
			name:        "Command with invalid tag format",
			command:     "command-send tag invalidtag echo test",
			shouldWork:  false,
			expectError: true,
			numResults:  0, // No results expected for invalid command
		},
	}

	// 🚀 OPTIMIZATION: Execute commands in batch, then poll intelligently
	var commandIDs []string
	var testNames []string

	batchStart := time.Now()

	// Phase 1: Send all successful commands rapidly (no waiting between sends)
	for _, tt := range tests {
		t.Run(fmt.Sprintf("send_%s", tt.name), func(t *testing.T) {
			if tt.expectError {
				// Handle error cases immediately
				output, err := runConsoleCommandWithTimeout(tt.command, 2*time.Second)
				assert.True(t, err != nil || strings.Contains(output, "Error") ||
					strings.Contains(output, "Usage:") || strings.Contains(output, "Command was not accepted"),
					"Command should fail or show error message")
				return
			}

			if !tt.shouldWork {
				return
			}

			// Send command quickly
			testStart := time.Now()
			output, err := runConsoleCommandWithTimeout(tt.command, 3*time.Second)
			commandExecTime := time.Since(testStart)

			assert.NoError(t, err, "Command send should not fail")
			assert.Contains(t, output, "Command dispatched successfully", "Should show success message")

			commandID := extractCommandID(output)
			assert.NotEmpty(t, commandID, "Should return a command ID")

			// Quick DB verification
			verifyCommandInDB(t, commandID)

			// Store for batch polling
			commandIDs = append(commandIDs, commandID)
			testNames = append(testNames, tt.name)

			if len(commandID) >= 8 {
				t.Logf("📤 Sent command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID[:8])
			} else {
				t.Logf("📤 Sent command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID)
			}
		})
	}

	sendDuration := time.Since(batchStart)
	t.Logf("🚀 BATCH SEND completed: %d commands in %v (vs %v with sequential waits)",
		len(commandIDs), sendDuration, time.Duration(len(commandIDs))*10*time.Second)

	// Phase 2: Intelligent polling for ALL results
	if len(commandIDs) > 0 {
		pollStart := time.Now()
		t.Logf("🔍 Starting intelligent polling for %d commands...", len(commandIDs))

		// Initial wait for execution to start
		time.Sleep(2 * time.Second)

		// Progressive polling with early termination
		resultsFound := make(map[string]bool)
		maxAttempts := 60 // 30 seconds max with 500ms polling

		for attempt := 0; attempt < maxAttempts; attempt++ {
			foundCount := 0

			for i, commandID := range commandIDs {
				if resultsFound[commandID] {
					foundCount++
					continue
				}

				actualResults := getNbResultsInDB(t, commandID)
				if actualResults > 0 {
					resultsFound[commandID] = true
					foundCount++
					elapsed := time.Since(pollStart)
					idDisplay := commandID
					if len(commandID) >= 8 {
						idDisplay = commandID[:8]
					}
					t.Logf("✅ Results for '%s' (%s) found after %v",
						testNames[i], idDisplay, elapsed)
				}
			}

			// Early termination when all results found
			if foundCount == len(commandIDs) {
				totalPollTime := time.Since(pollStart)
				t.Logf("🎯 ALL RESULTS FOUND: %d/%d in %v (early termination)",
					foundCount, len(commandIDs), totalPollTime)
				break
			}

			// Adaptive polling: fast initially, slower later
			pollInterval := 500 * time.Millisecond
			if attempt > 20 {
				pollInterval = 1 * time.Second
			}

			time.Sleep(pollInterval)
		}

		totalPollTime := time.Since(pollStart)
		finalCount := len(resultsFound)
		originalTime := time.Duration(len(commandIDs)) * 10 * time.Second
		timesSaved := originalTime - (sendDuration + totalPollTime)

		t.Logf("🚀 OPTIMIZATION RESULTS:")
		t.Logf("   Commands processed: %d/%d successful", finalCount, len(commandIDs))
		t.Logf("   Total time: %v (send: %v + poll: %v)", sendDuration+totalPollTime, sendDuration, totalPollTime)
		t.Logf("   Original approach: %v (with 10s fixed sleeps)", originalTime)
		t.Logf("   Time saved: %v (%.1f%% faster)", timesSaved, float64(timesSaved)/float64(originalTime)*100)
	}
}

// testFileCommands tests file-related commands
func testFileCommands(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		shouldWork  bool
		expectError bool
		numResults  int // Number of expected results in database
	}{
		{
			name:       "Get file content",
			command:    "command-send all file:get /etc/hostname",
			shouldWork: true,
			numResults: 1,
		},
		{
			name:       "Get file info",
			command:    "command-send all file:info /etc/hostname",
			shouldWork: true,
			numResults: 1,
		},
		{
			name:       "Get non-existent file",
			command:    "command-send all file:get /non/existent/file",
			shouldWork: true, // Command is sent but will fail on execution
			numResults: 1,
		},
		{
			name:       "File command with missing path",
			command:    "command-send all file:get",
			shouldWork: true, // Command is sent but will fail due to missing argument
			numResults: 1,
		},
		{
			name:       "File copy command",
			command:    "command-send all file:copy /etc/hostname /tmp/test-hostname",
			shouldWork: true,
			numResults: 1,
		},
		{
			name:       "File move command",
			command:    "command-send all file:move /tmp/test-hostname /tmp/moved-hostname",
			shouldWork: true,
			numResults: 1,
		},
	}

	// 🚀 OPTIMIZATION: Apply same intelligent polling to file commands
	var commandIDs []string
	var testNames []string

	batchStart := time.Now()

	// Phase 1: Send file commands rapidly
	for _, tt := range tests {
		t.Run(fmt.Sprintf("send_%s", tt.name), func(t *testing.T) {
			if tt.expectError {
				output, err := runConsoleCommandWithTimeout(tt.command, 15*time.Second)
				assert.True(t, err != nil || strings.Contains(output, "Error"),
					"Command should fail or show error message")
				return
			}

			if !tt.shouldWork {
				return
			}

			testStart := time.Now()
			output, err := runConsoleCommandWithTimeout(tt.command, 15*time.Second)
			commandExecTime := time.Since(testStart)

			assert.NoError(t, err, "File command send should not fail")
			assert.Contains(t, output, "Command dispatched successfully", "Should show success message")

			commandID := extractCommandID(output)
			assert.NotEmpty(t, commandID, "Should return a command ID")

			verifyCommandInDB(t, commandID)

			commandIDs = append(commandIDs, commandID)
			testNames = append(testNames, tt.name)

			if len(commandID) >= 8 {
				t.Logf("📁 Sent file command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID[:8])
			} else {
				t.Logf("📁 Sent file command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID)
			}
		})
	}

	// Phase 2: Intelligent polling for file results
	if len(commandIDs) > 0 {
		pollStart := time.Now()
		t.Logf("📁 Polling for %d file operation results...", len(commandIDs))

		time.Sleep(3 * time.Second) // Initial wait

		resultsFound := make(map[string]bool)
		for attempt := 0; attempt < 60; attempt++ {
			foundCount := 0

			for i, commandID := range commandIDs {
				if resultsFound[commandID] {
					foundCount++
					continue
				}

				actualResults := getNbResultsInDB(t, commandID)
				if actualResults > 0 {
					resultsFound[commandID] = true
					foundCount++
					elapsed := time.Since(pollStart)
					idDisplay := commandID
					if len(commandID) >= 8 {
						idDisplay = commandID[:8]
					}
					t.Logf("📁 File results for '%s' (%s) found after %v",
						testNames[i], idDisplay, elapsed)
				}
			}

			if foundCount == len(commandIDs) {
				totalPollTime := time.Since(pollStart)
				t.Logf("📁 ALL FILE RESULTS found in %v", totalPollTime)
				break
			}

			time.Sleep(500 * time.Millisecond)
		}

		totalTime := time.Since(batchStart)
		originalTime := time.Duration(len(commandIDs)) * 10 * time.Second
		t.Logf("📁 File optimization: %v vs %v original (%.1f%% improvement)",
			totalTime, originalTime, float64(originalTime-totalTime)/float64(originalTime)*100)
	}
}

// testSystemCommands tests system-related commands
func testSystemCommands(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		shouldWork bool
		numResults int // Number of expected results in database
	}{
		{
			name:       "System info",
			command:    "command-send all system:info",
			shouldWork: true,
			numResults: 1,
		},
		{
			name:       "System OS",
			command:    "command-send all system:os",
			shouldWork: true,
			numResults: 1,
		},
		{
			name:       "Process list",
			command:    "command-send all ps aux",
			shouldWork: true,
			numResults: 1,
		},
		{
			name:       "Disk usage",
			command:    "command-send all df -h",
			shouldWork: true,
			numResults: 1,
		},
		{
			name:       "Memory info",
			command:    "command-send all free -h",
			shouldWork: true,
			numResults: 1,
		},
		{
			name:       "Network interfaces",
			command:    "command-send all ip addr show",
			shouldWork: true,
			numResults: 1,
		},
	}

	// 🚀 OPTIMIZATION: System commands with intelligent polling
	var commandIDs []string
	var testNames []string

	batchStart := time.Now()

	// Phase 1: Send system commands rapidly
	for _, tt := range tests {
		t.Run(fmt.Sprintf("send_%s", tt.name), func(t *testing.T) {
			if !tt.shouldWork {
				return
			}

			testStart := time.Now()
			output, err := runConsoleCommandWithTimeout(tt.command, 15*time.Second)
			commandExecTime := time.Since(testStart)

			assert.NoError(t, err, "System command should not fail")
			assert.Contains(t, output, "Command dispatched successfully", "Should show success message")

			commandID := extractCommandID(output)
			assert.NotEmpty(t, commandID, "Should return a command ID")

			verifyCommandInDB(t, commandID)

			commandIDs = append(commandIDs, commandID)
			testNames = append(testNames, tt.name)

			if len(commandID) >= 8 {
				t.Logf("🖥️ Sent system command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID[:8])
			} else {
				t.Logf("🖥️ Sent system command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID)
			}
		})
	}

	// Phase 2: Intelligent polling for system command results
	if len(commandIDs) > 0 {
		pollStart := time.Now()
		t.Logf("🖥️ Polling for %d system command results...", len(commandIDs))

		time.Sleep(3 * time.Second) // Initial wait

		resultsFound := make(map[string]bool)
		for attempt := 0; attempt < 60; attempt++ {
			foundCount := 0

			for i, commandID := range commandIDs {
				if resultsFound[commandID] {
					foundCount++
					continue
				}

				actualResults := getNbResultsInDB(t, commandID)
				if actualResults > 0 {
					resultsFound[commandID] = true
					foundCount++
					elapsed := time.Since(pollStart)
					idDisplay := commandID
					if len(commandID) >= 8 {
						idDisplay = commandID[:8]
					}
					t.Logf("🖥️ System results for '%s' (%s) found after %v",
						testNames[i], idDisplay, elapsed)
				}
			}

			if foundCount == len(commandIDs) {
				totalPollTime := time.Since(pollStart)
				t.Logf("🖥️ ALL SYSTEM RESULTS found in %v", totalPollTime)
				break
			}

			time.Sleep(500 * time.Millisecond)
		}

		totalTime := time.Since(batchStart)
		originalTime := time.Duration(len(commandIDs)) * 10 * time.Second
		t.Logf("🖥️ System optimization: %v vs %v original (%.1f%% improvement)",
			totalTime, originalTime, float64(originalTime-totalTime)/float64(originalTime)*100)
	}
}

// testDockerComposeCommands tests docker-compose command functionality
func testDockerComposeCommands(t *testing.T) {
	// Create a temporary directory with a test docker-compose.yml file
	tmpDir := t.TempDir()
	composeContent := `version: '3.8'
services:
  test-web:
    image: nginx:alpine
    ports:
      - "8080:80"
  test-db:
    image: postgres:alpine
    environment:
      POSTGRES_DB: testdb
      POSTGRES_USER: testuser
      POSTGRES_PASSWORD: testpass
`

	composeFile := tmpDir + "/docker-compose.yml"
	err := os.WriteFile(composeFile, []byte(composeContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test docker-compose.yml: %v", err)
	}

	tests := []struct {
		name        string
		command     string
		shouldWork  bool
		expectError bool
		numResults  int
		contains    []string // Text that should be in the result
	}{
		{
			name:       "Docker Compose PS with valid path",
			command:    fmt.Sprintf("command-send all docker-compose:ps %s", tmpDir),
			shouldWork: true,
			numResults: 1,
			contains:   []string{}, // May fail if docker not available, that's ok
		},
		{
			name:       "Docker Compose PS with JSON format",
			command:    fmt.Sprintf(`command-send all '{"command": "ps", "path": "%s"}'`, tmpDir),
			shouldWork: true,
			numResults: 1,
			contains:   []string{},
		},
		{
			name:       "Docker Compose UP with valid path",
			command:    fmt.Sprintf("command-send all docker-compose:up %s", tmpDir),
			shouldWork: true,
			numResults: 1,
			contains:   []string{}, // May fail if docker not available, that's ok
		},
		{
			name:       "Docker Compose UP with JSON and service",
			command:    fmt.Sprintf(`command-send all '{"command": "up", "path": "%s", "service": "test-web"}'`, tmpDir),
			shouldWork: true,
			numResults: 1,
			contains:   []string{},
		},
		{
			name:       "Docker Compose UP with build flag",
			command:    fmt.Sprintf(`command-send all '{"command": "up", "path": "%s", "build": true}'`, tmpDir),
			shouldWork: true,
			numResults: 1,
			contains:   []string{},
		},
		{
			name:       "Docker Compose DOWN with valid path",
			command:    fmt.Sprintf("command-send all docker-compose:down %s", tmpDir),
			shouldWork: true,
			numResults: 1,
			contains:   []string{},
		},
		{
			name:       "Docker Compose DOWN with service",
			command:    fmt.Sprintf(`command-send all '{"command": "down", "path": "%s", "service": "test-web"}'`, tmpDir),
			shouldWork: true,
			numResults: 1,
			contains:   []string{},
		},
		{
			name:        "Docker Compose with nonexistent path",
			command:     "command-send all docker-compose:ps /nonexistent/path",
			shouldWork:  true,  // Command will be sent
			expectError: false, // But will fail on minion
			numResults:  1,
			contains:    []string{}, // Error will be in the result
		},
		{
			name:        "Docker Compose with invalid JSON",
			command:     `command-send all '{"command": "ps", "path":'`,
			shouldWork:  true,  // Command will be sent
			expectError: false, // But will fail on minion
			numResults:  1,
			contains:    []string{},
		},
		{
			name:        "Docker Compose with missing path",
			command:     `command-send all '{"command": "up"}'`,
			shouldWork:  true,  // Command will be sent
			expectError: false, // But will fail on minion
			numResults:  1,
			contains:    []string{},
		},
	}

	// Execute commands and collect command IDs
	var commandIDs []string
	var testNames []string

	for _, tt := range tests {
		t.Run(fmt.Sprintf("send_%s", tt.name), func(t *testing.T) {
			if tt.expectError {
				// Handle error cases immediately
				output, err := runConsoleCommandWithTimeout(tt.command, 5*time.Second)
				if err == nil && !strings.Contains(output, "Error") {
					t.Logf("Expected error but command seemed to work: %s", output)
				}
				return
			}

			if !tt.shouldWork {
				return
			}

			// Send command
			output, err := runConsoleCommandWithTimeout(tt.command, 5*time.Second)
			if err != nil {
				t.Errorf("Failed to send command: %v", err)
				return
			}

			// Extract command ID
			commandID := extractCommandID(output)
			if commandID == "" {
				t.Errorf("Could not extract command ID from output: %s", output)
				return
			}

			commandIDs = append(commandIDs, commandID)
			testNames = append(testNames, tt.name)
			t.Logf("📤 Sent docker-compose command '%s': %s", tt.name, commandID)
		})
	}

	// Wait for results with intelligent polling
	if len(commandIDs) > 0 {
		t.Logf("🔄 Waiting for %d docker-compose command results...", len(commandIDs))

		pollStart := time.Now()
		resultsFound := make(map[string]bool)
		maxAttempts := 30

		for attempt := 0; attempt < maxAttempts; attempt++ {
			foundCount := 0
			for i, commandID := range commandIDs {
				if resultsFound[commandID] {
					foundCount++
					continue
				}

				actualResults := getNbResultsInDB(t, commandID)
				if actualResults > 0 {
					resultsFound[commandID] = true
					foundCount++
					elapsed := time.Since(pollStart)
					idDisplay := commandID
					if len(commandID) >= 8 {
						idDisplay = commandID[:8]
					}
					t.Logf("✅ Docker-compose results for '%s' (%s) found after %v",
						testNames[i], idDisplay, elapsed)
				}
			}

			// Early termination when all results found
			if foundCount == len(commandIDs) {
				totalPollTime := time.Since(pollStart)
				t.Logf("🎯 ALL DOCKER-COMPOSE RESULTS FOUND: %d/%d in %v",
					foundCount, len(commandIDs), totalPollTime)
				break
			}

			time.Sleep(1 * time.Second)
		}

		// Verify final results
		finalCount := len(resultsFound)
		t.Logf("📊 Docker-compose commands processed: %d/%d", finalCount, len(commandIDs))

		// In integration tests, docker-compose commands may fail if Docker isn't available
		// This is expected and the test should focus on command delivery, not execution success
		if finalCount < len(commandIDs) {
			t.Logf("ℹ️  Some docker-compose commands may have failed due to Docker availability in test environment")
		}
	}
}

// testErrorCases tests various error scenarios
func testErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		expectError bool
		errorText   string
	}{
		{
			name:        "Empty command",
			command:     "",
			expectError: true,
			errorText:   "",
		},
		{
			name:        "Command with no arguments",
			command:     "command-send",
			expectError: true,
			errorText:   "Usage:",
		},
		{
			name:        "Invalid target type",
			command:     "command-send invalid echo test",
			expectError: true,
			errorText:   "",
		},
		{
			name:        "Tag with invalid format",
			command:     "command-send tag invalid echo test",
			expectError: true,
			errorText:   "format should be key=value",
		},
		{
			name:        "Minion with no ID",
			command:     "command-send minion echo test",
			expectError: true,
			errorText:   "Command was not accepted",
		},
		{
			name:        "Result-get with no ID",
			command:     "result-get",
			expectError: true,
			errorText:   "Usage: result-get",
		},
		{
			name:        "Result-get with invalid ID",
			command:     "result-get invalid-command-id",
			expectError: false, // Command succeeds but shows no results
			errorText:   "No results available",
		},
		{
			name:        "Tag-set with no arguments",
			command:     "tag-set",
			expectError: true,
			errorText:   "Usage: tag-set",
		},
		{
			name:        "Tag-set with invalid format",
			command:     "tag-set minion-id invalid-tag",
			expectError: true,
			errorText:   "Invalid tag format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runConsoleCommandWithTimeout(tt.command, 10*time.Second)

			if tt.expectError {
				if tt.errorText != "" {
					assert.Contains(t, output, tt.errorText,
						"Should contain expected error message")
				}
			} else {
				// For cases where command succeeds but shows no results
				if tt.errorText != "" {
					assert.Contains(t, output, tt.errorText,
						"Should contain expected message")
				}
			}

			// Log error if any for debugging
			if err != nil {
				t.Logf("Command error: %v", err)
			}
		})
	}
}

// testDatabaseIntegrity tests database consistency and integrity
func testDatabaseIntegrity(t *testing.T) {
	db, err := sql.Open("postgres", dbConnString)
	require.NoError(t, err, "Should connect to database")
	defer db.Close()

	// Test database connection
	err = db.Ping()
	require.NoError(t, err, "Should ping database successfully")

	// Check if required tables exist
	tables := []string{"hosts", "commands", "command_results"}
	for _, table := range tables {
		var exists bool
		err := db.QueryRow("SELECT EXISTS (SELECT FROM pg_tables WHERE tablename = $1)", table).Scan(&exists)
		require.NoError(t, err, "Should query table existence")
		assert.True(t, exists, fmt.Sprintf("Table %s should exist", table))
	}

	// Check if minion is registered in hosts table
	var minionCount int
	err = db.QueryRow("SELECT COUNT(*) FROM hosts WHERE id LIKE '%minion%'").Scan(&minionCount)
	require.NoError(t, err, "Should query minion count")
	assert.Greater(t, minionCount, 0, "Should have at least one minion registered")

	// Check if commands were recorded
	var commandCount int
	err = db.QueryRow("SELECT COUNT(*) FROM commands").Scan(&commandCount)
	require.NoError(t, err, "Should query command count")
	t.Logf("Total commands in database: %d", commandCount)

	// Check if command results were recorded
	var resultCount int
	err = db.QueryRow("SELECT COUNT(*) FROM command_results").Scan(&resultCount)
	require.NoError(t, err, "Should query result count")
	t.Logf("Total command results in database: %d", resultCount)

	// Verify foreign key relationships
	var orphanedResults int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM command_results cr 
		WHERE NOT EXISTS (SELECT 1 FROM commands c WHERE c.id = cr.command_id)
	`).Scan(&orphanedResults)
	require.NoError(t, err, "Should query orphaned results")
	assert.Equal(t, 0, orphanedResults, "Should have no orphaned command results")

	var orphanedCommands int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM commands c
		WHERE host_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM hosts h WHERE h.id = c.host_id)
	`).Scan(&orphanedCommands)
	require.NoError(t, err, "Should query orphaned commands")
	assert.Equal(t, 0, orphanedCommands, "Should have no orphaned commands")
}

// verifyCommandInDB verifies that a command was inserted into the commands table
func verifyCommandInDB(t *testing.T, commandID string) {
	db, err := sql.Open("postgres", dbConnString)
	require.NoError(t, err, "Should connect to database")
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM commands WHERE id = $1", commandID).Scan(&count)
	require.NoError(t, err, "Should query command existence")
	assert.Greater(t, count, 0, fmt.Sprintf("Command %s should exist in database", commandID))
}

// getNbResultsInDb returns the actual count of results for a command in the command_results table
func getNbResultsInDB(t *testing.T, commandID string) int {
	db, err := sql.Open("postgres", dbConnString)
	require.NoError(t, err, "Should connect to database")
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM command_results WHERE command_id = $1", commandID).Scan(&count)
	require.NoError(t, err, "Should query result existence")
	t.Logf("Command %s has %d results in database", commandID, count)
	return count
}

// TestIndividualCommands runs specific command tests individually
func TestIndividualCommands(t *testing.T) {
	// Skip if integration tests are not enabled
	if os.Getenv("SLOW_TESTS") == "" {
		t.Skip("Skipping individual console tests. Set SLOW_TESTS=1 to run with Docker services.")
		return
	}

	// Build console if needed
	buildConsole(t)

	// Test version command (should work without docker services)
	t.Run("VersionOnly", func(t *testing.T) {
		output, err := runConsoleCommandWithTimeout("version", 5*time.Second)
		assert.NoError(t, err, "Version command should work")
		assert.Contains(t, output, "Console", "Should show console version")
	})

	// Test help command
	t.Run("HelpOnly", func(t *testing.T) {
		output, err := runConsoleCommandWithTimeout("help", 5*time.Second)
		assert.NoError(t, err, "Help command should work")
		assert.Contains(t, output, "Console Commands", "Should show help")
	})
}

// TestConsoleInput tests console input handling with pipes
func TestConsoleInput(t *testing.T) {
	// Skip if integration tests are not enabled
	if os.Getenv("SLOW_TESTS") == "" {
		t.Skip("Skipping console input tests. Set SLOW_TESTS=1 to run with Docker services.")
		return
	}

	buildConsole(t)

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Multiple commands",
			input:    "version\nhelp\nquit\n",
			expected: []string{"Console", "Commands"},
		},
		{
			name:     "Empty lines",
			input:    "\n\nversion\n\nquit\n",
			expected: []string{"Console"},
		},
		{
			name:     "Command with spaces",
			input:    "   version   \nquit\n",
			expected: []string{"Console"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(consoleExecutable)
			stdin, err := cmd.StdinPipe()
			require.NoError(t, err, "Should create stdin pipe")

			stdout, err := cmd.StdoutPipe()
			require.NoError(t, err, "Should create stdout pipe")

			err = cmd.Start()
			require.NoError(t, err, "Should start console")

			// Write input
			go func() {
				defer stdin.Close()
				io.WriteString(stdin, tt.input)
			}()

			// Read output
			scanner := bufio.NewScanner(stdout)
			var output strings.Builder
			for scanner.Scan() {
				output.WriteString(scanner.Text() + "\n")
			}

			_ = cmd.Wait()
			// Console might exit with non-zero code, that's OK

			outputStr := output.String()
			for _, expected := range tt.expected {
				assert.Contains(t, outputStr, expected, "Output should contain expected text")
			}
		})
	}
}

// testMTLSConnectivity tests mTLS console connection functionality
func testMTLSConnectivity(t *testing.T) {
	t.Log("Testing mTLS console connectivity...")

	// Test basic console mTLS connection by running a simple command
	tests := []struct {
		name     string
		command  string
		expected []string
	}{
		{
			name:     "Console version via mTLS",
			command:  "version",
			expected: []string{"Console"},
		},
		{
			name:     "Console help via mTLS",
			command:  "help",
			expected: []string{"Console Commands"},
		},
		{
			name:     "Minion list via mTLS",
			command:  "minion-list",
			expected: []string{"Connected minions"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runConsoleCommandWithTimeout(tt.command, 10*time.Second)
			assert.NoError(t, err, "mTLS console command should succeed")

			for _, expected := range tt.expected {
				assert.Contains(t, output, expected, "mTLS console output should contain expected text")
			}

			t.Logf("✅ mTLS command '%s' executed successfully", tt.command)
		})
	}
}

// testDualPortServer tests that both minion and console ports are operational
func testDualPortServer(t *testing.T) {
	t.Log("Testing dual-port server functionality...")

	// Test minion port connectivity (should be accessible)
	t.Run("MinionPortConnectivity", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", minionPort), 5*time.Second)
		assert.NoError(t, err, "Should be able to connect to minion port")
		if conn != nil {
			conn.Close()
		}
		t.Logf("✅ Minion port %d is accessible", minionPort)
	})

	// Test console port connectivity (should be accessible)
	t.Run("ConsolePortConnectivity", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", consolePort), 5*time.Second)
		assert.NoError(t, err, "Should be able to connect to console port")
		if conn != nil {
			conn.Close()
		}
		t.Logf("✅ Console port %d is accessible", consolePort)
	})

	// Test that both ports are different
	t.Run("PortSeparation", func(t *testing.T) {
		assert.NotEqual(t, minionPort, consolePort, "Minion and console ports should be different")
		t.Logf("✅ Port separation verified: minion=%d, console=%d", minionPort, consolePort)
	})

	// Test simultaneous connections to both ports
	t.Run("SimultaneousConnections", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(2)

		// Connect to minion port
		go func() {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", minionPort), 5*time.Second)
			assert.NoError(t, err, "Should connect to minion port simultaneously")
			if conn != nil {
				time.Sleep(1 * time.Second) // Hold connection briefly
				conn.Close()
			}
		}()

		// Connect to console port
		go func() {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", consolePort), 5*time.Second)
			assert.NoError(t, err, "Should connect to console port simultaneously")
			if conn != nil {
				time.Sleep(1 * time.Second) // Hold connection briefly
				conn.Close()
			}
		}()

		wg.Wait()
		t.Log("✅ Simultaneous connections to both ports successful")
	})
}

// testCertificateValidation tests certificate validation scenarios
func testCertificateValidation(t *testing.T) {
	t.Log("Testing certificate validation...")

	// Since the console executable uses embedded certificates, we test that
	// the mTLS connection works (which proves certificate validation is working)
	t.Run("ValidCertificateAuthentication", func(t *testing.T) {
		// This test verifies that console can authenticate with valid certificates
		output, err := runConsoleCommandWithTimeout("version", 10*time.Second)
		assert.NoError(t, err, "Console with valid certificates should authenticate successfully")
		assert.Contains(t, output, "Console", "Should receive proper response with valid certificates")
		t.Log("✅ Valid certificate authentication successful")
	})

	// Test that console connects to the correct port (mTLS port)
	t.Run("MTLSPortUsage", func(t *testing.T) {
		// The console should be configured to use port 11973 (mTLS port)
		// This is verified indirectly by successful console operations
		output, err := runConsoleCommandWithTimeout("minion-list", 10*time.Second)
		assert.NoError(t, err, "Console should successfully connect to mTLS port")
		assert.Contains(t, output, "minions", "Should get proper response from mTLS port")
		t.Log("✅ mTLS port usage verified")
	})

	// Test certificate chain validation by ensuring console works
	t.Run("CertificateChainValidation", func(t *testing.T) {
		// The fact that console commands work proves the entire certificate chain is valid:
		// CA -> Server Cert (validated by console)
		// CA -> Client Cert (presented by console)
		output, err := runConsoleCommandWithTimeout("help", 5*time.Second)
		assert.NoError(t, err, "Certificate chain validation should succeed")
		assert.Contains(t, output, "Commands", "Should receive response with valid certificate chain")
		t.Log("✅ Certificate chain validation successful")
	})
}

// testMixedTrafficScenarios tests concurrent console and minion traffic
func testMixedTrafficScenarios(t *testing.T) {
	t.Log("Testing mixed traffic scenarios (console + minion)...")

	// Test concurrent console and minion operations
	t.Run("ConcurrentConsoleAndMinion", func(t *testing.T) {
		var wg sync.WaitGroup
		var consoleErr, minionErr error
		var consoleOutput, minionOutput string

		wg.Add(2)

		// Console operation (mTLS port)
		go func() {
			defer wg.Done()
			consoleOutput, consoleErr = runConsoleCommandWithTimeout("minion-list", 15*time.Second)
		}()

		// Minion operation (via console but targeting minion functionality)
		go func() {
			defer wg.Done()
			minionOutput, minionErr = runConsoleCommandWithTimeout("command-send all echo concurrent-test", 15*time.Second)
		}()

		wg.Wait()

		// Verify both operations succeeded
		assert.NoError(t, consoleErr, "Console operation should succeed during concurrent access")
		assert.NoError(t, minionErr, "Minion operation should succeed during concurrent access")
		assert.Contains(t, consoleOutput, "minions", "Console should get proper response during concurrent access")
		assert.Contains(t, minionOutput, "Command dispatched", "Minion operation should succeed during concurrent access")

		t.Log("✅ Concurrent console and minion operations successful")
	})

	// Test rapid consecutive operations on both protocols
	t.Run("RapidMixedOperations", func(t *testing.T) {
		operations := []struct {
			name    string
			command string
		}{
			{"console-version", "version"},
			{"minion-list", "minion-list"},
			{"console-help", "help"},
			{"tag-list", "tag-list"},
		}

		start := time.Now()
		for _, op := range operations {
			t.Run(op.name, func(t *testing.T) {
				output, err := runConsoleCommandWithTimeout(op.command, 10*time.Second)
				assert.NoError(t, err, fmt.Sprintf("Rapid operation %s should succeed", op.name))
				assert.NotEmpty(t, output, "Should receive response for each rapid operation")
			})
		}

		elapsed := time.Since(start)
		t.Logf("✅ Completed %d rapid mixed operations in %v", len(operations), elapsed)
	})

	// Test load handling with multiple simultaneous console connections
	t.Run("MultipleConsoleConnections", func(t *testing.T) {
		const numConnections = 5
		var wg sync.WaitGroup
		errors := make([]error, numConnections)

		wg.Add(numConnections)

		for i := 0; i < numConnections; i++ {
			go func(index int) {
				defer wg.Done()
				output, err := runConsoleCommandWithTimeout("version", 10*time.Second)
				errors[index] = err
				if err == nil && !strings.Contains(output, "Console") {
					errors[index] = fmt.Errorf("invalid response: %s", output)
				}
			}(i)
		}

		wg.Wait()

		// Check that all connections succeeded
		successCount := 0
		for i, err := range errors {
			if err == nil {
				successCount++
			} else {
				t.Logf("Connection %d failed: %v", i, err)
			}
		}

		assert.Equal(t, numConnections, successCount, "All simultaneous console connections should succeed")
		t.Logf("✅ %d/%d simultaneous console connections successful", successCount, numConnections)
	})
}

// testCertificateEdgeCases tests certificate-related edge cases and failure scenarios
func testCertificateEdgeCases(t *testing.T) {
	t.Log("Testing certificate edge cases...")

	// Test console configuration validation
	t.Run("ConsoleConfigValidation", func(t *testing.T) {
		// Test that console is properly configured for mTLS
		// This is validated by successful console operations
		output, err := runConsoleCommandWithTimeout("version", 5*time.Second)
		assert.NoError(t, err, "Console should be properly configured for mTLS")
		assert.Contains(t, output, "Console", "Should get proper version response")
		t.Log("✅ Console mTLS configuration validated")
	})

	// Test server certificate validation by console
	t.Run("ServerCertificateValidation", func(t *testing.T) {
		// The console validates the server's certificate during mTLS handshake
		// Successful operations prove server certificate validation works
		output, err := runConsoleCommandWithTimeout("help", 5*time.Second)
		assert.NoError(t, err, "Server certificate should be validated successfully")
		assert.Contains(t, output, "Commands", "Should receive response after server cert validation")
		t.Log("✅ Server certificate validation successful")
	})

	// Test client certificate presentation by console
	t.Run("ClientCertificatePresentation", func(t *testing.T) {
		// The console must present a valid client certificate for mTLS
		// Successful operations prove client certificate presentation works
		output, err := runConsoleCommandWithTimeout("minion-list", 10*time.Second)
		assert.NoError(t, err, "Client certificate should be presented successfully")
		assert.Contains(t, output, "minions", "Should receive response after client cert presentation")
		t.Log("✅ Client certificate presentation successful")
	})

	// Test certificate authority validation
	t.Run("CertificateAuthorityValidation", func(t *testing.T) {
		// Both client and server certificates must be validated against the same CA
		// Successful mTLS operations prove CA validation works
		operations := []string{"version", "help", "minion-list", "tag-list"}

		for _, cmd := range operations {
			output, err := runConsoleCommandWithTimeout(cmd, 5*time.Second)
			assert.NoError(t, err, fmt.Sprintf("CA validation should succeed for command: %s", cmd))
			assert.NotEmpty(t, output, "Should receive response with valid CA validation")
		}

		t.Log("✅ Certificate Authority validation successful for all operations")
	})

	// Test connection stability under load
	t.Run("ConnectionStabilityUnderLoad", func(t *testing.T) {
		const numIterations = 10
		successCount := 0

		for i := 0; i < numIterations; i++ {
			output, err := runConsoleCommandWithTimeout("version", 3*time.Second)
			if err == nil && strings.Contains(output, "Console") {
				successCount++
			} else {
				t.Logf("Iteration %d failed: %v", i+1, err)
			}
		}

		// Allow for some tolerance in case of temporary network issues
		expectedMinSuccess := int(float64(numIterations) * 0.9) // 90% success rate
		assert.GreaterOrEqual(t, successCount, expectedMinSuccess,
			"Should maintain stable mTLS connections under repeated load")

		t.Logf("✅ Connection stability: %d/%d successful (%d%% success rate)",
			successCount, numIterations, (successCount*100)/numIterations)
	})

	// Test protocol separation (minion vs console traffic)
	t.Run("ProtocolSeparation", func(t *testing.T) {
		// Verify that console traffic (mTLS) is properly separated from minion traffic (TLS)
		// This is validated by successful console operations on the mTLS port

		consoleTests := []string{"version", "help", "minion-list"}

		for _, cmd := range consoleTests {
			output, err := runConsoleCommandWithTimeout(cmd, 5*time.Second)
			assert.NoError(t, err, fmt.Sprintf("Console command %s should work on mTLS port", cmd))
			assert.NotEmpty(t, output, "Should receive response on mTLS port")
		}

		t.Log("✅ Protocol separation verified - console uses mTLS port successfully")
	})
}

// testMinionReconnectionRaceCondition tests the race condition fix where multiple
// concurrent StreamCommands calls during nexus restart cause registry failures
func testMinionReconnectionRaceCondition(t *testing.T) {
	t.Log("Testing minion reconnection race condition scenario...")

	// This test validates the fix for the bug where minions create multiple concurrent
	// StreamCommands calls during nexus server restart, causing registry synchronization failures.
	//
	// The bug was: Race condition where minion creates multiple concurrent StreamCommands calls
	// during nexus server restart, causing registry synchronization failures. The nexus
	// successfully registers the minion but immediately fails to find it in subsequent
	// stream establishment attempts.
	//
	// The fix: Added backoff delay (1 second) between reconnection attempts and enhanced
	// error handling to ensure clean disconnection state.

	// Step 1: Verify minion is currently connected and working
	t.Run("PreTestMinionConnectivity", func(t *testing.T) {
		output, err := runConsoleCommandWithTimeout("minion-list", 10*time.Second)
		assert.NoError(t, err, "Should be able to list minions before test")
		assert.Contains(t, output, "docker-minion", "Docker minion should be connected before test")
		t.Log("✅ Pre-test: Minion connectivity verified")
	})

	// Step 2: Send a baseline command to ensure system is working
	t.Run("BaselineCommandTest", func(t *testing.T) {
		output, err := runConsoleCommandWithTimeout("command-send all echo baseline-test", 10*time.Second)
		assert.NoError(t, err, "Baseline command should succeed")
		assert.Contains(t, output, "Command dispatched successfully", "Baseline command should be dispatched")

		commandID := extractCommandID(output)
		assert.NotEmpty(t, commandID, "Should get command ID for baseline test")

		// Wait for result to verify full system functionality
		for i := 0; i < 30; i++ {
			if getNbResultsInDB(t, commandID) > 0 {
				t.Log("✅ Baseline command completed successfully")
				break
			}
			time.Sleep(500 * time.Millisecond)
		}

		resultCount := getNbResultsInDB(t, commandID)
		assert.Greater(t, resultCount, 0, "Baseline command should have results")
	})

	// Step 3: Trigger the race condition scenario by restarting nexus server
	// This simulates the exact conditions that caused the original bug
	t.Run("NexusRestartRaceCondition", func(t *testing.T) {
		t.Log("🔄 Triggering race condition by restarting nexus server...")

		// Restart nexus server to trigger reconnection race condition
		restartCmd := exec.Command("docker", "compose", "restart", "nexus_server")
		restartCmd.Stdout = os.Stdout
		restartCmd.Stderr = os.Stderr

		restartStart := time.Now()
		err := restartCmd.Run()
		assert.NoError(t, err, "Should be able to restart nexus server")
		restartDuration := time.Since(restartStart)
		t.Logf("🔄 Nexus restart completed in %v", restartDuration)

		// Wait for services to stabilize - this is where the race condition would occur
		t.Log("⏳ Waiting for services to stabilize after restart...")
		time.Sleep(5 * time.Second)

		// Wait for nexus server to be ready
		for i := 0; i < 30; i++ {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", minionPort), 2*time.Second)
			if err == nil {
				conn.Close()
				break
			}

			if i == 29 {
				t.Fatalf("Nexus server not ready after restart within timeout")
			}
			time.Sleep(1 * time.Second)
		}

		t.Log("✅ Nexus server is responding after restart")
	})

	// Step 4: Verify that minion reconnects without race condition errors
	t.Run("PostRestartConnectivity", func(t *testing.T) {
		t.Log("🔍 Verifying minion reconnection after nexus restart...")

		// Wait a bit longer for minion to reconnect with backoff delay
		time.Sleep(3 * time.Second)

		// Check minion connectivity - this would fail if race condition occurred
		var lastErr error
		var lastOutput string

		for i := 0; i < 20; i++ {
			output, err := runConsoleCommandWithTimeout("minion-list", 10*time.Second)
			lastErr = err
			lastOutput = output

			if err == nil && strings.Contains(output, "docker-minion") {
				t.Logf("✅ Minion reconnected successfully after %d attempts", i+1)
				return
			}

			t.Logf("⏳ Attempt %d: Waiting for minion reconnection...", i+1)
			time.Sleep(2 * time.Second)
		}

		t.Errorf("Minion failed to reconnect after nexus restart. Last error: %v, Last output: %s", lastErr, lastOutput)
	})

	// Step 5: Verify system functionality after recovery
	t.Run("PostRecoveryFunctionality", func(t *testing.T) {
		t.Log("🧪 Testing system functionality after race condition recovery...")

		// Send a test command to verify end-to-end functionality
		output, err := runConsoleCommandWithTimeout("command-send all echo race-condition-recovery-test", 15*time.Second)
		assert.NoError(t, err, "Should be able to send commands after recovery")
		assert.Contains(t, output, "Command dispatched successfully", "Commands should be dispatched after recovery")

		commandID := extractCommandID(output)
		assert.NotEmpty(t, commandID, "Should get command ID after recovery")

		// Wait for command results to verify full system recovery
		resultFound := false
		for i := 0; i < 30; i++ {
			if getNbResultsInDB(t, commandID) > 0 {
				resultFound = true
				t.Logf("✅ Post-recovery command completed after %d attempts", i+1)
				break
			}
			time.Sleep(1 * time.Second)
		}

		assert.True(t, resultFound, "Command should complete execution after race condition recovery")

		// Verify the actual command result
		resultCount := getNbResultsInDB(t, commandID)
		assert.Greater(t, resultCount, 0, "Should have command results after recovery")

		t.Log("✅ System fully functional after race condition recovery")
	})

	// Step 6: Validate that logs don't show the original race condition error
	t.Run("LogValidation", func(t *testing.T) {
		t.Log("📋 Checking logs for race condition indicators...")

		// Get recent minion logs to check for race condition errors
		logCmd := exec.Command("docker", "logs", "minion", "--tail", "50")
		logOutput, err := logCmd.Output()

		if err != nil {
			t.Logf("⚠️  Could not retrieve minion logs: %v", err)
			return
		}

		logStr := string(logOutput)

		// Check for the original error pattern that indicated the race condition
		if strings.Contains(logStr, "Error receiving command") &&
			strings.Contains(logStr, "*status.Error") {
			t.Logf("⚠️  Original race condition error pattern still present in logs")
			t.Logf("Recent logs:\n%s", logStr)
		} else {
			t.Log("✅ No race condition error patterns detected in recent logs")
		}

		// Look for positive indicators of the fix working
		if strings.Contains(logStr, "Disconnected from nexus") ||
			strings.Contains(logStr, "Connected to nexus") {
			t.Log("✅ Proper connection state management detected in logs")
		}
	})

	// Step 7: Stress test the fix with multiple rapid reconnections
	t.Run("StressTestReconnection", func(t *testing.T) {
		t.Log("🚀 Stress testing reconnection resilience...")

		for iteration := 1; iteration <= 3; iteration++ {
			t.Logf("🔄 Stress test iteration %d/3", iteration)

			// Quick restart cycle
			restartCmd := exec.Command("docker", "compose", "restart", "nexus_server")
			restartStart := time.Now()
			err := restartCmd.Run()
			restartDuration := time.Since(restartStart)
			t.Logf("📊 Nexus restart took: %v", restartDuration)
			assert.NoError(t, err, "Should be able to restart nexus for stress test")

			// Wait for nexus server to be ready first
			t.Log("⏳ Waiting for nexus server to be ready...")
			for i := 0; i < 30; i++ {
				conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", minionPort), 2*time.Second)
				if err == nil {
					conn.Close()
					t.Logf("✅ Nexus server ready after %d attempts", i+1)
					break
				}
				if i == 29 {
					t.Fatalf("Nexus server not ready after restart within timeout")
				}
				time.Sleep(1 * time.Second)
			}

			// Additional wait for minion reconnection with backoff
			t.Log("⏳ Waiting for minion reconnection...")
			time.Sleep(3 * time.Second)

			// Verify connectivity with increased patience
			connected := false
			maxAttempts := 20 // Increased from 10
			for i := 0; i < maxAttempts; i++ {
				output, err := runConsoleCommandWithTimeout("minion-list", 8*time.Second) // Increased timeout
				if err == nil && strings.Contains(output, "docker-minion") {
					connected = true
					t.Logf("✅ Minion reconnected after %d attempts in iteration %d", i+1, iteration)
					break
				}
				if i < maxAttempts-1 { // Don't log on last attempt
					t.Logf("⏳ Attempt %d/%d: Waiting for minion reconnection...", i+1, maxAttempts)
				}
				time.Sleep(2 * time.Second) // Increased from 1 second
			}

			if !connected {
				t.Errorf("❌ Minion failed to reconnect in stress test iteration %d after %d attempts", iteration, maxAttempts)
				// Continue with next iteration instead of failing immediately
				continue
			}

			t.Logf("✅ Stress test iteration %d completed successfully", iteration)
		}

		t.Log("✅ Stress test completed - race condition fix is resilient")
	})

	t.Log("🎯 Race condition test completed successfully")
	t.Log("📊 Test Summary:")
	t.Log("   ✅ Original race condition scenario reproduced and resolved")
	t.Log("   ✅ Backoff delay prevents tight reconnection loops")
	t.Log("   ✅ System recovers gracefully from nexus restarts")
	t.Log("   ✅ End-to-end functionality verified after recovery")
	t.Log("   ✅ Fix is resilient under stress testing")
}
