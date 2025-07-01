package command

import (
	pb "github.com/arhuman/minexus/protogen"
)

// BaseCommand provides common functionality for all commands
type BaseCommand struct {
	name        string
	category    string
	description string
	usage       string
	examples    []Example
	parameters  []Param
	notes       []string
}

// NewBaseCommand creates a new base command with metadata
func NewBaseCommand(name, category, description, usage string) *BaseCommand {
	return &BaseCommand{
		name:        name,
		category:    category,
		description: description,
		usage:       usage,
		examples:    make([]Example, 0),
		parameters:  make([]Param, 0),
		notes:       make([]string, 0),
	}
}

// Metadata returns the command's definition
func (b *BaseCommand) Metadata() Definition {
	return Definition{
		Name:        b.name,
		Category:    b.category,
		Description: b.description,
		Usage:       b.usage,
		Examples:    b.examples,
		Parameters:  b.parameters,
		Notes:       b.notes,
	}
}

// WithExamples adds examples to the command
func (b *BaseCommand) WithExamples(examples ...Example) *BaseCommand {
	b.examples = append(b.examples, examples...)
	return b
}

// WithParameters adds parameters to the command
func (b *BaseCommand) WithParameters(params ...Param) *BaseCommand {
	b.parameters = append(b.parameters, params...)
	return b
}

// WithNotes adds notes to the command
func (b *BaseCommand) WithNotes(notes ...string) *BaseCommand {
	b.notes = append(b.notes, notes...)
	return b
}

// CreateSuccessResult creates a standardized success result
func (b *BaseCommand) CreateSuccessResult(ctx *ExecutionContext, output string) *pb.CommandResult {
	return &pb.CommandResult{
		CommandId: ctx.CommandID,
		MinionId:  ctx.MinionID,
		Timestamp: ctx.Timestamp,
		ExitCode:  0,
		Stdout:    output,
	}
}

// CreateErrorResult creates a standardized error result
func (b *BaseCommand) CreateErrorResult(ctx *ExecutionContext, err error) *pb.CommandResult {
	return &pb.CommandResult{
		CommandId: ctx.CommandID,
		MinionId:  ctx.MinionID,
		Timestamp: ctx.Timestamp,
		ExitCode:  1,
		Stderr:    err.Error(),
	}
}

// CreateErrorResultWithCode creates an error result with custom exit code
func (b *BaseCommand) CreateErrorResultWithCode(ctx *ExecutionContext, err error, exitCode int32) *pb.CommandResult {
	return &pb.CommandResult{
		CommandId: ctx.CommandID,
		MinionId:  ctx.MinionID,
		Timestamp: ctx.Timestamp,
		ExitCode:  exitCode,
		Stderr:    err.Error(),
	}
}
