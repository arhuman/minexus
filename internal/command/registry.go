package command

import (
	"fmt"
	"strings"
	"sync"

	pb "minexus/protogen"
)

// ExecutableCommand represents a simplified command that can execute and provides metadata
type ExecutableCommand interface {
	Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error)
	Metadata() Definition
}

// Registry provides a cleaner, self-registering command system
type Registry struct {
	commands map[string]ExecutableCommand
	mutex    sync.RWMutex
}

// NewRegistry creates a new registry
func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]ExecutableCommand),
	}
}

// Register adds a command to the registry
func (r *Registry) Register(cmd ExecutableCommand) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	metadata := cmd.Metadata()
	r.commands[metadata.Name] = cmd
}

// Execute executes a command by name
func (r *Registry) Execute(ctx *ExecutionContext, command *pb.Command) (*pb.CommandResult, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// Direct command lookup
	if cmd, exists := r.commands[command.Payload]; exists {
		return cmd.Execute(ctx, command.Payload)
	}

	// Pattern-based lookup for commands like "system:info"
	if strings.Contains(command.Payload, ":") {
		if cmd, exists := r.commands[command.Payload]; exists {
			return cmd.Execute(ctx, command.Payload)
		}
	}

	// Command not found
	return &pb.CommandResult{
		CommandId: ctx.CommandID,
		MinionId:  ctx.MinionID,
		Timestamp: ctx.Timestamp,
		ExitCode:  1,
		Stderr:    fmt.Sprintf("command not found: %s", command.Payload),
	}, fmt.Errorf("command not found: %s", command.Payload)
}

// GetCommand returns a command by name
func (r *Registry) GetCommand(name string) (ExecutableCommand, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	cmd, exists := r.commands[name]
	return cmd, exists
}

// GetAllCommands returns all registered commands
func (r *Registry) GetAllCommands() map[string]ExecutableCommand {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	result := make(map[string]ExecutableCommand)
	for name, cmd := range r.commands {
		result[name] = cmd
	}
	return result
}

// GetCommandsByCategory returns commands grouped by category
func (r *Registry) GetCommandsByCategory() map[string][]ExecutableCommand {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	categories := make(map[string][]ExecutableCommand)
	for _, cmd := range r.commands {
		metadata := cmd.Metadata()
		categories[metadata.Category] = append(categories[metadata.Category], cmd)
	}
	return categories
}

// FormatHelp returns formatted help for all commands
func (r *Registry) FormatHelp() string {
	var help strings.Builder

	help.WriteString("Available Commands:\n")
	help.WriteString("==================\n\n")

	categories := r.GetCommandsByCategory()

	for category, commands := range categories {
		help.WriteString(fmt.Sprintf("--- %s Commands ---\n", strings.Title(category)))

		for _, cmd := range commands {
			metadata := cmd.Metadata()
			help.WriteString(fmt.Sprintf("  %-15s - %s\n", metadata.Name, metadata.Description))
		}
		help.WriteString("\n")
	}

	return help.String()
}

// FormatCommandHelp returns detailed help for a specific command
func (r *Registry) FormatCommandHelp(commandName string) string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	cmd, exists := r.commands[commandName]
	if !exists {
		return fmt.Sprintf("Command '%s' not found. Use 'help' to see available commands.", commandName)
	}

	metadata := cmd.Metadata()
	var help strings.Builder

	help.WriteString(fmt.Sprintf("Command: %s\n", metadata.Name))
	help.WriteString(fmt.Sprintf("Category: %s\n", metadata.Category))
	help.WriteString(fmt.Sprintf("Description: %s\n\n", metadata.Description))

	help.WriteString("Usage:\n")
	help.WriteString(fmt.Sprintf("  %s\n\n", metadata.Usage))

	if len(metadata.Parameters) > 0 {
		help.WriteString("Parameters:\n")
		for _, param := range metadata.Parameters {
			required := ""
			if param.Required {
				required = " (required)"
			}
			defaultVal := ""
			if param.Default != "" {
				defaultVal = fmt.Sprintf(" [default: %s]", param.Default)
			}
			help.WriteString(fmt.Sprintf("  %-20s %s - %s%s%s\n",
				param.Name, param.Type, param.Description, required, defaultVal))
		}
		help.WriteString("\n")
	}

	if len(metadata.Examples) > 0 {
		help.WriteString("Examples:\n")
		for i, example := range metadata.Examples {
			help.WriteString(fmt.Sprintf("  %d. %s\n", i+1, example.Description))
			help.WriteString(fmt.Sprintf("     %s\n", example.Command))
			if example.Expected != "" {
				help.WriteString(fmt.Sprintf("     Expected: %s\n", example.Expected))
			}
			help.WriteString("\n")
		}
	}

	if len(metadata.Notes) > 0 {
		help.WriteString("Notes:\n")
		for _, note := range metadata.Notes {
			help.WriteString(fmt.Sprintf("  • %s\n", note))
		}
	}

	return help.String()
}
