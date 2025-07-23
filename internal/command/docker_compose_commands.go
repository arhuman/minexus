package command

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pb "github.com/arhuman/minexus/protogen"
)

// DockerComposeRequest represents the JSON structure for docker-compose commands
type DockerComposeRequest struct {
	Command string `json:"command"`
	Path    string `json:"path"`
	Service string `json:"service,omitempty"`
	Build   bool   `json:"build,omitempty"`
}

// DockerComposePSCommand lists docker-compose services
type DockerComposePSCommand struct {
	*BaseCommand
}

// NewDockerComposePSCommand creates a new docker-compose ps command
func NewDockerComposePSCommand() *DockerComposePSCommand {
	base := NewBaseCommand(
		"docker-compose:ps",
		"docker",
		"List docker-compose services and their status",
		"docker-compose:ps <path>",
	).WithParameters(
		Param{Name: "path", Type: "string", Required: true, Description: "Path to directory containing docker-compose.yml"},
	).WithExamples(
		Example{
			Description: "List services in /opt/myapp",
			Command:     `command-send minion web-01 '{"command": "ps", "path": "/opt/myapp"}'`,
			Expected:    "Lists all services defined in /opt/myapp/docker-compose.yml with their status",
		},
		Example{
			Description: "Simple syntax for current directory",
			Command:     "command-send minion web-01 \"docker-compose:ps .\"",
			Expected:    "Lists services in current directory",
		},
	).WithNotes(
		"Supports both JSON format and simple 'docker-compose:ps <path>' syntax",
		"Requires docker-compose to be installed on the minion",
		"Path must contain a docker-compose.yml or docker-compose.yaml file",
	)

	return &DockerComposePSCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *DockerComposePSCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	request, err := parseDockerComposePayload(payload)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid payload: %w", err)), nil
	}

	if request.Path == "" {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("path is required")), nil
	}

	// Validate path exists and contains docker-compose file
	if err := validateDockerComposePath(request.Path); err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, err), nil
	}

	// Execute docker-compose ps command
	cmd := exec.CommandContext(ctx.Context, "docker", "compose", "-f", getComposeFile(request.Path), "ps")
	cmd.Dir = request.Path

	output, err := cmd.CombinedOutput()
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("docker-compose ps failed: %w\nOutput: %s", err, string(output))), nil
	}

	return c.BaseCommand.CreateSuccessResult(ctx, string(output)), nil
}

// DockerComposeUpCommand starts docker-compose services
type DockerComposeUpCommand struct {
	*BaseCommand
}

// NewDockerComposeUpCommand creates a new docker-compose up command
func NewDockerComposeUpCommand() *DockerComposeUpCommand {
	base := NewBaseCommand(
		"docker-compose:up",
		"docker",
		"Start docker-compose services",
		"docker-compose:up <path> [--build] [service]",
	).WithParameters(
		Param{Name: "path", Type: "string", Required: true, Description: "Path to directory containing docker-compose.yml"},
		Param{Name: "service", Type: "string", Required: false, Description: "Specific service to start (optional)"},
		Param{Name: "build", Type: "boolean", Required: false, Description: "Force rebuild of images", Default: "false"},
	).WithExamples(
		Example{
			Description: "Start all services",
			Command:     `command-send minion web-01 '{"command": "up", "path": "/opt/myapp"}'`,
			Expected:    "Starts all services defined in docker-compose.yml",
		},
		Example{
			Description: "Start specific service with rebuild",
			Command:     `command-send minion web-01 '{"command": "up", "path": "/opt/myapp", "service": "web", "build": true}'`,
			Expected:    "Rebuilds and starts only the 'web' service",
		},
		Example{
			Description: "Simple syntax for all services",
			Command:     "command-send minion web-01 \"docker-compose:up /opt/myapp\"",
			Expected:    "Starts all services in /opt/myapp",
		},
	).WithNotes(
		"Runs in detached mode (-d flag) by default",
		"Use 'build: true' to force image rebuilding (equivalent to --build)",
		"Service parameter is optional - omit to start all services",
	)

	return &DockerComposeUpCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *DockerComposeUpCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	request, err := parseDockerComposePayload(payload)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid payload: %w", err)), nil
	}

	if request.Path == "" {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("path is required")), nil
	}

	// Validate path exists and contains docker-compose file
	if err := validateDockerComposePath(request.Path); err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, err), nil
	}

	// Build command arguments
	args := []string{"-f", getComposeFile(request.Path), "up", "-d"}
	if request.Build {
		args = append(args, "--build")
	}
	if request.Service != "" {
		args = append(args, request.Service)
	}

	// Execute docker-compose up command
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.CommandContext(ctx.Context, "docker", fullArgs...)
	cmd.Dir = request.Path

	output, err := cmd.CombinedOutput()
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("docker-compose up failed: %w\nOutput: %s", err, string(output))), nil
	}

	return c.BaseCommand.CreateSuccessResult(ctx, string(output)), nil
}

// DockerComposeDownCommand stops docker-compose services
type DockerComposeDownCommand struct {
	*BaseCommand
}

// NewDockerComposeDownCommand creates a new docker-compose down command
func NewDockerComposeDownCommand() *DockerComposeDownCommand {
	base := NewBaseCommand(
		"docker-compose:down",
		"docker",
		"Stop and remove docker-compose services",
		"docker-compose:down <path> [service]",
	).WithParameters(
		Param{Name: "path", Type: "string", Required: true, Description: "Path to directory containing docker-compose.yml"},
		Param{Name: "service", Type: "string", Required: false, Description: "Specific service to stop (optional)"},
	).WithExamples(
		Example{
			Description: "Stop all services",
			Command:     `command-send minion web-01 '{"command": "down", "path": "/opt/myapp"}'`,
			Expected:    "Stops and removes all services and networks",
		},
		Example{
			Description: "Stop specific service",
			Command:     `command-send minion web-01 '{"command": "down", "path": "/opt/myapp", "service": "web"}'`,
			Expected:    "Stops only the 'web' service",
		},
		Example{
			Description: "Simple syntax for all services",
			Command:     "command-send minion web-01 \"docker-compose:down /opt/myapp\"",
			Expected:    "Stops all services in /opt/myapp",
		},
	).WithNotes(
		"When stopping all services, also removes networks created by docker-compose",
		"When stopping a specific service, only that service is affected",
		"Containers are removed, not just stopped",
	)

	return &DockerComposeDownCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *DockerComposeDownCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	request, err := parseDockerComposePayload(payload)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid payload: %w", err)), nil
	}

	if request.Path == "" {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("path is required")), nil
	}

	// Validate path exists and contains docker-compose file
	if err := validateDockerComposePath(request.Path); err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, err), nil
	}

	// Build command arguments
	var args []string
	if request.Service != "" {
		// Stop specific service using 'stop' and 'rm' commands
		args = []string{"-f", getComposeFile(request.Path), "stop", request.Service}
	} else {
		// Stop all services using 'down' command
		args = []string{"-f", getComposeFile(request.Path), "down"}
	}

	// Execute docker-compose command
	fullArgs := append([]string{"compose"}, args...)
	cmd := exec.CommandContext(ctx.Context, "docker", fullArgs...)
	cmd.Dir = request.Path

	output, err := cmd.CombinedOutput()
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("docker-compose down failed: %w\nOutput: %s", err, string(output))), nil
	}

	// If stopping specific service, also remove it
	if request.Service != "" {
		rmArgs := []string{"-f", getComposeFile(request.Path), "rm", "-f", request.Service}
		rmFullArgs := append([]string{"compose"}, rmArgs...)
		rmCmd := exec.CommandContext(ctx.Context, "docker", rmFullArgs...)
		rmCmd.Dir = request.Path

		rmOutput, rmErr := rmCmd.CombinedOutput()
		if rmErr != nil {
			// Log warning but don't fail the command
			output = append(output, []byte(fmt.Sprintf("\nWarning: Failed to remove service containers: %s", string(rmOutput)))...)
		} else {
			output = append(output, []byte("\nService containers removed successfully")...)
		}
	}

	return c.BaseCommand.CreateSuccessResult(ctx, string(output)), nil
}

// DockerComposeFindCommand finds all directories containing docker-compose.yml files
type DockerComposeFindCommand struct {
	*BaseCommand
}

// NewDockerComposeFindCommand creates a new docker-compose find command
func NewDockerComposeFindCommand() *DockerComposeFindCommand {
	base := NewBaseCommand(
		"docker-compose:find",
		"docker",
		"Find all directories containing docker-compose.yml files under a given path",
		"docker-compose:find <path>",
	).WithParameters(
		Param{Name: "path", Type: "string", Required: true, Description: "Root path to search for docker-compose.yml files"},
	).WithExamples(
		Example{
			Description: "Find all docker-compose.yml files under /opt",
			Command:     `command-send minion web-01 '{"command": "find", "path": "/opt"}'`,
			Expected:    "Returns list of directories containing docker-compose.yml files under /opt",
		},
		Example{
			Description: "Simple syntax for current directory",
			Command:     "command-send minion web-01 \"docker-compose:find .\"",
			Expected:    "Lists all directories with docker-compose files under current directory",
		},
		Example{
			Description: "Search home directory",
			Command:     "command-send minion web-01 \"docker-compose:find /home/user\"",
			Expected:    "Recursively finds docker-compose files in user's home directory",
		},
	).WithNotes(
		"Searches recursively through all subdirectories",
		"Looks for both docker-compose.yml and docker-compose.yaml files",
		"Returns the directory paths containing compose files, not the file paths themselves",
		"May take time for large directory trees",
	)

	return &DockerComposeFindCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *DockerComposeFindCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	request, err := parseDockerComposePayload(payload)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid payload: %w", err)), nil
	}

	if request.Path == "" {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("path is required")), nil
	}

	// Check if the root path exists
	if _, err := os.Stat(request.Path); os.IsNotExist(err) {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("path does not exist: %s", request.Path)), nil
	}

	// Find all directories containing docker-compose files
	foundDirs, err := findDockerComposeDirectories(request.Path)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("error searching for docker-compose files: %w", err)), nil
	}

	// Format the output
	if len(foundDirs) == 0 {
		return c.BaseCommand.CreateSuccessResult(ctx, fmt.Sprintf("No docker-compose.yml files found under: %s", request.Path)), nil
	}

	output := fmt.Sprintf("Found %d director%s containing docker-compose.yml files under %s:\n\n", 
		len(foundDirs), 
		func() string { if len(foundDirs) == 1 { return "y" }; return "ies" }(),
		request.Path)
	
	for _, dir := range foundDirs {
		output += fmt.Sprintf("  %s\n", dir)
	}

	return c.BaseCommand.CreateSuccessResult(ctx, output), nil
}

// DockerComposeViewCommand displays the content of docker-compose.yml file in the specified path
type DockerComposeViewCommand struct {
	*BaseCommand
}

// NewDockerComposeViewCommand creates a new docker-compose view command
func NewDockerComposeViewCommand() *DockerComposeViewCommand {
	base := NewBaseCommand(
		"docker-compose:view",
		"docker",
		"Display the content of docker-compose.yml file in the specified path",
		"docker-compose:view <path>",
	).WithParameters(
		Param{Name: "path", Type: "string", Required: true, Description: "Path to directory containing docker-compose.yml file"},
	).WithExamples(
		Example{
			Description: "View docker-compose.yml content in /opt/myapp",
			Command:     `command-send minion web-01 '{"command": "view", "path": "/opt/myapp"}'`,
			Expected:    "Displays the content of /opt/myapp/docker-compose.yml",
		},
		Example{
			Description: "Simple syntax for current directory",
			Command:     "command-send minion web-01 \"docker-compose:view .\"",
			Expected:    "Shows docker-compose.yml content in current directory",
		},
		Example{
			Description: "View specific application configuration",
			Command:     "command-send minion web-01 \"docker-compose:view /home/user/myproject\"",
			Expected:    "Displays the docker-compose configuration for the project",
		},
	).WithNotes(
		"Only displays the docker-compose.yml file in the specified directory (not subdirectories)",
		"Looks for both docker-compose.yml and docker-compose.yaml files",
		"Path must contain a valid docker-compose file",
		"File content is displayed as-is without modification",
	)

	return &DockerComposeViewCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *DockerComposeViewCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	request, err := parseDockerComposePayload(payload)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid payload: %w", err)), nil
	}

	if request.Path == "" {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("path is required")), nil
	}

	// Validate path exists and contains docker-compose file
	if err := validateDockerComposePath(request.Path); err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, err), nil
	}

	// Get the compose file path
	composeFile := getComposeFile(request.Path)
	
	// Read the file content
	content, err := os.ReadFile(composeFile)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to read docker-compose file: %w", err)), nil
	}

	// Format the output with file path and content
	output := fmt.Sprintf("Content of %s:\n\n%s", composeFile, string(content))

	return c.BaseCommand.CreateSuccessResult(ctx, output), nil
}

// DockerComposeCommand is a unified command that routes to specific docker-compose operations
type DockerComposeCommand struct {
	*BaseCommand
}

// NewDockerComposeCommand creates a new unified docker-compose command
func NewDockerComposeCommand() *DockerComposeCommand {
	base := NewBaseCommand(
		"docker-compose",
		"docker",
		"Unified docker-compose command handler",
		"Use docker-compose:ps, docker-compose:up, docker-compose:down, or docker-compose:find instead",
	).WithNotes(
		"This is a router command - use specific subcommands instead",
		"Available subcommands: ps, up, down, find",
	)

	return &DockerComposeCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface (should not be called directly)
func (c *DockerComposeCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("use specific docker-compose subcommands: docker-compose:ps, docker-compose:up, docker-compose:down, docker-compose:find")), nil
}

// Helper functions

// parseDockerComposePayload parses either JSON format or simple string format
func parseDockerComposePayload(payload string) (*DockerComposeRequest, error) {
	// Remove the command prefix if present (e.g., "docker-compose:ps /path")
	payload = strings.TrimSpace(payload)

	// Try to parse as JSON first
	if strings.HasPrefix(payload, "{") {
		var request DockerComposeRequest
		if err := json.Unmarshal([]byte(payload), &request); err != nil {
			return nil, fmt.Errorf("invalid JSON format: %w", err)
		}
		return &request, nil
	}

	// Parse simple format: "docker-compose:command path [options]"
	parts := strings.Fields(payload)
	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid payload format")
	}

	request := &DockerComposeRequest{}

	// Extract command from the payload (e.g., "docker-compose:ps" -> "ps")
	if strings.Contains(parts[0], ":") {
		cmdParts := strings.Split(parts[0], ":")
		if len(cmdParts) == 2 {
			request.Command = cmdParts[1]
			// Remove the first part since it's the command
			if len(parts) > 1 {
				parts = parts[1:]
			} else {
				return nil, fmt.Errorf("path is required")
			}
		}
	}

	// First remaining part should be the path
	if len(parts) > 0 {
		request.Path = parts[0]
	}

	// Parse additional options
	for i := 1; i < len(parts); i++ {
		arg := parts[i]
		if arg == "--build" {
			request.Build = true
		} else if request.Service == "" {
			// Assume it's a service name
			request.Service = arg
		}
	}

	return request, nil
}

// validateDockerComposePath checks if the path exists and contains a docker-compose file
func validateDockerComposePath(path string) error {
	// Check if path exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", path)
	}

	// Check for docker-compose file
	composeFile := getComposeFile(path)
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return fmt.Errorf("no docker-compose.yml or docker-compose.yaml found in: %s", path)
	}

	return nil
}

// getComposeFile returns the path to the docker-compose file, preferring .yml over .yaml
func getComposeFile(basePath string) string {
	ymlPath := filepath.Join(basePath, "docker-compose.yml")
	yamlPath := filepath.Join(basePath, "docker-compose.yaml")

	// Prefer .yml over .yaml
	if _, err := os.Stat(ymlPath); err == nil {
		return ymlPath
	}
	if _, err := os.Stat(yamlPath); err == nil {
		return yamlPath
	}

	// Return .yml as default (will fail validation later)
	return ymlPath
}

// findDockerComposeDirectories recursively searches for directories containing docker-compose.yml or docker-compose.yaml files
func findDockerComposeDirectories(rootPath string) ([]string, error) {
	var foundDirs []string

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip directories we can't access
			return nil
		}

		// Check if current file is a docker-compose file
		if !info.IsDir() && (info.Name() == "docker-compose.yml" || info.Name() == "docker-compose.yaml") {
			// Add the directory containing this file
			dir := filepath.Dir(path)
			foundDirs = append(foundDirs, dir)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return foundDirs, nil
}
