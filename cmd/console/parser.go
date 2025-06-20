package main

import (
	"fmt"
	"strings"
	"time"

	pb "minexus/protogen"
)

// CommandParser handles command parsing and validation
type CommandParser struct{}

// NewCommandParser creates a new command parser
func NewCommandParser() *CommandParser {
	return &CommandParser{}
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
		if len(args[0]) == 16 && p.isHexString(args[0]) {
			return nil, fmt.Errorf("minion ID detected without target specifier. Did you mean: command-send minion %s %s", args[0], strings.Join(args[1:], " "))
		}

		return nil, fmt.Errorf("invalid target type: %s. Use 'all', 'minion', or 'tag'", args[0])
	}

	// Parse command and determine type
	cmdText, cmdType := p.parseCommandAndType(args[commandStart:])
	if cmdText == "" {
		return nil, fmt.Errorf("command cannot be empty")
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

// isHexString checks if a string contains only hexadecimal characters
func (p *CommandParser) isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ShowSendCommandHelp displays help for the command-send syntax
func (p *CommandParser) ShowSendCommandHelp() string {
	return `Usage:
  command-send all <command>                    - Send to all minions
  command-send minion <id> <command>            - Send to specific minion
  command-send tag <key>=<value> <command>      - Send to minions with tag

Command Types:
  system:info                                   - Get system information
  system:os                                     - Get OS information
  file:get <path>                              - Get file content
  shell <command>                              - Execute shell command
  <command>                                    - Execute as shell command (default)

Examples:
  command-send all system:info
  command-send minion abc123 shell "ls -la"
  command-send tag env=prod "df -h"
  command-send minion abc123 file:get "/etc/hosts"`
}
