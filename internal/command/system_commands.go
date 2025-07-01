package command

import (
	"fmt"
	"runtime"

	pb "github.com/arhuman/minexus/protogen"
)

// SystemInfoCommand provides system information
type SystemInfoCommand struct {
	*BaseCommand
}

// NewSystemInfoCommand creates a new system info command
func NewSystemInfoCommand() *SystemInfoCommand {
	base := NewBaseCommand(
		"system:info",
		"system",
		"Get system information including memory, uptime, and load",
		"system:info",
	).WithExamples(
		Example{
			Description: "Get system information",
			Command:     "command-send all system:info",
			Expected:    "Returns uptime, memory usage, and system load",
		},
	)

	return &SystemInfoCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *SystemInfoCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	memStats := new(runtime.MemStats)
	runtime.ReadMemStats(memStats)

	output := fmt.Sprintf("OS: %s\nArch: %s\nTotal Memory: %d MB\nAllocated Memory: %d MB\nGoroutines: %d",
		runtime.GOOS, runtime.GOARCH, memStats.TotalAlloc/1024/1024, memStats.Alloc/1024/1024, runtime.NumGoroutine())

	return c.BaseCommand.CreateSuccessResult(ctx, output), nil
}

// SystemOSCommand provides OS information
type SystemOSCommand struct {
	*BaseCommand
}

// NewSystemOSCommand creates a new system OS command
func NewSystemOSCommand() *SystemOSCommand {
	base := NewBaseCommand(
		"system:os",
		"system",
		"Get operating system and architecture information",
		"system:os",
	).WithExamples(
		Example{
			Description: "Get OS information",
			Command:     "command-send all system:os",
			Expected:    "Returns OS name and architecture (e.g., 'OS: linux, Arch: amd64')",
		},
	)

	return &SystemOSCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *SystemOSCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	output := fmt.Sprintf("OS: %s\nArch: %s", runtime.GOOS, runtime.GOARCH)
	return c.BaseCommand.CreateSuccessResult(ctx, output), nil
}
