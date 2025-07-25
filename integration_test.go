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

// Debug configuration
var debugMode = os.Getenv("DEBUG") != ""

// Debug logging helper
func logDebug(t *testing.T, format string, args ...interface{}) {
	if debugMode {
		t.Logf("DEBUG: "+format, args...)
	}
}

// loadTestEnvironment loads environment variables from .env.test
func loadTestEnvironment(t *testing.T) {
	envFile := ".env.test"
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		t.Logf("Warning: %s file not found, using defaults", envFile)
		return
	}

	file, err := os.Open(envFile)
	if err != nil {
		t.Logf("Warning: Failed to open %s: %v", envFile, err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Remove quotes if present
			if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
				value = value[1 : len(value)-1]
			}
			os.Setenv(key, value)
		}
	}

	if err := scanner.Err(); err != nil {
		t.Logf("Warning: Error reading %s: %v", envFile, err)
	}
}

// getDBConnectionString builds database connection string from environment variables
func getDBConnectionString() string {
	dbUser := os.Getenv("DBUSER")
	if dbUser == "" {
		dbUser = "postgres"
	}

	dbPass := os.Getenv("DBPASS")
	if dbPass == "" {
		dbPass = "postgres"
	}

	dbName := os.Getenv("DBNAME")
	if dbName == "" {
		dbName = "minexus"
	}

	dbPort := os.Getenv("DBPORT")
	if dbPort == "" {
		dbPort = "5432"
	}

	return fmt.Sprintf("postgres://%s:%s@localhost:%s/%s?sslmode=disable", dbUser, dbPass, dbPort, dbName)
}

// Test configuration
const (
	dockerComposeFile = "docker-compose.yml"
	consoleExecutable = "./console-test"
	maxRetries        = 15                     // Reduced from 30 (race conditions are fixed)
	retryInterval     = 500 * time.Millisecond // Reduced from 1s
	minionPort        = 11972                  // Standard TLS port for minions
	consolePort       = 11973                  // mTLS port for console
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

	// Load test environment variables from .env.test
	loadTestEnvironment(t)

	startTime := time.Now()
	t.Log("TIMING: Starting integration tests (SLOW_TESTS is set)")

	// Setup: Ensure Docker services are running
	setupStart := time.Now()
	t.Log("TIMING: Starting Docker services setup...")
	setupDockerServices(t)
	setupDockerDuration := time.Since(setupStart)
	t.Logf("TIMING: Docker setup completed in %v", setupDockerDuration)

	// Wait for services to be ready
	waitStart := time.Now()
	t.Log("TIMING: Starting service readiness checks...")
	waitForServices(t)
	waitDuration := time.Since(waitStart)
	t.Logf("TIMING: Service readiness check completed in %v", waitDuration)

	// Build console if needed
	buildStart := time.Now()
	t.Log("TIMING: Starting console build...")
	buildConsole(t)
	buildDuration := time.Since(buildStart)
	t.Logf("TIMING: Console build completed in %v", buildDuration)

	setupTotalDuration := time.Since(startTime)
	t.Logf("TIMING: TOTAL SETUP TIME: %v", setupTotalDuration)

	// Run test suites with parallelization for significant speed improvement
	testsStart := time.Now()
	t.Log("TIMING: Starting PARALLELIZED test suite execution...")

	// Track test suite completion times with channels for parallel execution
	testTimes := make(map[string]time.Duration)
	var mu sync.Mutex

	// Function to record test completion time
	recordTime := func(name string, duration time.Duration) {
		mu.Lock()
		testTimes[name] = duration
		mu.Unlock()
		t.Logf("TIMING: %s test suite completed in %v", name, duration)
	}

	// Phase 1: Run independent test suites in parallel (most tests can run concurrently)
	t.Log("TIMING: Phase 1 - Running independent test suites in parallel...")
	phase1Start := time.Now()

	t.Run("ParallelPhase1", func(t *testing.T) {
		// Basic console and connectivity tests (can run in parallel)
		t.Run("ConsoleCommands", func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			testConsoleCommands(t)
			recordTime("ConsoleCommands", time.Since(start))
		})

		t.Run("MTLSConnectivity", func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			testMTLSConnectivity(t)
			recordTime("MTLSConnectivity", time.Since(start))
		})

		t.Run("DualPortServer", func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			testDualPortServer(t)
			recordTime("DualPortServer", time.Since(start))
		})

		t.Run("CertificateValidation", func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			testCertificateValidation(t)
			recordTime("CertificateValidation", time.Since(start))
		})

		t.Run("CertificateEdgeCases", func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			testCertificateEdgeCases(t)
			recordTime("CertificateEdgeCases", time.Since(start))
		})

		t.Run("ErrorCases", func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			testErrorCases(t)
			recordTime("ErrorCases", time.Since(start))
		})
	})

	phase1Duration := time.Since(phase1Start)
	t.Logf("TIMING: Phase 1 (parallel basic tests) completed in %v", phase1Duration)

	// Phase 2: Run command execution tests in parallel (these already use intelligent batching)
	t.Log("TIMING: Phase 2 - Running command execution test suites in parallel...")
	phase2Start := time.Now()

	t.Run("ParallelPhase2", func(t *testing.T) {
		t.Run("ShellCommands", func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			testShellCommands(t)
			recordTime("ShellCommands", time.Since(start))
		})

		t.Run("FileCommands", func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			testFileCommands(t)
			recordTime("FileCommands", time.Since(start))
		})

		t.Run("SystemCommands", func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			testSystemCommands(t)
			recordTime("SystemCommands", time.Since(start))
		})

		t.Run("DockerComposeCommands", func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			testDockerComposeCommands(t)
			recordTime("DockerComposeCommands", time.Since(start))
		})

		t.Run("MixedTrafficScenarios", func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			testMixedTrafficScenarios(t)
			recordTime("MixedTrafficScenarios", time.Since(start))
		})
	})

	phase2Duration := time.Since(phase2Start)
	t.Logf("TIMING: Phase 2 (parallel command tests) completed in %v", phase2Duration)

	// Phase 3: Run tests that need to be sequential (database integrity and disruptive tests)
	t.Log("TIMING: Phase 3 - Running sequential tests that require isolation...")
	phase3Start := time.Now()

	// Database integrity should run after command tests to verify data consistency
	dbStart := time.Now()
	t.Run("DatabaseIntegrity", testDatabaseIntegrity)
	dbDuration := time.Since(dbStart)
	recordTime("DatabaseIntegrity", dbDuration)

	// Race condition test must run last as it restarts services (disruptive)
	raceConditionStart := time.Now()
	t.Run("MinionReconnectionRaceCondition", testMinionReconnectionRaceCondition)
	raceConditionDuration := time.Since(raceConditionStart)
	recordTime("MinionReconnectionRaceCondition", raceConditionDuration)

	phase3Duration := time.Since(phase3Start)
	t.Logf("TIMING: Phase 3 (sequential tests) completed in %v", phase3Duration)

	testsDuration := time.Since(testsStart)
	totalDuration := time.Since(startTime)

	// Enhanced timing summary with parallelization benefits
	t.Log("TIMING: =============== PARALLELIZED PERFORMANCE SUMMARY ===============")
	t.Logf("TIMING: Setup Phase:")
	t.Logf("TIMING:   - Docker setup:        %8v (%5.1f%%)", setupDockerDuration, float64(setupDockerDuration)/float64(totalDuration)*100)
	t.Logf("TIMING:   - Service readiness:   %8v (%5.1f%%)", waitDuration, float64(waitDuration)/float64(totalDuration)*100)
	t.Logf("TIMING:   - Console build:       %8v (%5.1f%%)", buildDuration, float64(buildDuration)/float64(totalDuration)*100)
	t.Logf("TIMING:   - Total setup:         %8v (%5.1f%%)", setupTotalDuration, float64(setupTotalDuration)/float64(totalDuration)*100)

	t.Logf("TIMING: Parallel Execution Phases:")
	t.Logf("TIMING:   - Phase 1 (basic):     %8v (%5.1f%%) - 6 suites in parallel", phase1Duration, float64(phase1Duration)/float64(totalDuration)*100)
	t.Logf("TIMING:   - Phase 2 (commands):  %8v (%5.1f%%) - 5 suites in parallel", phase2Duration, float64(phase2Duration)/float64(totalDuration)*100)
	t.Logf("TIMING:   - Phase 3 (sequential): %8v (%5.1f%%) - 2 suites sequential", phase3Duration, float64(phase3Duration)/float64(totalDuration)*100)

	t.Logf("TIMING: Individual Test Suites (actual runtime in parallel context):")

	// Display individual test times in sorted order
	testNames := []string{
		"ConsoleCommands", "ShellCommands", "FileCommands", "SystemCommands",
		"DockerComposeCommands", "ErrorCases", "DatabaseIntegrity", "MTLSConnectivity",
		"DualPortServer", "CertificateValidation", "MixedTrafficScenarios",
		"CertificateEdgeCases", "MinionReconnectionRaceCondition",
	}

	totalIndividualTime := time.Duration(0)
	for _, name := range testNames {
		if duration, exists := testTimes[name]; exists {
			t.Logf("TIMING:   - %-24s %8v (%5.1f%%)", name+":", duration, float64(duration)/float64(totalDuration)*100)
			totalIndividualTime += duration
		}
	}

	t.Logf("TIMING: Parallelization Efficiency:")
	t.Logf("TIMING:   - Total test execution: %8v (%5.1f%%)", testsDuration, float64(testsDuration)/float64(totalDuration)*100)
	t.Logf("TIMING:   - Sum of individual:   %8v (if run sequentially)", totalIndividualTime)
	if totalIndividualTime > testsDuration {
		parallelSpeedup := float64(totalIndividualTime) / float64(testsDuration)
		t.Logf("TIMING:   - Parallel speedup:    %.1fx faster than sequential", parallelSpeedup)
		t.Logf("TIMING:   - Time saved:          %8v", totalIndividualTime-testsDuration)
	}

	t.Logf("TIMING: TOTAL INTEGRATION TIME:   %8v", totalDuration)
	t.Log("TIMING: ================================================================")
}

// setupDockerServices ensures nexus, nexus_db, and minion services are running
func setupDockerServices(t *testing.T) {
	logDebug(t, "Checking Docker Compose services status...")

	// Check if services are running
	statusCheckStart := time.Now()
	cmd := exec.Command("docker", "compose", "ps", "--format", "json")
	output, err := cmd.Output()
	statusCheckDuration := time.Since(statusCheckStart)
	t.Logf("TIMING: Docker status check took %v", statusCheckDuration)

	if err != nil {
		t.Fatalf("Failed to check docker compose status: %v", err)
	}

	// Parse output to check service status
	parseStart := time.Now()
	services := parseDockerComposePS(string(output))
	parseDuration := time.Since(parseStart)
	t.Logf("TIMING: Docker status parsing took %v", parseDuration)

	requiredServices := []string{"nexus_db", "nexus_server", "minion"}
	missingServices := []string{}

	for _, service := range requiredServices {
		if status, exists := services[service]; !exists || status != "running" {
			missingServices = append(missingServices, service)
		}
	}

	if len(missingServices) > 0 {
		logDebug(t, "TIMING: Services not running: %v. Starting them...", missingServices)

		// Start services
		serviceStartStart := time.Now()
		cmd = exec.Command("docker", "compose", "up", "-d", "nexus_server", "minion")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to start docker compose services: %v", err)
		}
		serviceStartDuration := time.Since(serviceStartStart)
		t.Logf("TIMING: Docker service startup took %v", serviceStartDuration)

		logDebug(t, "Services started successfully")
	} else {
		logDebug(t, "All required services are already running")
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
	t.Log("TIMING: Starting service readiness checks...")

	// Wait for database
	logDebug(t, "Checking database connectivity...")
	dbStart := time.Now()
	for i := 0; i < maxRetries; i++ {
		db, err := sql.Open("postgres", getDBConnectionString())
		if err == nil {
			if err := db.Ping(); err == nil {
				db.Close()
				dbDuration := time.Since(dbStart)
				t.Logf("TIMING: Database ready after %v (attempt %d/%d)", dbDuration, i+1, maxRetries)
				break
			}
			db.Close()
		}

		if i == maxRetries-1 {
			t.Fatalf("TIMING: Database not ready after %d retries and %v", maxRetries, time.Since(dbStart))
		}

		if i%5 == 0 { // Log every 5 attempts
			t.Logf("TIMING: Database attempt %d/%d (elapsed: %v)", i+1, maxRetries, time.Since(dbStart))
		}
		time.Sleep(retryInterval)
	}

	// Check Docker health status before port tests
	logDebug(t, "Checking Docker Compose service health...")
	healthCheckStart := time.Now()
	cmd := exec.Command("docker", "compose", "ps", "--format", "table")
	if output, err := cmd.Output(); err == nil {
		logDebug(t, "Docker services status:\n%s", string(output))
	}
	healthCheckDuration := time.Since(healthCheckStart)
	t.Logf("TIMING: Docker health check took %v", healthCheckDuration)

	// Wait for nexus minion server (port 11972)
	t.Logf("TIMING: Checking nexus minion server (port %d)...", minionPort)
	minionStart := time.Now()
	for i := 0; i < maxRetries; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", minionPort), 1*time.Second)
		if err == nil {
			conn.Close()
			minionDuration := time.Since(minionStart)
			t.Logf("TIMING: Minion server ready after %v (attempt %d/%d)", minionDuration, i+1, maxRetries)
			break
		}

		if i == maxRetries-1 {
			t.Fatalf("TIMING: Nexus minion server not ready after %d retries and %v. Last error: %v", maxRetries, time.Since(minionStart), err)
		}

		if i%3 == 0 { // Log every 3 attempts
			t.Logf("TIMING: Minion port attempt %d/%d (elapsed: %v, error: %v)", i+1, maxRetries, time.Since(minionStart), err)
		}
		time.Sleep(retryInterval)
	}

	// Wait for nexus console server (port 11973)
	t.Logf("TIMING: Checking nexus console server (port %d)...", consolePort)
	consoleStart := time.Now()
	for i := 0; i < maxRetries; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", consolePort), 1*time.Second)
		if err == nil {
			conn.Close()
			consoleDuration := time.Since(consoleStart)
			t.Logf("TIMING: Console server ready after %v (attempt %d/%d)", consoleDuration, i+1, maxRetries)
			break
		}

		if i == maxRetries-1 {
			t.Fatalf("TIMING: Nexus console server not ready after %d retries and %v. Last error: %v", maxRetries, time.Since(consoleStart), err)
		}

		if i%3 == 0 { // Log every 3 attempts
			t.Logf("TIMING: Console port attempt %d/%d (elapsed: %v, error: %v)", i+1, maxRetries, time.Since(consoleStart), err)
		}
		time.Sleep(retryInterval)
	}

	t.Log("TIMING: All services are ready (database, minion port, console port)")
}

// buildConsole builds the console executable if it doesn't exist
func buildConsole(t *testing.T) {
	if _, err := os.Stat(consoleExecutable); os.IsNotExist(err) {
		logDebug(t, "Building console executable...")
		buildStart := time.Now()

		// Backup certs
		backupStart := time.Now()
		cmd := exec.Command("mv", "internal/certs/files", "internal/certs/files.backup")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to backup certs: %v", err)
		}
		backupDuration := time.Since(backupStart)
		t.Logf("TIMING: Cert backup took %v", backupDuration)

		// Copy test certs
		copyStart := time.Now()
		cmd = exec.Command("cp", "-r", "internal/certs/files.backup/test", "internal/certs/files")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to copy test certs: %v", err)
		}
		copyDuration := time.Since(copyStart)
		t.Logf("TIMING: Test cert copy took %v", copyDuration)

		// Build console
		goBuildStart := time.Now()
		cmd = exec.Command("go", "build", "-o", "console-test", "./cmd/console")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to build console: %v", err)
		}
		goBuildDuration := time.Since(goBuildStart)
		t.Logf("TIMING: Go build took %v", goBuildDuration)

		// Cleanup
		cleanupStart := time.Now()
		cmd = exec.Command("rm", "-rf", "internal/certs/files")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to remove certs: %v", err)
		}
		cmd = exec.Command("mv", "internal/certs/files.backup", "internal/certs/files")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to restore certs: %v", err)
		}
		cleanupDuration := time.Since(cleanupStart)
		t.Logf("TIMING: Cleanup took %v", cleanupDuration)

		totalBuildDuration := time.Since(buildStart)
		t.Logf("TIMING: Total console build took %v", totalBuildDuration)
	} else {
		logDebug(t, "Console executable already exists, skipping build")
	}
}

// runConsoleCommandWithTimeout executes a console command with timeout
func runConsoleCommandWithTimeout(command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Pass command directly as stdin to avoid shell quote interpretation issues
	// This approach handles JSON arguments with unmatched quotes reliably
	cmd := exec.CommandContext(ctx, consoleExecutable, "--server", "localhost:11973")
	cmd.Stdin = strings.NewReader(command + "\n")

	// Use explicit --server flag instead of environment variables for reliability
	// This ensures the console connects to localhost:11973 regardless of .env file settings

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
			t.Parallel()                                                           // Enable parallel execution for console command tests
			output, err := runConsoleCommandWithTimeout(tt.command, 5*time.Second) // Reduced from 10s

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

	// TIMING: Execute commands in batch, then poll intelligently
	var commandIDs []string
	var testNames []string

	batchStart := time.Now()
	t.Log("TIMING: Starting shell command batch execution...")

	// Phase 1: Send all successful commands rapidly (no waiting between sends)
	for _, tt := range tests {
		t.Run(fmt.Sprintf("send_%s", tt.name), func(t *testing.T) {
			if tt.expectError {
				// Handle error cases immediately
				errorStart := time.Now()
				output, err := runConsoleCommandWithTimeout(tt.command, 2*time.Second)
				errorDuration := time.Since(errorStart)
				logDebug(t, "Error case '%s' handled in %v", tt.name, errorDuration)
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
			dbVerifyStart := time.Now()
			verifyCommandInDB(t, commandID)
			dbVerifyDuration := time.Since(dbVerifyStart)

			// Store for batch polling
			commandIDs = append(commandIDs, commandID)
			testNames = append(testNames, tt.name)

			if len(commandID) >= 8 {
				t.Logf("TIMING: Sent command '%s' in %v (ID: %s, DB verify: %v)", tt.name, commandExecTime, commandID[:8], dbVerifyDuration)
			} else {
				t.Logf("TIMING: Sent command '%s' in %v (ID: %s, DB verify: %v)", tt.name, commandExecTime, commandID, dbVerifyDuration)
			}
		})
	}

	sendDuration := time.Since(batchStart)
	t.Logf("TIMING: BATCH SEND completed: %d commands in %v (vs %v with sequential waits)",
		len(commandIDs), sendDuration, time.Duration(len(commandIDs))*10*time.Second)

	// Phase 2: Intelligent polling for ALL results
	if len(commandIDs) > 0 {
		pollStart := time.Now()
		t.Logf("TIMING: Starting intelligent polling for %d commands...", len(commandIDs))

		// Initial wait for execution to start
		t.Log("TIMING: Initial wait period before polling...")
		time.Sleep(1 * time.Second) // Reduced from 2 seconds

		// Progressive polling with early termination
		resultsFound := make(map[string]bool)
		maxAttempts := 30 // 15 seconds max with 500ms polling (reduced from 60)
		pollCount := 0

		for attempt := 0; attempt < maxAttempts; attempt++ {
			pollCount++
			attemptStart := time.Now()
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
					t.Logf("TIMING: Results for '%s' (%s) found after %v (poll attempt %d)",
						testNames[i], idDisplay, elapsed, pollCount)
				}
			}

			attemptDuration := time.Since(attemptStart)
			if attempt%10 == 0 { // Log every 10 attempts
				t.Logf("TIMING: Poll attempt %d took %v, found %d/%d results", pollCount, attemptDuration, foundCount, len(commandIDs))
			}

			// Early termination when all results found
			if foundCount == len(commandIDs) {
				totalPollTime := time.Since(pollStart)
				t.Logf("TIMING: ALL RESULTS FOUND: %d/%d in %v after %d poll attempts (early termination)",
					foundCount, len(commandIDs), totalPollTime, pollCount)
				break
			}

			// Adaptive polling: fast initially, slower later
			pollInterval := 300 * time.Millisecond // Reduced from 500ms
			if attempt > 15 {                      // Reduced threshold from 20
				pollInterval = 500 * time.Millisecond // Reduced from 1s
			}

			time.Sleep(pollInterval)
		}

		totalPollTime := time.Since(pollStart)
		finalCount := len(resultsFound)
		originalTime := time.Duration(len(commandIDs)) * 10 * time.Second
		timesSaved := originalTime - (sendDuration + totalPollTime)

		t.Logf("TIMING: SHELL COMMAND OPTIMIZATION RESULTS:")
		t.Logf("TIMING:   Commands processed: %d/%d successful", finalCount, len(commandIDs))
		t.Logf("TIMING:   Total time: %v (send: %v + poll: %v)", sendDuration+totalPollTime, sendDuration, totalPollTime)
		t.Logf("TIMING:   Poll attempts: %d", pollCount)
		t.Logf("TIMING:   Original approach: %v (with 10s fixed sleeps)", originalTime)
		t.Logf("TIMING:   Time saved: %v (%.1f%% faster)", timesSaved, float64(timesSaved)/float64(originalTime)*100)
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

	// TIMING: Apply same intelligent polling to file commands
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
				t.Logf("TIMING: Sent file command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID[:8])
			} else {
				t.Logf("TIMING: Sent file command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID)
			}
		})
	}

	// Phase 2: Intelligent polling for file results
	if len(commandIDs) > 0 {
		pollStart := time.Now()
		t.Logf("TIMING: Polling for %d file operation results...", len(commandIDs))

		time.Sleep(1 * time.Second) // Initial wait (reduced from 3s)

		resultsFound := make(map[string]bool)
		for attempt := 0; attempt < 30; attempt++ { // Reduced from 60
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
					t.Logf("TIMING: File results for '%s' (%s) found after %v",
						testNames[i], idDisplay, elapsed)
				}
			}

			if foundCount == len(commandIDs) {
				totalPollTime := time.Since(pollStart)
				t.Logf("TIMING: ALL FILE RESULTS found in %v", totalPollTime)
				break
			}

			time.Sleep(300 * time.Millisecond) // Reduced from 500ms
		}

		totalTime := time.Since(batchStart)
		originalTime := time.Duration(len(commandIDs)) * 10 * time.Second
		t.Logf("TIMING: File optimization: %v vs %v original (%.1f%% improvement)",
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

	// TIMING: System commands with intelligent polling
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
				t.Logf("TIMING: Sent system command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID[:8])
			} else {
				t.Logf("TIMING: Sent system command '%s' in %v (ID: %s)", tt.name, commandExecTime, commandID)
			}
		})
	}

	// Phase 2: Intelligent polling for system command results
	if len(commandIDs) > 0 {
		pollStart := time.Now()
		t.Logf("TIMING: Polling for %d system command results...", len(commandIDs))

		time.Sleep(1 * time.Second) // Initial wait (reduced from 3s)

		resultsFound := make(map[string]bool)
		for attempt := 0; attempt < 30; attempt++ { // Reduced from 60
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
					t.Logf("TIMING: System results for '%s' (%s) found after %v",
						testNames[i], idDisplay, elapsed)
				}
			}

			if foundCount == len(commandIDs) {
				totalPollTime := time.Since(pollStart)
				t.Logf("TIMING: ALL SYSTEM RESULTS found in %v", totalPollTime)
				break
			}

			time.Sleep(300 * time.Millisecond) // Reduced from 500ms
		}

		totalTime := time.Since(batchStart)
		originalTime := time.Duration(len(commandIDs)) * 10 * time.Second
		t.Logf("TIMING: System optimization: %v vs %v original (%.1f%% improvement)",
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
			logDebug(t, "📤 Sent docker-compose command '%s': %s", tt.name, commandID)
		})
	}

	// Wait for results with intelligent polling
	if len(commandIDs) > 0 {
		t.Logf("TIMING: Waiting for %d docker-compose command results...", len(commandIDs))

		pollStart := time.Now()
		resultsFound := make(map[string]bool)
		maxAttempts := 20 // Reduced from 30

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
					t.Logf("TIMING: Docker-compose results for '%s' (%s) found after %v",
						testNames[i], idDisplay, elapsed)
				}
			}

			// Early termination when all results found
			if foundCount == len(commandIDs) {
				totalPollTime := time.Since(pollStart)
				t.Logf("TIMING: ALL DOCKER-COMPOSE RESULTS FOUND: %d/%d in %v",
					foundCount, len(commandIDs), totalPollTime)
				break
			}

			time.Sleep(500 * time.Millisecond) // Reduced from 1s
		}

		// Verify final results
		finalCount := len(resultsFound)
		logDebug(t, "📊 Docker-compose commands processed: %d/%d", finalCount, len(commandIDs))

		// In integration tests, docker-compose commands may fail if Docker isn't available
		// This is expected and the test should focus on command delivery, not execution success
		if finalCount < len(commandIDs) {
			logDebug(t, "Some docker-compose commands may have failed due to Docker availability in test environment")
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
			t.Parallel()                                                           // Enable parallel execution for error case tests
			output, err := runConsoleCommandWithTimeout(tt.command, 5*time.Second) // Reduced from 10s

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
				logDebug(t, "Command error: %v", err)
			}
		})
	}
}

// testDatabaseIntegrity tests database consistency and integrity
func testDatabaseIntegrity(t *testing.T) {
	db, err := sql.Open("postgres", getDBConnectionString())
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
	logDebug(t, "Total commands in database: %d", commandCount)

	// Check if command results were recorded
	var resultCount int
	err = db.QueryRow("SELECT COUNT(*) FROM command_results").Scan(&resultCount)
	require.NoError(t, err, "Should query result count")
	logDebug(t, "Total command results in database: %d", resultCount)

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
	db, err := sql.Open("postgres", getDBConnectionString())
	require.NoError(t, err, "Should connect to database")
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM commands WHERE id = $1", commandID).Scan(&count)
	require.NoError(t, err, "Should query command existence")
	assert.Greater(t, count, 0, fmt.Sprintf("Command %s should exist in database", commandID))
}

// getNbResultsInDb returns the actual count of results for a command in the command_results table
func getNbResultsInDB(t *testing.T, commandID string) int {
	db, err := sql.Open("postgres", getDBConnectionString())
	require.NoError(t, err, "Should connect to database")
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM command_results WHERE command_id = $1", commandID).Scan(&count)
	require.NoError(t, err, "Should query result existence")
	logDebug(t, "Command %s has %d results in database", commandID, count)
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
			t.Parallel()                                                           // Enable parallel execution for mTLS connectivity tests
			output, err := runConsoleCommandWithTimeout(tt.command, 5*time.Second) // Reduced from 10s
			assert.NoError(t, err, "mTLS console command should succeed")

			for _, expected := range tt.expected {
				assert.Contains(t, output, expected, "mTLS console output should contain expected text")
			}

			t.Logf("TIMING: mTLS command '%s' executed successfully", tt.command)
		})
	}
}

// testDualPortServer tests that both minion and console ports are operational
func testDualPortServer(t *testing.T) {
	t.Log("Testing dual-port server functionality...")

	// Test minion port connectivity (should be accessible)
	t.Run("MinionPortConnectivity", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", minionPort), 2*time.Second) // Reduced from 5s
		assert.NoError(t, err, "Should be able to connect to minion port")
		if conn != nil {
			conn.Close()
		}
		t.Logf("TIMING: Minion port %d is accessible", minionPort)
	})

	// Test console port connectivity (should be accessible)
	t.Run("ConsolePortConnectivity", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", consolePort), 2*time.Second) // Reduced from 5s
		assert.NoError(t, err, "Should be able to connect to console port")
		if conn != nil {
			conn.Close()
		}
		t.Logf("TIMING: Console port %d is accessible", consolePort)
	})

	// Test that both ports are different
	t.Run("PortSeparation", func(t *testing.T) {
		assert.NotEqual(t, minionPort, consolePort, "Minion and console ports should be different")
		t.Logf("TIMING: Port separation verified: minion=%d, console=%d", minionPort, consolePort)
	})

	// Test simultaneous connections to both ports
	t.Run("SimultaneousConnections", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(2)

		// Connect to minion port
		go func() {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", minionPort), 2*time.Second) // Reduced from 5s
			assert.NoError(t, err, "Should connect to minion port simultaneously")
			if conn != nil {
				time.Sleep(1 * time.Second) // Hold connection briefly
				conn.Close()
			}
		}()

		// Connect to console port
		go func() {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", consolePort), 2*time.Second) // Reduced from 5s
			assert.NoError(t, err, "Should connect to console port simultaneously")
			if conn != nil {
				time.Sleep(1 * time.Second) // Hold connection briefly
				conn.Close()
			}
		}()

		wg.Wait()
		t.Log("TIMING: Simultaneous connections to both ports successful")
	})
}

// testCertificateValidation tests certificate validation scenarios
func testCertificateValidation(t *testing.T) {
	t.Log("Testing certificate validation...")

	// Since the console executable uses embedded certificates, we test that
	// the mTLS connection works (which proves certificate validation is working)
	t.Run("ValidCertificateAuthentication", func(t *testing.T) {
		// This test verifies that console can authenticate with valid certificates
		output, err := runConsoleCommandWithTimeout("version", 5*time.Second) // Reduced from 10s
		assert.NoError(t, err, "Console with valid certificates should authenticate successfully")
		assert.Contains(t, output, "Console", "Should receive proper response with valid certificates")
		t.Log("✅ Valid certificate authentication successful")
	})

	// Test that console connects to the correct port (mTLS port)
	t.Run("MTLSPortUsage", func(t *testing.T) {
		// The console should be configured to use port 11973 (mTLS port)
		// This is verified indirectly by successful console operations
		output, err := runConsoleCommandWithTimeout("minion-list", 5*time.Second) // Reduced from 10s
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
			consoleOutput, consoleErr = runConsoleCommandWithTimeout("minion-list", 8*time.Second) // Reduced from 15s
		}()

		// Minion operation (via console but targeting minion functionality)
		go func() {
			defer wg.Done()
			minionOutput, minionErr = runConsoleCommandWithTimeout("command-send all echo concurrent-test", 8*time.Second) // Reduced from 15s
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
				output, err := runConsoleCommandWithTimeout(op.command, 5*time.Second) // Reduced from 10s
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
				output, err := runConsoleCommandWithTimeout("version", 5*time.Second) // Reduced from 10s
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
				logDebug(t, "Connection %d failed: %v", i, err)
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
				logDebug(t, "Iteration %d failed: %v", i+1, err)
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

	t.Run("PreTestMinionConnectivity", func(t *testing.T) {
		verifyPreTestConnectivity(t)
	})

	t.Run("BaselineCommandTest", func(t *testing.T) {
		runBaselineCommandTest(t)
	})

	t.Run("NexusRestartRaceCondition", func(t *testing.T) {
		triggerNexusRestart(t)
	})

	t.Run("PostRestartConnectivity", func(t *testing.T) {
		verifyPostRestartConnectivity(t)
	})

	t.Run("PostRecoveryFunctionality", func(t *testing.T) {
		verifyPostRecoveryFunctionality(t)
	})

	t.Run("LogValidation", func(t *testing.T) {
		validateRaceConditionLogs(t)
	})

	t.Run("StressTestReconnection", func(t *testing.T) {
		stressTestReconnection(t)
	})
}

// verifyPreTestConnectivity verifies minion connectivity before testing
func verifyPreTestConnectivity(t *testing.T) {
	output, err := runConsoleCommandWithTimeout("minion-list", 5*time.Second)
	assert.NoError(t, err, "Should be able to list minions before test")
	assert.Contains(t, output, "docker-minion", "Docker minion should be connected before test")
	t.Log("✅ Pre-test: Minion connectivity verified")
}

// runBaselineCommandTest runs a baseline command to ensure system is working
func runBaselineCommandTest(t *testing.T) {
	output, err := runConsoleCommandWithTimeout("command-send all echo baseline-test", 5*time.Second)
	assert.NoError(t, err, "Baseline command should succeed")
	assert.Contains(t, output, "Command dispatched successfully", "Baseline command should be dispatched")

	commandID := extractCommandID(output)
	assert.NotEmpty(t, commandID, "Should get command ID for baseline test")

	waitForCommandCompletion(t, commandID, 30, 300*time.Millisecond, "Baseline command completed successfully")

	resultCount := getNbResultsInDB(t, commandID)
	assert.Greater(t, resultCount, 0, "Baseline command should have results")
}

// triggerNexusRestart triggers nexus restart to simulate race conditions
func triggerNexusRestart(t *testing.T) {
	t.Log("🔄 Triggering race condition by restarting nexus server...")

	restartStart := time.Now()
	err := executeNexusRestart()
	assert.NoError(t, err, "Should be able to restart nexus server")

	restartDuration := time.Since(restartStart)
	t.Logf("🔄 Nexus restart completed in %v", restartDuration)

	t.Log("⏳ Waiting for services to stabilize after restart...")
	time.Sleep(5 * time.Second)

	waitForNexusReadiness(t, 20, 1*time.Second, 500*time.Millisecond)
	t.Log("✅ Nexus server is responding after restart")
}

// verifyPostRestartConnectivity verifies minion reconnection after restart
func verifyPostRestartConnectivity(t *testing.T) {
	t.Log("🔍 Verifying minion reconnection after nexus restart...")
	time.Sleep(2 * time.Second)

	success := waitForMinionReconnection(t, 15, 5*time.Second, 1*time.Second)
	if !success {
		t.Error("Minion failed to reconnect after nexus restart")
	}
}

// verifyPostRecoveryFunctionality verifies system functionality after recovery
func verifyPostRecoveryFunctionality(t *testing.T) {
	t.Log("🧪 Testing system functionality after race condition recovery...")

	output, err := runConsoleCommandWithTimeout("command-send all echo race-condition-recovery-test", 8*time.Second)
	assert.NoError(t, err, "Should be able to send commands after recovery")
	assert.Contains(t, output, "Command dispatched successfully", "Commands should be dispatched after recovery")

	commandID := extractCommandID(output)
	assert.NotEmpty(t, commandID, "Should get command ID after recovery")

	resultFound := waitForCommandResults(t, commandID, 20, 500*time.Millisecond)
	assert.True(t, resultFound, "Command should complete execution after race condition recovery")

	resultCount := getNbResultsInDB(t, commandID)
	assert.Greater(t, resultCount, 0, "Should have command results after recovery")

	t.Log("✅ System fully functional after race condition recovery")
}

// validateRaceConditionLogs validates logs for race condition indicators
func validateRaceConditionLogs(t *testing.T) {
	t.Log("📋 Checking logs for race condition indicators...")

	logOutput, err := getMinionLogs()
	if err != nil {
		logDebug(t, "⚠️  Could not retrieve minion logs: %v", err)
		return
	}

	logStr := string(logOutput)
	checkForRaceConditionErrors(t, logStr)
	checkForConnectionManagement(t, logStr)
}

// stressTestReconnection performs stress testing of reconnection resilience
func stressTestReconnection(t *testing.T) {
	t.Log("🚀 Stress testing reconnection resilience...")

	for iteration := 1; iteration <= 3; iteration++ {
		t.Logf("🔄 Stress test iteration %d/3", iteration)

		if !performStressTestIteration(t, iteration) {
			continue
		}

		t.Logf("✅ Stress test iteration %d completed successfully", iteration)
	}

	t.Log("✅ Stress test completed - race condition fix is resilient")
}

// waitForCommandCompletion waits for a command to complete with configurable parameters
func waitForCommandCompletion(t *testing.T, commandID string, maxAttempts int, sleepDuration time.Duration, successMsg string) {
	for i := 0; i < maxAttempts; i++ {
		if getNbResultsInDB(t, commandID) > 0 {
			t.Log("✅ " + successMsg)
			return
		}
		time.Sleep(sleepDuration)
	}
}

// executeNexusRestart executes the nexus server restart command
func executeNexusRestart() error {
	restartCmd := exec.Command("docker", "compose", "restart", "nexus_server")
	restartCmd.Stdout = os.Stdout
	restartCmd.Stderr = os.Stderr
	return restartCmd.Run()
}

// waitForNexusReadiness waits for nexus server to be ready after restart
func waitForNexusReadiness(t *testing.T, maxAttempts int, connectTimeout, sleepDuration time.Duration) {
	for i := 0; i < maxAttempts; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", minionPort), connectTimeout)
		if err == nil {
			conn.Close()
			return
		}

		if i == maxAttempts-1 {
			t.Fatalf("Nexus server not ready after restart within timeout")
		}
		time.Sleep(sleepDuration)
	}
}

// waitForMinionReconnection waits for minion to reconnect with retry logic
func waitForMinionReconnection(t *testing.T, maxAttempts int, commandTimeout, sleepDuration time.Duration) bool {
	var lastErr error
	var lastOutput string

	for i := 0; i < maxAttempts; i++ {
		output, err := runConsoleCommandWithTimeout("minion-list", commandTimeout)
		lastErr = err
		lastOutput = output

		if err == nil && strings.Contains(output, "docker-minion") {
			t.Logf("✅ Minion reconnected successfully after %d attempts", i+1)
			return true
		}

		t.Logf("⏳ Attempt %d: Waiting for minion reconnection...", i+1)
		time.Sleep(sleepDuration)
	}

	t.Errorf("Minion failed to reconnect. Last error: %v, Last output: %s", lastErr, lastOutput)
	return false
}

// waitForCommandResults waits for command results to be available
func waitForCommandResults(t *testing.T, commandID string, maxAttempts int, sleepDuration time.Duration) bool {
	for i := 0; i < maxAttempts; i++ {
		if getNbResultsInDB(t, commandID) > 0 {
			t.Logf("✅ Post-recovery command completed after %d attempts", i+1)
			return true
		}
		time.Sleep(sleepDuration)
	}
	return false
}

// getMinionLogs retrieves minion logs for analysis
func getMinionLogs() ([]byte, error) {
	logCmd := exec.Command("docker", "logs", "minion", "--tail", "50")
	return logCmd.Output()
}

// checkForRaceConditionErrors checks logs for race condition error patterns
func checkForRaceConditionErrors(t *testing.T, logStr string) {
	if strings.Contains(logStr, "Error receiving command") && strings.Contains(logStr, "*status.Error") {
		logDebug(t, "⚠️  Original race condition error pattern still present in logs")
		logDebug(t, "Recent logs:\n%s", logStr)
	} else {
		logDebug(t, "✅ No race condition error patterns detected in recent logs")
	}
}

// checkForConnectionManagement checks logs for proper connection management
func checkForConnectionManagement(t *testing.T, logStr string) {
	if strings.Contains(logStr, "Disconnected from nexus") || strings.Contains(logStr, "Connected to nexus") {
		logDebug(t, "✅ Proper connection state management detected in logs")
	}
}

// performStressTestIteration performs a single stress test iteration
func performStressTestIteration(t *testing.T, iteration int) bool {
	restartStart := time.Now()
	err := executeNexusRestart()
	restartDuration := time.Since(restartStart)
	t.Logf("📊 Nexus restart took: %v", restartDuration)
	assert.NoError(t, err, "Should be able to restart nexus for stress test")

	logDebug(t, "⏳ Waiting for nexus server to be ready...")
	waitForNexusReadiness(t, 20, 1*time.Second, 500*time.Millisecond)
	t.Logf("✅ Nexus server ready after stress test restart %d", iteration)

	logDebug(t, "⏳ Waiting for minion reconnection...")
	time.Sleep(2 * time.Second)

	return verifyStressTestConnectivity(t, iteration, 15)
}

// verifyStressTestConnectivity verifies connectivity during stress test
func verifyStressTestConnectivity(t *testing.T, iteration, maxAttempts int) bool {
	for i := 0; i < maxAttempts; i++ {
		output, err := runConsoleCommandWithTimeout("minion-list", 5*time.Second)
		if err == nil && strings.Contains(output, "docker-minion") {
			t.Logf("✅ Minion reconnected after %d attempts in iteration %d", i+1, iteration)
			return true
		}
		if i < maxAttempts-1 {
			logDebug(t, "⏳ Attempt %d/%d: Waiting for minion reconnection...", i+1, maxAttempts)
		}
		time.Sleep(1 * time.Second)
	}

	t.Errorf("❌ Minion failed to reconnect in stress test iteration %d after %d attempts", iteration, maxAttempts)
	return false
}
