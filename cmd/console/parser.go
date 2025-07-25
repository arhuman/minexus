package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/arhuman/minexus/internal/command"
	"github.com/arhuman/minexus/internal/util"
	pb "github.com/arhuman/minexus/protogen"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// CommandParser handles command parsing and validation
type CommandParser struct {
	registry *command.Registry
}

// NewCommandParser creates a new command parser with registry access
func NewCommandParser(registry *command.Registry) *CommandParser {
	return &CommandParser{
		registry: registry,
	}
}

// ParsedCommand represents a parsed command with its targeting and type information
type ParsedCommand struct {
	Request     *pb.CommandRequest
	CommandText string
	CommandType pb.CommandType
}

// ParseCommand parses console command arguments into a structured command request
func (p *CommandParser) ParseCommand(args []string) (*ParsedCommand, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("missing command arguments")
	}

	// New syntax: command-send <target-type> [target-specifier] <command>
	var req pb.CommandRequest
	var commandStart int

	switch args[0] {
	case "all":
		if len(args) < 2 {
			return nil, fmt.Errorf("missing command for 'all' target")
		}
		// Target all minions
		commandStart = 1

	case "minion":
		if len(args) < 3 {
			return nil, fmt.Errorf("missing minion ID or command")
		}
		// Target specific minion
		req.MinionIds = []string{args[1]}
		commandStart = 2

	case "tag":
		if len(args) < 3 {
			return nil, fmt.Errorf("missing tag selector or command")
		}
		// Target by tag
		tagParts := strings.SplitN(args[1], "=", 2)
		if len(tagParts) != 2 {
			return nil, fmt.Errorf("tag format should be key=value")
		}

		req.TagSelector = &pb.TagSelector{
			Rules: []*pb.TagMatch{
				{
					Key: tagParts[0],
					Condition: &pb.TagMatch_Equals{
						Equals: tagParts[1],
					},
				},
			},
		}
		commandStart = 2

	default:
		// Check if it looks like a minion ID (common mistake)
		if len(args[0]) == 16 && util.IsHexString(args[0]) {
			return nil, fmt.Errorf("minion ID detected without target specifier. Did you mean: command-send minion %s %s", args[0], strings.Join(args[1:], " "))
		}

		return nil, fmt.Errorf("invalid target type: %s. Use 'all', 'minion', or 'tag'", args[0])
	}

	// Parse command and determine type
	cmdText, cmdType := p.parseCommandAndType(args[commandStart:])
	if cmdText == "" {
		return nil, fmt.Errorf("command cannot be empty")
	}

	// Validate structured commands (commands with ':' prefix)
	if err := p.validateStructuredCommand(cmdText); err != nil {
		return nil, err
	}

	req.Command = &pb.Command{
		Id:      fmt.Sprintf("cmd-%d", time.Now().UnixNano()),
		Type:    cmdType,
		Payload: cmdText,
	}

	return &ParsedCommand{
		Request:     &req,
		CommandText: cmdText,
		CommandType: cmdType,
	}, nil
}

// parseCommandAndType determines the command type and formats the payload
func (p *CommandParser) parseCommandAndType(args []string) (string, pb.CommandType) {
	if len(args) == 0 {
		return "", pb.CommandType_SYSTEM
	}

	// Check if the first argument is a JSON command (even if malformed)
	if len(args) == 1 && strings.HasPrefix(args[0], "{") {
		return p.parseJSONCommand(args[0])
	}

	// Check for explicit command type prefix
	if len(args) >= 2 {
		switch args[0] {
		case "shell":
			// Explicit shell command
			return strings.Join(args[1:], " "), pb.CommandType_SYSTEM
		case "system:info", "system:os":
			// System commands don't need shell prefix
			return args[0], pb.CommandType_SYSTEM
		}
	}

	// Check if it's a known system command
	fullCmd := strings.Join(args, " ")
	if strings.HasPrefix(fullCmd, "system:") {
		return fullCmd, pb.CommandType_SYSTEM
	}

	// Check if it's a file command
	if strings.HasPrefix(fullCmd, "file:") {
		return fullCmd, pb.CommandType_INTERNAL
	}

	// Default to shell command
	return fullCmd, pb.CommandType_SYSTEM
}

// parseJSONCommand parses JSON-formatted commands and converts them to structured format
func (p *CommandParser) parseJSONCommand(jsonStr string) (string, pb.CommandType) {
	var jsonCmd map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &jsonCmd); err != nil {
		// If JSON parsing fails, treat as regular shell command
		return jsonStr, pb.CommandType_SYSTEM
	}

	// Extract the command type
	cmdInterface, exists := jsonCmd["command"]
	if !exists {
		// No command field, treat as shell command
		return jsonStr, pb.CommandType_SYSTEM
	}

	command, ok := cmdInterface.(string)
	if !ok {
		// Command is not a string, treat as shell command
		return jsonStr, pb.CommandType_SYSTEM
	}

	// Handle different command types
	switch command {
	case "ps", "up", "down", "logs", "build", "pull", "push":
		// Docker compose commands
		return p.formatDockerComposeCommand(jsonCmd, command)
	case "get", "copy", "move", "delete":
		// File commands
		return p.formatFileCommand(jsonCmd, command), pb.CommandType_INTERNAL
	case "info", "os":
		// System commands
		return p.formatSystemCommand(jsonCmd, command), pb.CommandType_SYSTEM
	default:
		// Unknown command, treat as shell command
		return jsonStr, pb.CommandType_SYSTEM
	}
}

// formatDockerComposeCommand formats Docker Compose JSON commands to structured format
func (p *CommandParser) formatDockerComposeCommand(jsonCmd map[string]interface{}, command string) (string, pb.CommandType) {
	var parts []string
	parts = append(parts, "docker-compose:"+command)

	// Add path if provided
	if path, exists := jsonCmd["path"]; exists {
		if pathStr, ok := path.(string); ok {
			parts = append(parts, pathStr)
		}
	}

	// Add service if provided
	if service, exists := jsonCmd["service"]; exists {
		if serviceStr, ok := service.(string); ok {
			parts = append(parts, "--service", serviceStr)
		}
	}

	// Add build flag if provided
	if build, exists := jsonCmd["build"]; exists {
		if buildBool, ok := build.(bool); ok && buildBool {
			parts = append(parts, "--build")
		}
	}

	// Add detach flag if provided
	if detach, exists := jsonCmd["detach"]; exists {
		if detachBool, ok := detach.(bool); ok && detachBool {
			parts = append(parts, "--detach")
		}
	}

	return strings.Join(parts, " "), pb.CommandType_SYSTEM
}

// formatFileCommand formats file JSON commands to structured format
func (p *CommandParser) formatFileCommand(jsonCmd map[string]interface{}, command string) string {
	var parts []string
	parts = append(parts, "file:"+command)

	// Add path if provided
	if path, exists := jsonCmd["path"]; exists {
		if pathStr, ok := path.(string); ok {
			parts = append(parts, pathStr)
		}
	}

	// Add destination for copy/move commands
	if dest, exists := jsonCmd["dest"]; exists {
		if destStr, ok := dest.(string); ok {
			parts = append(parts, destStr)
		}
	}

	return strings.Join(parts, " ")
}

// formatSystemCommand formats system JSON commands to structured format
func (p *CommandParser) formatSystemCommand(jsonCmd map[string]interface{}, command string) string {
	return "system:" + command
}

// validateStructuredCommand validates that structured commands (with ':' prefix) are valid
func (p *CommandParser) validateStructuredCommand(cmdText string) error {
	// Allow non-structured commands (no colon) to pass through
	if !strings.Contains(cmdText, ":") {
		return nil
	}

	// Skip validation for JSON commands or other shell commands that contain colons
	// Only validate actual structured commands that follow the "prefix:subcommand" pattern
	if strings.HasPrefix(cmdText, "{") || !p.isStructuredCommand(cmdText) {
		return nil
	}

	// Parse the command prefix and subcommand
	parts := strings.SplitN(cmdText, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid command format: %s", cmdText)
	}

	prefix := parts[0]
	subcommand := parts[1]

	// For shell commands, allow any subcommand (special case)
	if prefix == "shell" {
		return nil
	}

	// Extract just the subcommand part (before any arguments)
	subcommandParts := strings.Fields(subcommand)
	if len(subcommandParts) == 0 {
		return fmt.Errorf("empty %s subcommand", prefix)
	}

	actualSubcommand := subcommandParts[0]
	fullCommand := prefix + ":" + actualSubcommand

	// Check if the exact command is registered
	if _, exists := p.registry.GetCommand(fullCommand); exists {
		return nil
	}

	// If not found, provide helpful error message with available commands
	allCommands := p.registry.GetAllCommands()
	var validCommands []string
	prefixCommands := make(map[string][]string)

	for cmdName := range allCommands {
		if strings.HasPrefix(cmdName, prefix+":") {
			cmdParts := strings.SplitN(cmdName, ":", 2)
			if len(cmdParts) == 2 {
				prefixCommands[cmdParts[0]] = append(prefixCommands[cmdParts[0]], cmdParts[1])
				validCommands = append(validCommands, cmdName)
			}
		}
	}

	if len(validCommands) == 0 {
		// Get all available prefixes
		var availablePrefixes []string
		for cmdName := range allCommands {
			if strings.Contains(cmdName, ":") {
				cmdParts := strings.SplitN(cmdName, ":", 2)
				if len(cmdParts) == 2 {
					found := false
					for _, existing := range availablePrefixes {
						if existing == cmdParts[0] {
							found = true
							break
						}
					}
					if !found {
						availablePrefixes = append(availablePrefixes, cmdParts[0])
					}
				}
			}
		}
		return fmt.Errorf("unknown command prefix: %s. Valid prefixes: %v", prefix, availablePrefixes)
	}

	return fmt.Errorf("invalid %s subcommand: %s. Valid subcommands: %v", prefix, actualSubcommand, prefixCommands[prefix])
}

// isStructuredCommand determines if a command text represents a structured command
// vs a shell command that happens to contain colons
func (p *CommandParser) isStructuredCommand(cmdText string) bool {
	// Get the first part before the colon
	parts := strings.SplitN(cmdText, ":", 2)
	if len(parts) != 2 {
		return false
	}

	prefix := parts[0]

	// Check if this prefix exists in our registry
	allCommands := p.registry.GetAllCommands()
	for cmdName := range allCommands {
		if strings.HasPrefix(cmdName, prefix+":") {
			return true
		}
	}

	return false
}

// ShowSendCommandHelp displays help for the command-send syntax
func (p *CommandParser) ShowSendCommandHelp() string {
	helpText := `Usage:
  command-send all <command>                    - Send to all minions
  command-send minion <id> <command>            - Send to specific minion
  command-send tag <key>=<value> <command>      - Send to minions with tag

Available Commands:
`

	// If registry is available, show actual registered commands
	if p.registry == nil {
		fmt.Println("Warning: Command registry is not available. Help may be incomplete.")
	}
	categories := p.registry.GetCommandsByCategory()
	for category, commands := range categories {
		if len(commands) > 0 {
			helpText += fmt.Sprintf("\n--- %s Commands ---\n", cases.Title(language.English).String(category))
			for _, cmd := range commands {
				metadata := cmd.Metadata()
				helpText += fmt.Sprintf("  %-30s - %s\n", metadata.Name, metadata.Description)
				if len(metadata.Examples) > 0 {
					for _, example := range metadata.Examples {
						helpText += fmt.Sprintf("    Example: %s\n", example)
					}
				}
			}
		}
	}

	helpText += `

Note: Only registered commands are accepted. Invalid commands will be rejected at parse time.`

	return helpText
}
