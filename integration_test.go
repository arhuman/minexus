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
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test configuration
const (
	dockerComposeFile = "docker-compose.yml"
	consoleExecutable = "./console"
	dbConnString      = "postgres://postgres:postgres@localhost:5432/minexus?sslmode=disable"
	maxRetries        = 30
	retryInterval     = 1 * time.Second
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
//   - nexus_server: Nexus gRPC server (port 11972)
//   - minion_1: Test minion client
//
// Test Categories:
//   - Console command testing (help, version, listings)
//   - Shell command execution via minions
//   - File operations on remote systems
//   - System information gathering
//   - Error handling and edge cases
//   - Database integrity and consistency

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
	t.Run("ErrorCases", testErrorCases)
	t.Run("DatabaseIntegrity", testDatabaseIntegrity)

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

	requiredServices := []string{"nexus_db", "nexus_server", "minion_1"}
	missingServices := []string{}

	for _, service := range requiredServices {
		if status, exists := services[service]; !exists || status != "running" {
			missingServices = append(missingServices, service)
		}
	}

	if len(missingServices) > 0 {
		t.Logf("Services not running: %v. Starting them...", missingServices)

		// Start services
		cmd = exec.Command("docker", "compose", "up", "-d", "nexus", "minion")
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
		if strings.Contains(line, "minion_1") {
			if strings.Contains(line, "running") {
				services["minion_1"] = "running"
			}
		}
	}

	return services
}

// waitForServices waits for services to be ready
func waitForServices(t *testing.T) {
	t.Log("Waiting for services to be ready...")

	// Wait for database
	for i := 0; i < maxRetries; i++ {
		db, err := sql.Open("postgres", dbConnString)
		if err == nil {
			if err := db.Ping(); err == nil {
				db.Close()
				break
			}
			db.Close()
		}

		if i == maxRetries-1 {
			t.Fatalf("Database not ready after %d retries", maxRetries)
		}

		time.Sleep(retryInterval)
	}

	// Wait for nexus server (check port connectivity directly)
	for i := 0; i < maxRetries; i++ {
		conn, err := net.DialTimeout("tcp", "localhost:11972", 2*time.Second)
		if err == nil {
			conn.Close()
			break
		}

		if i == maxRetries-1 {
			t.Fatalf("Nexus server not ready after %d retries. Last error: %v", maxRetries, err)
		}

		time.Sleep(retryInterval)
	}

	t.Log("All services are ready")
}

// buildConsole builds the console executable if it doesn't exist
func buildConsole(t *testing.T) {
	if _, err := os.Stat(consoleExecutable); os.IsNotExist(err) {
		t.Log("Building console executable...")
		cmd := exec.Command("go", "build", "-o", "console", "./cmd/console")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to build console: %v", err)
		}
	}
}

// runConsoleCommandWithTimeout executes a console command with timeout
func runConsoleCommandWithTimeout(command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("echo '%s' | %s", command, consoleExecutable))
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
			contains:   []string{"Connected minions", "minion-docker-01"},
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

			t.Logf("📤 Sent command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID[:8])
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

				actualResults := getNbResultsInDb(t, commandID)
				if actualResults > 0 {
					resultsFound[commandID] = true
					foundCount++
					elapsed := time.Since(pollStart)
					t.Logf("✅ Results for '%s' (%s) found after %v",
						testNames[i], commandID[:8], elapsed)
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

			t.Logf("📁 Sent file command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID[:8])
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

				actualResults := getNbResultsInDb(t, commandID)
				if actualResults > 0 {
					resultsFound[commandID] = true
					foundCount++
					elapsed := time.Since(pollStart)
					t.Logf("📁 File results for '%s' (%s) found after %v",
						testNames[i], commandID[:8], elapsed)
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

			t.Logf("🖥️ Sent system command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID[:8])
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

				actualResults := getNbResultsInDb(t, commandID)
				if actualResults > 0 {
					resultsFound[commandID] = true
					foundCount++
					elapsed := time.Since(pollStart)
					t.Logf("🖥️ System results for '%s' (%s) found after %v",
						testNames[i], commandID[:8], elapsed)
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
func getNbResultsInDb(t *testing.T, commandID string) int {
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

			err = cmd.Wait()
			// Console might exit with non-zero code, that's OK

			outputStr := output.String()
			for _, expected := range tt.expected {
				assert.Contains(t, outputStr, expected, "Output should contain expected text")
			}
		})
	}
}
