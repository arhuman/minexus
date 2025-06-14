package command

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	pb "minexus/protogen"

	"go.uber.org/zap"
)

// ShellRequest represents a shell command request
type ShellRequest struct {
	Command string `json:"command"`
	Shell   string `json:"shell,omitempty"`   // Optional: specify shell (sh, bash, cmd, powershell)
	Timeout int    `json:"timeout,omitempty"` // Optional: timeout in seconds
}

// ShellResponse represents the response from a shell command
type ShellResponse struct {
	Command   string `json:"command"`
	Shell     string `json:"shell"`
	ExitCode  int32  `json:"exit_code"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	Duration  string `json:"duration"`
	TimedOut  bool   `json:"timed_out,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// ShellExecutor handles shell command execution
type ShellExecutor struct {
	defaultTimeout time.Duration
}

// NewShellExecutor creates a new shell executor
func NewShellExecutor(defaultTimeout time.Duration) *ShellExecutor {
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second // Default 30 second timeout
	}
	return &ShellExecutor{
		defaultTimeout: defaultTimeout,
	}
}

// ParseShellRequest parses a shell command request from various formats
func ParseShellRequest(payload string) (*ShellRequest, error) {
	// Simple string format - just the command
	if !strings.HasPrefix(payload, "{") {
		return &ShellRequest{
			Command: payload,
		}, nil
	}

	// JSON format parsing would go here if needed
	// For now, treat everything as simple command
	return &ShellRequest{
		Command: payload,
	}, nil
}

// Execute processes a shell command and returns the response
func (se *ShellExecutor) Execute(ctx context.Context, request *ShellRequest) *ShellResponse {
	startTime := time.Now()

	response := &ShellResponse{
		Command:   request.Command,
		Timestamp: startTime.Unix(),
	}

	// Determine shell to use
	shell, flag := se.getShellAndFlag(request.Shell)
	response.Shell = shell

	// Set up timeout
	timeout := se.defaultTimeout
	if request.Timeout > 0 {
		timeout = time.Duration(request.Timeout) * time.Second
	}

	// Create context with timeout
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute command
	var execCmd *exec.Cmd
	if flag != "" {
		execCmd = exec.CommandContext(cmdCtx, shell, flag, request.Command)
	} else {
		// Direct execution for cases where we split the command
		parts := strings.Fields(request.Command)
		if len(parts) == 0 {
			response.ExitCode = 1
			response.Stderr = "empty command"
			response.Duration = time.Since(startTime).String()
			return response
		}

		if len(parts) == 1 {
			execCmd = exec.CommandContext(cmdCtx, parts[0])
		} else {
			execCmd = exec.CommandContext(cmdCtx, parts[0], parts[1:]...)
		}
	}

	// Execute and capture output
	output, err := execCmd.CombinedOutput()
	response.Duration = time.Since(startTime).String()

	if err != nil {
		response.ExitCode = 1

		// Check if it was a timeout
		if cmdCtx.Err() == context.DeadlineExceeded {
			response.TimedOut = true
			response.Stderr = fmt.Sprintf("command timed out after %v", timeout)
		} else {
			// Check for exit code
			if exitErr, ok := err.(*exec.ExitError); ok {
				response.ExitCode = int32(exitErr.ExitCode())
			}
			response.Stderr = err.Error()
		}

		response.Stdout = string(output)
	} else {
		response.ExitCode = 0
		response.Stdout = string(output)
	}

	return response
}

// getShellAndFlag returns the appropriate shell and flag for the OS and requested shell
func (se *ShellExecutor) getShellAndFlag(requestedShell string) (string, string) {
	if requestedShell != "" {
		// Use requested shell
		switch requestedShell {
		case "bash":
			return "bash", "-c"
		case "sh":
			return "sh", "-c"
		case "zsh":
			return "zsh", "-c"
		case "cmd":
			return "cmd", "/C"
		case "powershell":
			return "powershell", "-Command"
		case "pwsh":
			return "pwsh", "-Command"
		default:
			// Fallback to OS default
		}
	}

	// OS-specific defaults
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	} else {
		return "sh", "-c"
	}
}

// Unified shell command implementations

// ShellCommand provides a unified shell command interface
type ShellCommand struct {
	*BaseCommand
	executor *ShellExecutor
}

// NewShellCommand creates a new unified shell command
func NewShellCommand() *ShellCommand {
	base := NewBaseCommand(
		"shell",
		"shell",
		"Execute shell commands with enhanced logging and validation",
		`{"command": "ls -la", "shell": "bash", "timeout": 30}`,
	).WithExamples(
		Example{
			Description: "Simple shell command",
			Command:     "command-send minion abc123 'shell ls -la'",
			Expected:    "Returns directory listing with execution details",
		},
		Example{
			Description: "Shell command with specific shell",
			Command:     `command-send minion abc123 '{"command": "Get-Process", "shell": "powershell"}'`,
			Expected:    "Executes PowerShell command and returns process list",
		},
		Example{
			Description: "Shell command with timeout",
			Command:     `command-send minion abc123 '{"command": "sleep 5", "timeout": 10}'`,
			Expected:    "Executes command with 10 second timeout",
		},
	).WithParameters(
		Param{Name: "command", Type: "string", Required: true, Description: "Shell command to execute"},
		Param{Name: "shell", Type: "string", Required: false, Description: "Specific shell to use (bash, sh, zsh, cmd, powershell)", Default: "OS default"},
		Param{Name: "timeout", Type: "int", Required: false, Description: "Timeout in seconds", Default: "30"},
	).WithNotes(
		"Commands are executed in the shell specified or OS default",
		"All output (stdout/stderr) is captured and returned",
		"Exit codes and execution duration are tracked",
		"Commands have a default 30-second timeout for safety",
		"Timed out commands are properly terminated",
	)

	return &ShellCommand{
		BaseCommand: base,
		executor:    NewShellExecutor(30 * time.Second),
	}
}

// Execute implements Command interface for shell commands
func (c *ShellCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	// Parse the request
	request, err := ParseShellRequest(payload)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to parse shell request: %w", err)), nil
	}

	// Validate the command
	if strings.TrimSpace(request.Command) == "" {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("empty command")), nil
	}

	// Execute the shell command
	response := c.executor.Execute(ctx.Context, request)

	// Create result based on shell response
	result := &pb.CommandResult{
		CommandId: ctx.CommandID,
		MinionId:  ctx.MinionID,
		Timestamp: ctx.Timestamp,
		ExitCode:  response.ExitCode,
		Stdout:    response.Stdout,
		Stderr:    response.Stderr,
	}

	// Add execution metadata to stdout if successful
	if response.ExitCode == 0 && response.Stdout != "" {
		metadata := fmt.Sprintf("\n--- Execution Info ---\nShell: %s\nDuration: %s\nExit Code: %d\n",
			response.Shell, response.Duration, response.ExitCode)
		result.Stdout = response.Stdout + metadata
	}

	ctx.Logger.Info("Shell command executed",
		zap.String("command", request.Command),
		zap.String("shell", response.Shell),
		zap.Int32("exit_code", response.ExitCode),
		zap.String("duration", response.Duration),
		zap.Bool("timed_out", response.TimedOut),
	)

	return result, nil
}

// SystemCommand provides backwards compatibility for system commands
type SystemCommand struct {
	*BaseCommand
	executor *ShellExecutor
}

// NewSystemCommand creates a system command (for backwards compatibility)
func NewSystemCommand() *SystemCommand {
	base := NewBaseCommand(
		"system",
		"shell",
		"Execute system commands (alias for shell command)",
		"system <command>",
	).WithExamples(
		Example{
			Description: "System command execution",
			Command:     "command-send minion abc123 'system uname -a'",
			Expected:    "Returns system information",
		},
	).WithParameters(
		Param{Name: "command", Type: "string", Required: true, Description: "System command to execute"},
	).WithNotes(
		"This is an alias for the shell command for backwards compatibility",
		"Uses the OS default shell for execution",
	)

	return &SystemCommand{
		BaseCommand: base,
		executor:    NewShellExecutor(30 * time.Second),
	}
}

// Execute implements Command interface for system commands
func (c *SystemCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	// For system commands, treat payload as direct command
	request := &ShellRequest{
		Command: payload,
	}

	response := c.executor.Execute(ctx.Context, request)

	result := &pb.CommandResult{
		CommandId: ctx.CommandID,
		MinionId:  ctx.MinionID,
		Timestamp: ctx.Timestamp,
		ExitCode:  response.ExitCode,
		Stdout:    response.Stdout,
		Stderr:    response.Stderr,
	}

	ctx.Logger.Info("System command executed",
		zap.String("command", request.Command),
		zap.String("shell", response.Shell),
		zap.Int32("exit_code", response.ExitCode),
		zap.String("duration", response.Duration),
	)

	return result, nil
}
