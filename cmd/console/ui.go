package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"minexus/internal/command"
	"minexus/internal/version"

	"github.com/chzyer/readline"
	"go.uber.org/zap"
)

// UIManager handles all user interface operations
type UIManager struct {
	rl       *readline.Instance
	logger   *zap.Logger
	registry *command.Registry
}

// NewUIManager creates a new UI manager
func NewUIManager(logger *zap.Logger, registry *command.Registry) *UIManager {
	ui := &UIManager{
		logger:   logger,
		registry: registry,
	}

	// Set up readline with completion and history
	ui.setupReadline()
	return ui
}

// setupReadline configures the readline instance with completion and history
func (ui *UIManager) setupReadline() {
	// Create completer function
	completer := ui.createCompleter()

	// Set up history file path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	historyFile := filepath.Join(homeDir, ".minexus_history")

	// Create readline config
	config := &readline.Config{
		Prompt:              "minexus> ",
		HistoryFile:         historyFile,
		AutoComplete:        completer,
		InterruptPrompt:     "^C",
		EOFPrompt:           "quit",
		HistorySearchFold:   true,
		FuncFilterInputRune: ui.filterInput,
	}

	// Create readline instance
	rl, err := readline.NewEx(config)
	if err != nil {
		ui.logger.Error("Failed to create readline instance", zap.Error(err))
		// Fallback to a basic readline without advanced features
		basicRL, fallbackErr := readline.New("minexus> ")
		if fallbackErr != nil {
			ui.logger.Error("Failed to create basic readline instance", zap.Error(fallbackErr))
			// For testing environments or when no TTY available, create a minimal mock
			ui.rl = nil
			return
		}
		rl = basicRL
	}

	ui.rl = rl
}

// filterInput filters input runes for readline
func (ui *UIManager) filterInput(r rune) (rune, bool) {
	switch r {
	// Block Ctrl+Z, it's useless for our console
	case readline.CharCtrlZ:
		return r, false
	}
	return r, true
}

// createCompleter creates a tab completion function
func (ui *UIManager) createCompleter() *readline.PrefixCompleter {
	// Main console commands
	consoleCommands := []readline.PrefixCompleterInterface{
		readline.PcItem("help"),
		readline.PcItem("h"),
		readline.PcItem("version"),
		readline.PcItem("v"),
		readline.PcItem("minion-list"),
		readline.PcItem("lm"),
		readline.PcItem("tag-list"),
		readline.PcItem("lt"),
		readline.PcItem("result-get"),
		readline.PcItem("results"),
		readline.PcItem("tag-set"),
		readline.PcItem("tag-update"),
		readline.PcItem("clear"),
		readline.PcItem("history"),
		readline.PcItem("quit"),
		readline.PcItem("exit"),
	}

	// Command-send with subcommands
	commandSendItem := readline.PcItem("command-send",
		readline.PcItem("all"),
		readline.PcItem("minion"),
		readline.PcItem("tag"),
	)
	consoleCommands = append(consoleCommands, commandSendItem)

	// Also add "cmd" alias
	cmdItem := readline.PcItem("cmd",
		readline.PcItem("all"),
		readline.PcItem("minion"),
		readline.PcItem("tag"),
	)
	consoleCommands = append(consoleCommands, cmdItem)

	return readline.NewPrefixCompleter(consoleCommands...)
}

// ReadLine reads a line of input from the user
func (ui *UIManager) ReadLine() (string, error) {
	if ui.rl == nil {
		// Fallback for testing environments without TTY
		return "", io.EOF
	}
	return ui.rl.Readline()
}

// Shutdown gracefully closes the readline instance
func (ui *UIManager) Shutdown() {
	if ui.rl != nil {
		ui.rl.Close()
	}
}

// ShowWelcome displays the welcome message
func (ui *UIManager) ShowWelcome() {
	fmt.Println("=== Minexus Console ===")
	fmt.Printf("Version: %s\n", version.Short())
	fmt.Println("Type 'help' for available commands, use arrow keys for history, or 'quit' to exit")
	fmt.Println()
}

// ShowHelp displays available commands or detailed help for a specific command
func (ui *UIManager) ShowHelp(args []string) {
	// If a specific command is requested, show detailed help
	if len(args) > 0 {
		commandName := args[0]
		fmt.Println(ui.registry.FormatCommandHelp(commandName))
		return
	}

	// Show general help
	fmt.Println("=== Console Commands ===")
	fmt.Println("  help, h [command]                          - Show this help message or help for specific command")
	fmt.Println("  version, v                                 - Show version information")
	fmt.Println("  minion-list, lm                            - List all connected minions with last seen time")
	fmt.Println("  tag-list, lt                               - List all available tags")
	fmt.Println("  command-send all <cmd>                     - Send command to all minions")
	fmt.Println("  command-send minion <id> <cmd>             - Send command to specific minion")
	fmt.Println("  command-send tag <key>=<value> <cmd>       - Send command to minions with tag")
	fmt.Println("  result-get <cmd-id>                        - Get results for a command ID")
	fmt.Println("  tag-set <minion-id> <key>=<value> [...]    - Set tags for a minion (replaces all)")
	fmt.Println("  tag-update <minion-id> +<key>=<value> -<key> [...] - Update tags for a minion")
	fmt.Println("  clear                                      - Clear screen")
	fmt.Println("  history                                    - Show command history")
	fmt.Println("  quit, exit                                 - Exit the console")
	fmt.Println()
	fmt.Println("=== Command Examples ===")
	fmt.Println("  command-send all system:info               - Get system info from all minions")
	fmt.Println("  command-send minion abc123 \"ls -la\"        - Run shell command on specific minion")
	fmt.Println("  command-send tag env=prod \"df -h\"          - Check disk usage on production servers")
	fmt.Println("  command-send minion abc123 file:get \"/etc/hosts\" - Get file content from minion")
	fmt.Println()

	// Show minion commands
	fmt.Println(ui.registry.FormatHelp())
}

// ShowVersion displays version information
func (ui *UIManager) ShowVersion() {
	fmt.Printf("Console %s\n", version.Info())
}

// ClearScreen clears the terminal screen
func (ui *UIManager) ClearScreen() {
	fmt.Print("\033[2J\033[H")
}

// ShowHistory displays the command history
func (ui *UIManager) ShowHistory() {
	fmt.Println("Command history is available using arrow keys:")
	fmt.Println("  ↑ (Up Arrow)    - Previous command")
	fmt.Println("  ↓ (Down Arrow)  - Next command")
	fmt.Println("  Ctrl+R          - Search history")
	fmt.Println()
	fmt.Println("History is automatically saved to ~/.minexus_history")
}

// AddToHistory adds a command to the readline history
func (ui *UIManager) AddToHistory(command string) {
	if ui.rl != nil {
		// Save the command to history file
		ui.rl.SaveHistory(command)
	}
}

// PrintError prints an error message to the console
func (ui *UIManager) PrintError(msg string) {
	fmt.Printf("Error: %s\n", msg)
}

// PrintWarning prints a warning message to the console
func (ui *UIManager) PrintWarning(msg string) {
	fmt.Printf("⚠️  %s\n", msg)
}

// PrintSuccess prints a success message to the console
func (ui *UIManager) PrintSuccess(msg string) {
	fmt.Printf("✓ %s\n", msg)
}

// PrintInfo prints an informational message to the console
func (ui *UIManager) PrintInfo(msg string) {
	fmt.Println(msg)
}

// HandleInterrupt handles interrupt signals (Ctrl+C)
func (ui *UIManager) HandleInterrupt(line string) bool {
	if len(line) == 0 {
		fmt.Println("\nUse 'quit' or 'exit' to leave the console")
		return true // Continue the loop
	}
	return true // Continue the loop
}

// HandleEOF handles EOF signals (Ctrl+D)
func (ui *UIManager) HandleEOF() {
	fmt.Println("\nGoodbye!")
}

// IsEOF checks if the error is EOF
func (ui *UIManager) IsEOF(err error) bool {
	return err == io.EOF
}

// IsInterrupt checks if the error is an interrupt
func (ui *UIManager) IsInterrupt(err error) bool {
	return err == readline.ErrInterrupt
}

// PrintGoodbye prints the goodbye message
func (ui *UIManager) PrintGoodbye() {
	fmt.Println("Goodbye!")
}

// PrintBlankLine prints a blank line for spacing
func (ui *UIManager) PrintBlankLine() {
	fmt.Println()
}
