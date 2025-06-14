package command

import (
	"fmt"
	pb "minexus/protogen"

	"go.uber.org/zap"
)

// LoggingLevelCommand gets the current logging level
type LoggingLevelCommand struct {
	*BaseCommand
}

// NewLoggingLevelCommand creates a new logging level command
func NewLoggingLevelCommand() *LoggingLevelCommand {
	base := NewBaseCommand(
		"logging:level",
		"logging",
		"Get the current logging level",
		"logging:level",
	).WithExamples(
		Example{
			Description: "Get current logging level",
			Command:     "command-send all logging:level",
			Expected:    "Returns current log level (debug, info, warn, error)",
		},
	)

	return &LoggingLevelCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *LoggingLevelCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	if ctx.AtomicLevel == nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("logging level not available")), nil
	}

	level := ctx.AtomicLevel.Level().String()
	output := fmt.Sprintf("Current logging level: %s", level)
	return c.BaseCommand.CreateSuccessResult(ctx, output), nil
}

// LoggingIncreaseCommand increases the logging level
type LoggingIncreaseCommand struct {
	*BaseCommand
}

// NewLoggingIncreaseCommand creates a new logging increase command
func NewLoggingIncreaseCommand() *LoggingIncreaseCommand {
	base := NewBaseCommand(
		"logging:increase",
		"logging",
		"Increase the logging level (debug->info->warn->error)",
		"logging:increase",
	).WithExamples(
		Example{
			Description: "Increase logging level",
			Command:     "command-send all logging:increase",
			Expected:    "Returns new logging level",
		},
	)

	return &LoggingIncreaseCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *LoggingIncreaseCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	if ctx.AtomicLevel == nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("logging level not available")), nil
	}

	currentLevel := ctx.AtomicLevel.Level()
	var newLevel zap.AtomicLevel

	// Increase means more verbose (lower level numbers)
	switch currentLevel {
	case zap.ErrorLevel:
		newLevel = zap.NewAtomicLevelAt(zap.WarnLevel)
	case zap.WarnLevel:
		newLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	case zap.InfoLevel:
		newLevel = zap.NewAtomicLevelAt(zap.DebugLevel)
	case zap.DebugLevel:
		return c.BaseCommand.CreateSuccessResult(ctx, "Already at highest level: debug"), nil
	default:
		newLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	ctx.AtomicLevel.SetLevel(newLevel.Level())
	output := fmt.Sprintf("Logging level increased from %s to %s", currentLevel.String(), newLevel.Level().String())
	return c.BaseCommand.CreateSuccessResult(ctx, output), nil
}

// LoggingDecreaseCommand decreases the logging level
type LoggingDecreaseCommand struct {
	*BaseCommand
}

// NewLoggingDecreaseCommand creates a new logging decrease command
func NewLoggingDecreaseCommand() *LoggingDecreaseCommand {
	base := NewBaseCommand(
		"logging:decrease",
		"logging",
		"Decrease the logging level (error->warn->info->debug)",
		"logging:decrease",
	).WithExamples(
		Example{
			Description: "Decrease logging level",
			Command:     "command-send all logging:decrease",
			Expected:    "Returns new logging level",
		},
	).WithNotes(
		"Decreasing logging level reduces verbosity",
	)

	return &LoggingDecreaseCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *LoggingDecreaseCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	if ctx.AtomicLevel == nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("logging level not available")), nil
	}

	currentLevel := ctx.AtomicLevel.Level()
	var newLevel zap.AtomicLevel

	// Decrease means less verbose (higher level numbers)
	switch currentLevel {
	case zap.DebugLevel:
		newLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	case zap.InfoLevel:
		newLevel = zap.NewAtomicLevelAt(zap.WarnLevel)
	case zap.WarnLevel:
		newLevel = zap.NewAtomicLevelAt(zap.ErrorLevel)
	case zap.ErrorLevel:
		return c.BaseCommand.CreateSuccessResult(ctx, "Already at lowest level: error"), nil
	default:
		newLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	ctx.AtomicLevel.SetLevel(newLevel.Level())
	output := fmt.Sprintf("Logging level decreased from %s to %s", currentLevel.String(), newLevel.Level().String())
	return c.BaseCommand.CreateSuccessResult(ctx, output), nil
}
