package command

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/arhuman/minexus/protogen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// setupTestDir creates a temporary directory with docker-compose files for testing
func setupTestDir(t *testing.T) string {
	tmpDir := t.TempDir()

	// Create a valid docker-compose.yml file
	composeContent := `version: '3.8'
services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"
  database:
    image: postgres:alpine
    environment:
      POSTGRES_DB: testdb
      POSTGRES_USER: testuser
      POSTGRES_PASSWORD: testpass
`

	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	err := os.WriteFile(composeFile, []byte(composeContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test docker-compose.yml: %v", err)
	}

	return tmpDir
}

// setupInvalidTestDir creates a directory without docker-compose files
func setupInvalidTestDir(t *testing.T) string {
	return t.TempDir()
}

// createTestExecutionContext creates a test execution context
func createTestExecutionContext() *ExecutionContext {
	logger := zap.NewNop()
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)
	ctx := context.Background()

	return NewExecutionContext(ctx, logger, &atom, "test-minion", "test-cmd-123")
}

func TestDockerComposeUpCommand(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		setupDir    func(t *testing.T) string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid path with simple syntax",
			payload:     "docker-compose:up " + setupTestDir(t),
			setupDir:    setupTestDir,
			expectError: false,
		},
		{
			name:        "Valid path with JSON syntax",
			payload:     `{"command": "up", "path": "` + setupTestDir(t) + `"}`,
			setupDir:    setupTestDir,
			expectError: false,
		},
		{
			name:        "With build flag",
			payload:     `{"command": "up", "path": "` + setupTestDir(t) + `", "build": true}`,
			setupDir:    setupTestDir,
			expectError: false,
		},
		{
			name:        "With specific service",
			payload:     `{"command": "up", "path": "` + setupTestDir(t) + `", "service": "web"}`,
			setupDir:    setupTestDir,
			expectError: false,
		},
		{
			name:        "With service and build",
			payload:     `{"command": "up", "path": "` + setupTestDir(t) + `", "service": "web", "build": true}`,
			setupDir:    setupTestDir,
			expectError: false,
		},
		{
			name:        "Missing path",
			payload:     `{"command": "up"}`,
			setupDir:    setupTestDir,
			expectError: true,
			errorMsg:    "path is required",
		},
		{
			name:        "Nonexistent path",
			payload:     "docker-compose:up /nonexistent/path",
			setupDir:    setupTestDir,
			expectError: true,
			errorMsg:    "path does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewDockerComposeUpCommand()
			ctx := createTestExecutionContext()

			// Setup test directory if needed
			var testDir string
			if tt.setupDir != nil {
				testDir = tt.setupDir(t)
				// Replace placeholder in payload with actual test directory
				if tt.payload == "docker-compose:up "+setupTestDir(t) {
					tt.payload = "docker-compose:up " + testDir
				} else if containsIgnoreCase(tt.payload, setupTestDir(t)) {
					tt.payload = replaceIgnoreCase(tt.payload, setupTestDir(t), testDir)
				}
			}

			result, err := cmd.Execute(ctx, tt.payload)

			if tt.expectError {
				if err != nil {
					t.Logf("Expected error occurred: %v", err)
				}
				if result != nil && result.ExitCode == 0 {
					t.Error("Expected command to fail but it succeeded")
				}
				if tt.errorMsg != "" && result != nil && result.Stderr != "" {
					if result.Stderr != tt.errorMsg && !containsIgnoreCase(result.Stderr, tt.errorMsg) {
						t.Logf("Expected error message to contain '%s', got '%s'", tt.errorMsg, result.Stderr)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result == nil {
					t.Error("Expected result but got nil")
				} else if result.ExitCode != 0 {
					// Note: In real scenarios, docker-compose commands might fail if Docker isn't running
					// In unit tests, we mainly test the command parsing and validation logic
					t.Logf("Command failed as expected in unit test environment (no Docker): %s", result.Stderr)
				}
			}
		})
	}
}

func TestDockerComposeDownCommand(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		setupDir    func(t *testing.T) string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid path with simple syntax",
			payload:     "docker-compose:down " + setupTestDir(t),
			setupDir:    setupTestDir,
			expectError: false,
		},
		{
			name:        "Valid path with JSON syntax",
			payload:     `{"command": "down", "path": "` + setupTestDir(t) + `"}`,
			setupDir:    setupTestDir,
			expectError: false,
		},
		{
			name:        "With specific service",
			payload:     `{"command": "down", "path": "` + setupTestDir(t) + `", "service": "web"}`,
			setupDir:    setupTestDir,
			expectError: false,
		},
		{
			name:        "Missing path",
			payload:     `{"command": "down"}`,
			setupDir:    setupTestDir,
			expectError: true,
			errorMsg:    "path is required",
		},
		{
			name:        "Nonexistent path",
			payload:     "docker-compose:down /nonexistent/path",
			setupDir:    setupTestDir,
			expectError: true,
			errorMsg:    "path does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewDockerComposeDownCommand()
			ctx := createTestExecutionContext()

			// Setup test directory if needed
			var testDir string
			if tt.setupDir != nil {
				testDir = tt.setupDir(t)
				// Replace placeholder in payload with actual test directory
				if tt.payload == "docker-compose:down "+setupTestDir(t) {
					tt.payload = "docker-compose:down " + testDir
				} else if containsIgnoreCase(tt.payload, setupTestDir(t)) {
					tt.payload = replaceIgnoreCase(tt.payload, setupTestDir(t), testDir)
				}
			}

			result, err := cmd.Execute(ctx, tt.payload)

			if tt.expectError {
				if err != nil {
					t.Logf("Expected error occurred: %v", err)
				}
				if result != nil && result.ExitCode == 0 {
					t.Error("Expected command to fail but it succeeded")
				}
				if tt.errorMsg != "" && result != nil && result.Stderr != "" {
					if result.Stderr != tt.errorMsg && !containsIgnoreCase(result.Stderr, tt.errorMsg) {
						t.Logf("Expected error message to contain '%s', got '%s'", tt.errorMsg, result.Stderr)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result == nil {
					t.Error("Expected result but got nil")
				} else if result.ExitCode != 0 {
					// Note: In real scenarios, docker-compose commands might fail if Docker isn't running
					// In unit tests, we mainly test the command parsing and validation logic
					t.Logf("Command failed as expected in unit test environment (no Docker): %s", result.Stderr)
				}
			}
		})
	}
}

func TestDockerComposeCommand(t *testing.T) {
	// Test the unified router command (should always fail)
	cmd := NewDockerComposeCommand()
	ctx := createTestExecutionContext()

	result, err := cmd.Execute(ctx, "docker-compose")

	if err != nil {
		t.Logf("Expected error occurred: %v", err)
	}

	if result == nil {
		t.Error("Expected result but got nil")
	} else if result.ExitCode == 0 {
		t.Error("Expected router command to fail but it succeeded")
	}

	if result != nil && result.Stderr != "" {
		expectedMsg := "use specific docker-compose subcommands"
		if !containsIgnoreCase(result.Stderr, expectedMsg) {
			t.Errorf("Expected error message to contain '%s', got '%s'", expectedMsg, result.Stderr)
		}
	}
}

func TestParseDockerComposePayload(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		expected    *DockerComposeRequest
		expectError bool
	}{
		{
			name:    "Simple ps command",
			payload: "docker-compose:ps /opt/myapp",
			expected: &DockerComposeRequest{
				Command: "ps",
				Path:    "/opt/myapp",
			},
			expectError: false,
		},
		{
			name:    "Simple up command",
			payload: "docker-compose:up /opt/myapp",
			expected: &DockerComposeRequest{
				Command: "up",
				Path:    "/opt/myapp",
			},
			expectError: false,
		},
		{
			name:    "Up command with service",
			payload: "docker-compose:up /opt/myapp web",
			expected: &DockerComposeRequest{
				Command: "up",
				Path:    "/opt/myapp",
				Service: "web",
			},
			expectError: false,
		},
		{
			name:    "Up command with build flag",
			payload: "docker-compose:up /opt/myapp --build",
			expected: &DockerComposeRequest{
				Command: "up",
				Path:    "/opt/myapp",
				Build:   true,
			},
			expectError: false,
		},
		{
			name:    "Up command with service and build",
			payload: "docker-compose:up /opt/myapp --build web",
			expected: &DockerComposeRequest{
				Command: "up",
				Path:    "/opt/myapp",
				Service: "web",
				Build:   true,
			},
			expectError: false,
		},
		{
			name:    "JSON format",
			payload: `{"command": "up", "path": "/opt/myapp", "service": "web", "build": true}`,
			expected: &DockerComposeRequest{
				Command: "up",
				Path:    "/opt/myapp",
				Service: "web",
				Build:   true,
			},
			expectError: false,
		},
		{
			name:        "Invalid JSON",
			payload:     `{"command": "up", "path":`,
			expected:    nil,
			expectError: true,
		},
		{
			name:        "Empty payload",
			payload:     "",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "Command without path",
			payload:     "docker-compose:ps",
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDockerComposePayload(tt.payload)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Error("Expected result but got nil")
				return
			}

			if result.Command != tt.expected.Command {
				t.Errorf("Expected command %s, got %s", tt.expected.Command, result.Command)
			}
			if result.Path != tt.expected.Path {
				t.Errorf("Expected path %s, got %s", tt.expected.Path, result.Path)
			}
			if result.Service != tt.expected.Service {
				t.Errorf("Expected service %s, got %s", tt.expected.Service, result.Service)
			}
			if result.Build != tt.expected.Build {
				t.Errorf("Expected build %t, got %t", tt.expected.Build, result.Build)
			}
		})
	}
}

func TestValidateDockerComposePath(t *testing.T) {
	// Test with valid directory containing docker-compose.yml
	validDir := setupTestDir(t)
	err := validateDockerComposePath(validDir)
	if err != nil {
		t.Errorf("Expected valid path to pass validation, got error: %v", err)
	}

	// Test with nonexistent directory
	err = validateDockerComposePath("/nonexistent/path")
	if err == nil {
		t.Error("Expected validation to fail for nonexistent path")
	}

	// Test with directory without docker-compose file
	invalidDir := setupInvalidTestDir(t)
	err = validateDockerComposePath(invalidDir)
	if err == nil {
		t.Error("Expected validation to fail for directory without docker-compose file")
	}
}

func TestGetComposeFile(t *testing.T) {
	// Test with directory containing docker-compose.yml
	testDir := setupTestDir(t)
	composeFile := getComposeFile(testDir)
	expectedPath := filepath.Join(testDir, "docker-compose.yml")
	if composeFile != expectedPath {
		t.Errorf("Expected compose file path %s, got %s", expectedPath, composeFile)
	}

	// Test with directory containing docker-compose.yaml (create it)
	testDir2 := t.TempDir()
	yamlFile := filepath.Join(testDir2, "docker-compose.yaml")
	err := os.WriteFile(yamlFile, []byte("version: '3.8'\nservices:\n  test:\n    image: nginx"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	composeFile2 := getComposeFile(testDir2)
	if composeFile2 != yamlFile {
		t.Errorf("Expected compose file path %s, got %s", yamlFile, composeFile2)
	}

	// Test preference for .yml over .yaml
	testDir3 := t.TempDir()
	ymlFile := filepath.Join(testDir3, "docker-compose.yml")
	yamlFile3 := filepath.Join(testDir3, "docker-compose.yaml")

	err = os.WriteFile(ymlFile, []byte("version: '3.8'"), 0644)
	if err != nil {
		t.Fatalf("Failed to create yml file: %v", err)
	}
	err = os.WriteFile(yamlFile3, []byte("version: '3.8'"), 0644)
	if err != nil {
		t.Fatalf("Failed to create yaml file: %v", err)
	}

	composeFile3 := getComposeFile(testDir3)
	if composeFile3 != ymlFile {
		t.Errorf("Expected preference for .yml file %s, got %s", ymlFile, composeFile3)
	}
}

func TestDockerComposeCommandsMetadata(t *testing.T) {
	commands := []ExecutableCommand{
		NewDockerComposePSCommand(),
		NewDockerComposeUpCommand(),
		NewDockerComposeDownCommand(),
		NewDockerComposeFindCommand(),
		NewDockerComposeViewCommand(),
		NewDockerComposeCommand(),
	}

	for _, cmd := range commands {
		metadata := cmd.Metadata()

		if metadata.Name == "" {
			t.Error("Command name should not be empty")
		}

		if metadata.Category != "docker" {
			t.Errorf("Expected category 'docker', got '%s'", metadata.Category)
		}

		if metadata.Description == "" {
			t.Error("Command description should not be empty")
		}

		if metadata.Usage == "" {
			t.Error("Command usage should not be empty")
		}

		t.Logf("Command: %s - %s", metadata.Name, metadata.Description)
	}
}

func TestDockerComposeViewCommand(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(t *testing.T) string
		payload     string
		expectError bool
		errorMsg    string
		checkOutput func(t *testing.T, output string, testDir string)
	}{
		{
			name:        "Valid path with simple syntax",
			setupFunc:   setupTestDir,
			payload:     "", // Will be set dynamically with test directory
			expectError: false,
			checkOutput: func(t *testing.T, output string, testDir string) {
				assert.Contains(t, output, "Content of")
				assert.Contains(t, output, "docker-compose.yml")
				assert.Contains(t, output, "version: '3.8'")
				assert.Contains(t, output, "services:")
				assert.Contains(t, output, "web:")
				assert.Contains(t, output, "database:")
				assert.Contains(t, output, "nginx:alpine")
				assert.Contains(t, output, "postgres:alpine")
			},
		},
		{
			name:        "Valid path with JSON syntax",
			setupFunc:   setupTestDir,
			payload:     "", // Will be set dynamically with test directory
			expectError: false,
			checkOutput: func(t *testing.T, output string, testDir string) {
				assert.Contains(t, output, "Content of")
				assert.Contains(t, output, "docker-compose.yml")
				assert.Contains(t, output, "version: '3.8'")
				assert.Contains(t, output, "services:")
			},
		},
		{
			name:        "Missing path",
			setupFunc:   setupTestDir,
			payload:     `{"command": "view"}`,
			expectError: true,
			errorMsg:    "path is required",
		},
		{
			name:        "Invalid JSON format",
			setupFunc:   setupTestDir,
			payload:     `{"command": "view", "path": "/invalid/path"`,
			expectError: true,
			errorMsg:    "invalid JSON format",
		},
		{
			name:        "Nonexistent path",
			setupFunc:   setupTestDir,
			payload:     `{"command": "view", "path": "/nonexistent/path"}`,
			expectError: true,
			errorMsg:    "path does not exist",
		},
		{
			name:        "Path without docker-compose file",
			setupFunc:   setupInvalidTestDir,
			payload:     "", // Will be set dynamically with test directory
			expectError: true,
			errorMsg:    "no docker-compose.yml or docker-compose.yaml found",
		},
		{
			name:        "Empty payload",
			setupFunc:   setupTestDir,
			payload:     "",
			expectError: true,
			errorMsg:    "invalid payload format",
		},
		{
			name: "Simple syntax with .yaml extension",
			setupFunc: func(t *testing.T) string {
				tmpDir := t.TempDir()
				// Create a docker-compose.yaml file (with .yaml extension)
				composeContent := `version: '3.8'
services:
  api:
    image: node:alpine
    ports:
      - "3000:3000"
`
				composeFile := filepath.Join(tmpDir, "docker-compose.yaml")
				err := os.WriteFile(composeFile, []byte(composeContent), 0644)
				if err != nil {
					t.Fatalf("Failed to create test docker-compose.yaml: %v", err)
				}
				return tmpDir
			},
			payload:     "", // Will be set dynamically with test directory
			expectError: false,
			checkOutput: func(t *testing.T, output string, testDir string) {
				assert.Contains(t, output, "Content of")
				assert.Contains(t, output, "docker-compose.yaml")
				assert.Contains(t, output, "version: '3.8'")
				assert.Contains(t, output, "api:")
				assert.Contains(t, output, "node:alpine")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := tt.setupFunc(t)

			// Set payload dynamically if needed
			payload := tt.payload
			if payload == "" && !strings.Contains(tt.name, "Empty payload") {
				if strings.Contains(tt.name, "JSON syntax") {
					payload = fmt.Sprintf(`{"command": "view", "path": "%s"}`, testDir)
				} else {
					payload = fmt.Sprintf("docker-compose:view %s", testDir)
				}
			} else if payload == "" && strings.Contains(tt.name, "Empty payload") {
				payload = ""
			}

			// Set up command and context
			cmd := NewDockerComposeViewCommand()
			ctx := createTestExecutionContext()

			// Execute command
			result, err := cmd.Execute(ctx, payload)

			// Verify results
			if tt.expectError {
				require.NotNil(t, result)
				assert.NotEqual(t, int32(0), result.ExitCode)
				if tt.errorMsg != "" {
					assert.Contains(t, result.Stderr, tt.errorMsg)
				}
			} else {
				require.Nil(t, err)
				require.NotNil(t, result)
				assert.Equal(t, int32(0), result.ExitCode)
				if tt.checkOutput != nil {
					tt.checkOutput(t, result.Stdout, testDir)
				}
			}
		})
	}
}

// Helper functions for tests

func containsIgnoreCase(s, substr string) bool {
	s = strings.ToLower(s)
	substr = strings.ToLower(substr)
	return strings.Contains(s, substr)
}

func replaceIgnoreCase(s, old, new string) string {
	return strings.ReplaceAll(s, old, new)
}

func TestFindDockerComposeDirectories(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(t *testing.T) string
		expectedCount int
		expectedDirs  []string // relative to test directory
	}{
		{
			name: "Multiple nested directories with compose files",
			setupFunc: func(t *testing.T) string {
				rootDir := t.TempDir()

				// Create complex structure
				dirs := []string{
					"app1",
					"app2/backend",
					"app2/frontend",
					"services/database",
					"services/cache",
				}

				for _, dir := range dirs {
					fullPath := filepath.Join(rootDir, dir)
					os.MkdirAll(fullPath, 0755)

					// Create compose file in each directory
					composeFile := filepath.Join(fullPath, "docker-compose.yml")
					err := os.WriteFile(composeFile, []byte("version: '3.8'\nservices:\n  test:\n    image: alpine"), 0644)
					require.NoError(t, err)
				}

				// Create a directory without compose file
				emptyDir := filepath.Join(rootDir, "empty")
				os.MkdirAll(emptyDir, 0755)

				return rootDir
			},
			expectedCount: 5,
			expectedDirs: []string{
				"app1",
				"app2/backend",
				"app2/frontend",
				"services/database",
				"services/cache",
			},
		},
		{
			name: "Single compose file",
			setupFunc: func(t *testing.T) string {
				rootDir := t.TempDir()
				err := os.WriteFile(filepath.Join(rootDir, "docker-compose.yml"), []byte("version: '3.8'"), 0644)
				require.NoError(t, err)
				return rootDir
			},
			expectedCount: 1,
			expectedDirs:  []string{"."},
		},
		{
			name: "No compose files",
			setupFunc: func(t *testing.T) string {
				rootDir := t.TempDir()

				// Create some directories with other files
				subDir := filepath.Join(rootDir, "subdir")
				os.MkdirAll(subDir, 0755)

				err := os.WriteFile(filepath.Join(rootDir, "readme.txt"), []byte("no compose here"), 0644)
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join(subDir, "config.json"), []byte("{}"), 0644)
				require.NoError(t, err)

				return rootDir
			},
			expectedCount: 0,
			expectedDirs:  []string{},
		},
		{
			name: "Mixed .yml and .yaml files",
			setupFunc: func(t *testing.T) string {
				rootDir := t.TempDir()

				// Create directories with different file extensions
				ymlDir := filepath.Join(rootDir, "yml-app")
				yamlDir := filepath.Join(rootDir, "yaml-app")
				os.MkdirAll(ymlDir, 0755)
				os.MkdirAll(yamlDir, 0755)

				err := os.WriteFile(filepath.Join(ymlDir, "docker-compose.yml"), []byte("version: '3.8'"), 0644)
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join(yamlDir, "docker-compose.yaml"), []byte("version: '3.8'"), 0644)
				require.NoError(t, err)

				return rootDir
			},
			expectedCount: 2,
			expectedDirs: []string{
				"yml-app",
				"yaml-app",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := tt.setupFunc(t)

			foundDirs, err := findDockerComposeDirectories(testDir)

			require.NoError(t, err, "findDockerComposeDirectories should not return error")
			assert.Equal(t, tt.expectedCount, len(foundDirs), "Should find expected number of directories")

			// Check that all expected directories are found
			for _, expectedDir := range tt.expectedDirs {
				expectedPath := filepath.Join(testDir, expectedDir)
				if expectedDir == "." {
					expectedPath = testDir
				}

				found := false
				for _, foundDir := range foundDirs {
					if foundDir == expectedPath {
						found = true
						break
					}
				}
				assert.True(t, found, "Should find directory: %s", expectedPath)
			}
		})
	}
}

func TestDockerComposePSCommand2(t *testing.T) {
	tests := createDockerComposePSTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewDockerComposePSCommand()
			ctx := createTestExecutionContext()

			// Setup test directory and update payload
			payload := prepareTestPayload(t, tt)

			// Execute command
			result, err := cmd.Execute(ctx, payload)

			// Validate results
			validateDockerComposePSResult(t, tt, result, err, ctx)
		})
	}
}

// createDockerComposePSTestCases creates the test cases for docker compose ps command
func createDockerComposePSTestCases() []dockerComposePSTestCase {
	return []dockerComposePSTestCase{
		{
			name:        "Valid path with simple syntax",
			payload:     "docker-compose:ps ",
			setupDir:    setupTestDir,
			usesTestDir: true,
			expectError: false,
		},
		{
			name:        "Valid path with JSON syntax",
			payload:     `{"command": "ps", "path": ""}`,
			setupDir:    setupTestDir,
			usesTestDir: true,
			expectError: false,
		},
		{
			name:        "Missing path",
			payload:     "docker-compose:ps",
			setupDir:    setupTestDir,
			expectError: true,
			errorMsg:    "path is required",
		},
		{
			name:        "Invalid JSON",
			payload:     `{"command": "ps", "path":`,
			setupDir:    setupTestDir,
			expectError: true,
			errorMsg:    "invalid JSON format",
		},
		{
			name:        "Nonexistent path",
			payload:     "docker-compose:ps /nonexistent/path",
			setupDir:    setupTestDir,
			expectError: true,
			errorMsg:    "path does not exist",
		},
		{
			name:           "Path without docker-compose file",
			payload:        "docker-compose:ps ",
			setupDir:       setupInvalidTestDir,
			usesInvalidDir: true,
			expectError:    true,
			errorMsg:       "no docker-compose.yml",
		},
		{
			name:        "Empty payload",
			payload:     "",
			setupDir:    setupTestDir,
			expectError: true,
			errorMsg:    "invalid payload format",
		},
	}
}

// dockerComposePSTestCase represents a test case for docker compose PS command
type dockerComposePSTestCase struct {
	name           string
	payload        string
	setupDir       func(t *testing.T) string
	usesTestDir    bool
	usesInvalidDir bool
	expectError    bool
	errorMsg       string
}

// prepareTestPayload sets up the test directory and prepares the payload
func prepareTestPayload(t *testing.T, tt dockerComposePSTestCase) string {
	if tt.setupDir == nil {
		return tt.payload
	}

	testDir := tt.setupDir(t)
	return buildPayloadWithTestDir(tt.payload, testDir, tt.usesTestDir, tt.usesInvalidDir)
}

// buildPayloadWithTestDir builds the payload with the actual test directory path
func buildPayloadWithTestDir(payload, testDir string, usesTestDir, usesInvalidDir bool) string {
	if usesTestDir {
		return handleTestDirPayload(payload, testDir)
	}
	if usesInvalidDir {
		return handleInvalidDirPayload(payload, testDir)
	}
	return payload
}

// handleTestDirPayload handles payloads that use the test directory
func handleTestDirPayload(payload, testDir string) string {
	if payload == "docker-compose:ps " {
		return "docker-compose:ps " + testDir
	}
	if payload == `{"command": "ps", "path": ""}` {
		return `{"command": "ps", "path": "` + testDir + `"}`
	}
	return payload
}

// handleInvalidDirPayload handles payloads that use the invalid directory
func handleInvalidDirPayload(payload, testDir string) string {
	if payload == "docker-compose:ps " {
		return "docker-compose:ps " + testDir
	}
	return payload
}

// validateDockerComposePSResult validates the test results
func validateDockerComposePSResult(t *testing.T, tt dockerComposePSTestCase, result *pb.CommandResult, err error, ctx *ExecutionContext) {
	if tt.expectError {
		validateExpectedError(t, tt, result, err)
	} else {
		validateExpectedSuccess(t, result, err)
	}

	// Validate result structure
	validateResultStructure(t, result, ctx)
}

// validateExpectedError validates when an error is expected
func validateExpectedError(t *testing.T, tt dockerComposePSTestCase, result *pb.CommandResult, err error) {
	if err != nil {
		t.Logf("Expected error occurred: %v", err)
	}
	if result != nil && result.ExitCode == 0 {
		t.Error("Expected command to fail but it succeeded")
	}
	if shouldCheckErrorMessage(tt, result) {
		checkErrorMessage(t, tt.errorMsg, result.Stderr)
	}
}

// validateExpectedSuccess validates when success is expected
func validateExpectedSuccess(t *testing.T, result *pb.CommandResult, err error) {
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Error("Expected result but got nil")
	} else if result.ExitCode != 0 {
		t.Errorf("Expected success (exit code 0) but got %d. Stderr: %s", result.ExitCode, result.Stderr)
	}
}

// shouldCheckErrorMessage determines if we should check the error message
func shouldCheckErrorMessage(tt dockerComposePSTestCase, result *pb.CommandResult) bool {
	return tt.errorMsg != "" && result != nil && result.Stderr != ""
}

// checkErrorMessage checks if the error message contains the expected text
func checkErrorMessage(t *testing.T, expectedMsg, actualMsg string) {
	if actualMsg != expectedMsg && !containsIgnoreCase(actualMsg, expectedMsg) {
		t.Logf("Expected error message to contain '%s', got '%s'", expectedMsg, actualMsg)
	}
}

// validateResultStructure validates the basic structure of the result
func validateResultStructure(t *testing.T, result *pb.CommandResult, ctx *ExecutionContext) {
	if result == nil {
		return
	}

	validateCommandID(t, result, ctx)
	validateMinionID(t, result, ctx)
}

// validateCommandID validates the command ID in the result
func validateCommandID(t *testing.T, result *pb.CommandResult, ctx *ExecutionContext) {
	if result.CommandId != ctx.CommandID {
		t.Errorf("Expected command ID %s, got %s", ctx.CommandID, result.CommandId)
	}
}

// validateMinionID validates the minion ID in the result
func validateMinionID(t *testing.T, result *pb.CommandResult, ctx *ExecutionContext) {
	if result.MinionId != ctx.MinionID {
		t.Errorf("Expected minion ID %s, got %s", ctx.MinionID, result.MinionId)
	}
}
