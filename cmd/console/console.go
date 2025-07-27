package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/arhuman/minexus/internal/command"
	"github.com/arhuman/minexus/internal/config"
	"github.com/arhuman/minexus/internal/logging"
	"github.com/arhuman/minexus/internal/util"
	"github.com/arhuman/minexus/internal/version"

	// Import with correct package name
	pb "github.com/arhuman/minexus/protogen"

	"go.uber.org/zap"
)

// CommandStatus tracks the status of a command for each minion
type CommandStatus struct {
	CommandID string
	Statuses  map[string]string // minion_id -> status
	Timestamp time.Time
}

type Console struct {
	client        pb.ConsoleServiceClient
	grpc          *GRPCClient
	ui            *UIManager
	parser        *CommandParser
	logger        *zap.Logger
	commandStatus map[string]*CommandStatus // command_id -> status
}

// NewConsole creates a new console instance
func NewConsole(grpcClient *GRPCClient, logger *zap.Logger) *Console {
	registry := command.SetupCommands(15 * time.Second) // Default 15s timeout for console commands

	console := &Console{
		client:        grpcClient.client,
		grpc:          grpcClient,
		ui:            NewUIManager(logger, registry),
		parser:        NewCommandParser(registry),
		logger:        logger,
		commandStatus: make(map[string]*CommandStatus),
	}

	return console
}

// Shutdown gracefully closes the console components
func (c *Console) Shutdown() {
	if c.ui != nil {
		c.ui.Shutdown()
	}
}

// Start begins the REPL loop
func (c *Console) Start() {
	defer c.ui.Shutdown()

	c.ui.ShowWelcome()

	for {
		line, err := c.ui.ReadLine()
		if err != nil {
			if c.ui.IsInterrupt(err) {
				if c.ui.HandleInterrupt(line) {
					continue
				}
				break
			} else if c.ui.IsEOF(err) {
				c.ui.HandleEOF()
				break
			}
			c.logger.Error("Error reading input", zap.Error(err))
			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse command and arguments with proper shell-style quoting support
		parts, err := util.ParseCommandLine(line)
		if err != nil {
			c.ui.PrintError(fmt.Sprintf("Error parsing command: %v", err))
			continue
		}
		if len(parts) == 0 {
			continue
		}

		command := strings.ToLower(parts[0])
		args := parts[1:]

		// Handle command
		if command == "quit" || command == "exit" {
			c.ui.PrintGoodbye()
			break
		}

		c.handleCommand(command, args)
		c.ui.PrintBlankLine()
	}
}

// handleCommand processes a single command
func (c *Console) handleCommand(command string, args []string) {
	ctx := context.Background()

	switch command {
	case "help", "h":
		c.ui.ShowHelp(args)

	case "command-status":
		c.showCommandStatus(ctx, args)

	case "version", "v":
		c.ui.ShowVersion()

	case "minion-list", "lm":
		c.listMinions(ctx)

	case "tag-list", "lt":
		c.listTags(ctx)

	case "command-send", "cmd":
		c.sendCommand(ctx, args)

	case "result-get", "results":
		c.getResults(ctx, args)

	case "tag-set":
		c.setTags(ctx, args)

	case "tag-update":
		c.updateTags(ctx, args)

	case "clear":
		c.ui.ClearScreen()

	case "history":
		c.ui.ShowHistory()

	default:
		c.ui.PrintError(fmt.Sprintf("Unknown command: %s. Type 'help' for available commands", command))
	}
}

// listMinions lists all connected minions
func (c *Console) listMinions(ctx context.Context) {
	c.logger.Debug("Attempting to list minions from nexus server")
	response, err := c.grpc.ListMinions(ctx)
	if err != nil {
		c.logger.Error("Failed to list minions from nexus server", zap.Error(err))
		c.ui.PrintError(fmt.Sprintf("Error listing minions: %v", err))
		return
	}
	c.logger.Debug("Successfully received minion list", zap.Int("count", len(response.Minions)))

	if len(response.Minions) == 0 {
		c.logger.Info("No minions are currently connected to nexus server")
		c.ui.PrintInfo("No minions connected - Commands will not execute until minions connect")
		return
	}

	fmt.Printf("Connected minions (%d):\n", len(response.Minions))
	fmt.Println("ID                                   | Hostname          | IP             | OS       | Last Seen        | Tags")
	fmt.Println("------------------------------------ | ----------------- | -------------- | -------- | ---------------- | ----")

	for _, minion := range response.Minions {
		tags := util.FormatTags(minion.Tags)
		lastSeen := util.FormatLastSeen(minion.LastSeen)
		fmt.Printf("%-36s | %-17s | %-14s | %-8s | %-16s | %s\n",
			minion.Id, minion.Hostname, minion.Ip, minion.Os, lastSeen, tags)
	}
}

// listTags lists all available tags
func (c *Console) listTags(ctx context.Context) {
	response, err := c.grpc.ListTags(ctx)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Error listing tags: %v", err))
		return
	}

	if len(response.Tags) == 0 {
		c.ui.PrintInfo("No tags found")
		return
	}

	fmt.Printf("Available tags (%d):\n", len(response.Tags))
	for _, tag := range response.Tags {
		fmt.Printf("  %s\n", tag)
	}
}

// sendCommand sends a command to minions using the CommandParser
func (c *Console) sendCommand(ctx context.Context, args []string) {
	if len(args) == 0 {
		c.ui.PrintInfo(c.parser.ShowSendCommandHelp())
		return
	}

	c.logger.Debug("Attempting to send command", zap.Strings("args", args))

	// Parse the command using CommandParser
	parsed, err := c.parser.ParseCommand(args)
	if err != nil {
		c.logger.Error("Failed to parse command", zap.Strings("args", args), zap.Error(err))
		c.ui.PrintError(err.Error())
		return
	}

	c.logger.Debug("Command parsed successfully",
		zap.String("command_payload", parsed.Request.Command.Payload),
		zap.String("command_id", parsed.Request.Command.Id),
		zap.Strings("minion_ids", parsed.Request.MinionIds),
		zap.Any("tag_selector", parsed.Request.TagSelector))

	// Send command
	response, err := c.grpc.SendCommand(ctx, parsed.Request)
	if err != nil {
		c.logger.Error("Failed to send command to nexus server",
			zap.String("command_payload", parsed.Request.Command.Payload),
			zap.Error(err))
		c.ui.PrintError(fmt.Sprintf("Error sending command: %v", err))
		return
	}

	c.logger.Debug("Command sent to nexus server",
		zap.String("command_id", response.CommandId),
		zap.Bool("accepted", response.Accepted))

	if response.Accepted {
		// Initialize command status tracking
		status := &CommandStatus{
			CommandID: response.CommandId,
			Statuses:  make(map[string]string),
			Timestamp: time.Now(),
		}

		// Set initial status for targeted minions
		if len(parsed.Request.MinionIds) > 0 {
			for _, minionID := range parsed.Request.MinionIds {
				status.Statuses[minionID] = "PENDING"
			}
		} else {
			// For 'all' target, get list of minions and set pending status
			minions, err := c.grpc.ListMinions(ctx)
			if err == nil {
				for _, minion := range minions.Minions {
					status.Statuses[minion.Id] = "PENDING"
				}
			}
		}

		c.commandStatus[response.CommandId] = status

		fmt.Printf("Command dispatched successfully. Command ID: %s\n", response.CommandId)

		// Check if command result are available immediately **in database**
		// if yes returns them immediately
		// (with a header saying that further results will be available later through result-get)
		resultsReq := &pb.ResultRequest{
			CommandId: response.CommandId,
		}
		resultsResponse, err := c.grpc.GetCommandResults(ctx, resultsReq)
		if err == nil && len(resultsResponse.Results) > 0 {
			fmt.Printf("Immediate results (%d):\n", len(resultsResponse.Results))
			fmt.Println("Minion ID                            | Exit Code | Output")
			fmt.Println("------------------------------------ | --------- | ------")
			for _, result := range resultsResponse.Results {
				timestamp := time.Unix(result.Timestamp, 0).Format("15:04:05")
				output := strings.ReplaceAll(result.Stdout, "\n", "\\n")
				if len(output) > 50 {
					output = output[:47] + "..."
				}
				fmt.Printf("%-36s | %-9d | %s [%s]\n",
					result.MinionId, result.ExitCode, output, timestamp)
				if result.Stderr != "" {
					stderr := strings.ReplaceAll(result.Stderr, "\n", "\\n")
					if len(stderr) > 50 {
						stderr = stderr[:47] + "..."
					}
					fmt.Printf("%-36s | %-9s | STDERR: %s\n", "", "", stderr)
				}
			}
		} else {
			c.ui.PrintInfo("No immediate results available, check later with 'result-get " + response.CommandId + "'")
		}
		// Add command to history
		resultCmd := fmt.Sprintf("result-get %s", response.CommandId)
		c.ui.AddToHistory(resultCmd)
	} else {
		c.ui.PrintInfo("Command was not accepted")
	}
}

// getResults gets command execution results
func (c *Console) getResults(ctx context.Context, args []string) {
	if len(args) != 1 {
		c.ui.PrintError("Usage: result-get <command-id>")
		return
	}

	commandID := args[0]
	c.logger.Debug("Attempting to get results for command", zap.String("command_id", commandID))

	req := &pb.ResultRequest{
		CommandId: commandID,
	}

	response, err := c.grpc.GetCommandResults(ctx, req)
	if err != nil {
		c.logger.Error("Failed to get command results from nexus server",
			zap.String("command_id", commandID),
			zap.Error(err))
		c.ui.PrintError(fmt.Sprintf("Error getting results: %v", err))
		return
	}

	c.logger.Debug("Received results response",
		zap.String("command_id", commandID),
		zap.Int("result_count", len(response.Results)))

	if len(response.Results) == 0 {
		c.logger.Info("No results available yet for command", zap.String("command_id", commandID))

		// Check if we have any minions connected to help diagnose the issue
		minions, err := c.grpc.ListMinions(ctx)
		if err != nil {
			c.logger.Error("Failed to list minions for diagnostics", zap.Error(err))
			c.ui.PrintInfo("No results available yet")
		} else {
			c.logger.Info("Minion count for diagnostics", zap.Int("minion_count", len(minions.Minions)))
			if len(minions.Minions) == 0 {
				c.ui.PrintInfo("No results available yet - Diagnostic: No minions are currently connected")
			} else {
				c.ui.PrintInfo(fmt.Sprintf("No results available yet - Diagnostic: %d minion(s) connected, command may still be executing", len(minions.Minions)))
			}
		}
		return
	}

	// Update command status for received results
	if status, ok := c.commandStatus[commandID]; ok {
		for _, result := range response.Results {
			if result.ExitCode == 0 {
				status.Statuses[result.MinionId] = "COMPLETED"
			} else {
				status.Statuses[result.MinionId] = "FAILED"
			}
		}
	}

	fmt.Printf("Command results (%d):\n", len(response.Results))
	fmt.Println("Minion ID                            | Exit Code | Output")
	fmt.Println("------------------------------------ | --------- | ------")

	for _, result := range response.Results {
		timestamp := time.Unix(result.Timestamp, 0).Format("15:04:05")
		output := strings.ReplaceAll(result.Stdout, "\n", "\\n")
		if len(output) > 50 {
			output = output[:47] + "..."
		}

		fmt.Printf("%-36s | %-9d | %s [%s]\n",
			result.MinionId, result.ExitCode, output, timestamp)

		if result.Stderr != "" {
			stderr := strings.ReplaceAll(result.Stderr, "\n", "\\n")
			if len(stderr) > 50 {
				stderr = stderr[:47] + "..."
			}
			fmt.Printf("%-36s | %-9s | STDERR: %s\n", "", "", stderr)
		}
	}
}

// setTags sets tags for a minion (replaces all existing tags)
func (c *Console) setTags(ctx context.Context, args []string) {
	if len(args) < 2 {
		c.ui.PrintError("Usage: tag-set <minion-id> <key>=<value> [<key>=<value>...]")
		return
	}

	minionID := args[0]
	tags := make(map[string]string)

	// Parse tag assignments
	for _, arg := range args[1:] {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			c.ui.PrintError(fmt.Sprintf("Invalid tag format '%s'. Use key=value", arg))
			return
		}
		tags[parts[0]] = parts[1]
	}

	req := &pb.SetTagsRequest{
		MinionId: minionID,
		Tags:     tags,
	}

	response, err := c.grpc.SetTags(ctx, req)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Error setting tags: %v", err))
		return
	}

	if response.Success {
		c.ui.PrintSuccess(fmt.Sprintf("Tags set successfully for minion %s", minionID))
	} else {
		c.ui.PrintError("Failed to set tags")
	}
}

// updateTags updates tags for a minion (add/remove specific tags)
func (c *Console) updateTags(ctx context.Context, args []string) {
	logger, start := logging.FuncLogger(c.logger, "Console.updateTags")
	defer logging.FuncExit(logger, start)

	if len(args) < 2 {
		logger.Warn("Invalid arguments provided")
		c.ui.PrintError("Usage: tag-update <minion-id> +<key>=<value> -<key> [...]")
		fmt.Println("  +<key>=<value> : Add or update tag")
		fmt.Println("  -<key>         : Remove tag")
		return
	}

	minionID := args[0]
	addTags := make(map[string]string)
	var removeKeys []string

	// Parse tag operations
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "+") {
			// Add tag
			tagExpr := arg[1:]
			parts := strings.SplitN(tagExpr, "=", 2)
			if len(parts) != 2 {
				logger.Warn("Invalid add tag format",
					zap.String("minion_id", minionID),
					zap.String("tag", arg))
				c.ui.PrintError(fmt.Sprintf("Invalid add tag format '%s'. Use +key=value", arg))
				return
			}
			addTags[parts[0]] = parts[1]
		} else if strings.HasPrefix(arg, "-") {
			// Remove tag
			key := arg[1:]
			if key == "" {
				logger.Warn("Invalid remove tag format",
					zap.String("minion_id", minionID),
					zap.String("tag", arg))
				c.ui.PrintError(fmt.Sprintf("Invalid remove tag format '%s'. Use -key", arg))
				return
			}
			removeKeys = append(removeKeys, key)
		} else {
			logger.Warn("Invalid tag operation",
				zap.String("minion_id", minionID),
				zap.String("tag", arg))
			c.ui.PrintError(fmt.Sprintf("Tag operation must start with + or -: '%s'", arg))
			return
		}
	}

	logger.Debug("Updating tags",
		zap.String("minion_id", minionID),
		zap.Any("add_tags", addTags),
		zap.Strings("remove_keys", removeKeys))

	req := &pb.UpdateTagsRequest{
		MinionId:   minionID,
		Add:        addTags,
		RemoveKeys: removeKeys,
	}

	response, err := c.grpc.UpdateTags(ctx, req)
	if err != nil {
		logger.Error("Failed to update tags",
			zap.String("minion_id", minionID),
			zap.Error(err))
		c.ui.PrintError(fmt.Sprintf("Error updating tags: %v", err))
		return
	}

	if response.Success {
		logger.Info("Tags updated successfully",
			zap.String("minion_id", minionID))
		c.ui.PrintSuccess(fmt.Sprintf("Tags updated successfully for minion %s", minionID))
	} else {
		logger.Warn("Failed to update tags",
			zap.String("minion_id", minionID))
		c.ui.PrintError("Failed to update tags")
	}
}

// showCommandStatus displays the current status of commands with reduced cyclomatic complexity
func (c *Console) showCommandStatus(ctx context.Context, args []string) {
	if len(args) == 0 {
		c.ui.PrintError("Usage: command-status <all | minion <minion-id> | stats>")
		return
	}

	// Get list of all minions for statistics
	minions, err := c.grpc.ListMinions(ctx)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Error getting minion list: %v", err))
		return
	}

	// Delegate to specific handlers based on command type
	switch args[0] {
	case "all":
		c.showAllCommandsStatus()
	case "minion":
		c.showMinionCommandStatus(ctx, args, minions.Minions)
	case "stats":
		c.showCommandStatsStatus(minions.Minions)
	default:
		c.ui.PrintError("Invalid target type. Use 'all', 'minion <minion-id>', or 'stats'")
	}
}

// showAllCommandsStatus shows status for all commands
func (c *Console) showAllCommandsStatus() {
	if len(c.commandStatus) == 0 {
		c.ui.PrintInfo("No commands have been executed")
		return
	}

	fmt.Println("Command Status Overview:")
	fmt.Println("Command ID                            | Pending | Received | Executing | Completed | Failed | Total")
	fmt.Println("------------------------------------ | -------- | -------- | --------- | --------- | ------- | -----")

	totalCounts := c.initializeStatusCounts()

	for cmdID, status := range c.commandStatus {
		counts := c.calculateCommandCounts(status)
		c.updateTotalCounts(totalCounts, counts)
		c.printCommandRow(cmdID, counts)
	}

	c.printTotalRow(totalCounts)
}

// showMinionCommandStatus shows detailed status for a specific minion
func (c *Console) showMinionCommandStatus(ctx context.Context, args []string, minions []*pb.HostInfo) {
	if len(args) < 2 {
		c.ui.PrintError("Usage: command-status minion <minion-id>")
		return
	}

	minionID := args[1]
	minionInfo := c.findMinionInfo(minionID, minions)

	c.printMinionHeader(minionID, minionInfo)
	c.printMinionCommandsTable(ctx, minionID)
}

// showCommandStatsStatus shows statistics per minion
func (c *Console) showCommandStatsStatus(minions []*pb.HostInfo) {
	fmt.Println("Command Statistics by Minion:")
	fmt.Println("Minion ID                            | Hostname          | Total | Completed | Failed | Success Rate")
	fmt.Println("------------------------------------ | ----------------- | ----- | --------- | ------ | ------------")

	minionStats := c.initializeMinionStats(minions)
	c.collectMinionStatistics(minionStats)
	totalCommands, totalCompleted, totalFailed := c.printMinionStats(minions, minionStats)
	c.printStatsTotal(totalCommands, totalCompleted, totalFailed)
}

// initializeStatusCounts creates and returns initial status counts
func (c *Console) initializeStatusCounts() map[string]int {
	return map[string]int{
		"PENDING":   0,
		"RECEIVED":  0,
		"EXECUTING": 0,
		"COMPLETED": 0,
		"FAILED":    0,
	}
}

// calculateCommandCounts calculates counts for a single command
func (c *Console) calculateCommandCounts(status *CommandStatus) map[string]int {
	counts := c.initializeStatusCounts()
	for _, st := range status.Statuses {
		counts[st]++
	}
	return counts
}

// updateTotalCounts updates total counts with command counts
func (c *Console) updateTotalCounts(totalCounts, counts map[string]int) {
	for status, count := range counts {
		totalCounts[status] += count
	}
}

// printCommandRow prints a single command status row
func (c *Console) printCommandRow(cmdID string, counts map[string]int) {
	total := c.sumCounts(counts)
	fmt.Printf("%-36s | %-8d | %-8d | %-9d | %-9d | %-7d | %-5d\n",
		cmdID,
		counts["PENDING"],
		counts["RECEIVED"],
		counts["EXECUTING"],
		counts["COMPLETED"],
		counts["FAILED"],
		total)
}

// printTotalRow prints the total summary row
func (c *Console) printTotalRow(totalCounts map[string]int) {
	totalSum := c.sumCounts(totalCounts)
	fmt.Println("------------------------------------ | -------- | -------- | --------- | --------- | ------- | -----")
	fmt.Printf("%-36s | %-8d | %-8d | %-9d | %-9d | %-7d | %-5d\n",
		"TOTAL",
		totalCounts["PENDING"],
		totalCounts["RECEIVED"],
		totalCounts["EXECUTING"],
		totalCounts["COMPLETED"],
		totalCounts["FAILED"],
		totalSum)
}

// sumCounts calculates the sum of all counts
func (c *Console) sumCounts(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

// findMinionInfo finds minion info by ID
func (c *Console) findMinionInfo(minionID string, minions []*pb.HostInfo) *pb.HostInfo {
	for _, m := range minions {
		if m.Id == minionID {
			return m
		}
	}
	return nil
}

// printMinionHeader prints the header for minion command status
func (c *Console) printMinionHeader(minionID string, minionInfo *pb.HostInfo) {
	if minionInfo != nil {
		fmt.Printf("Command status for minion %s (%s):\n", minionID, minionInfo.Hostname)
	} else {
		fmt.Printf("Command status for minion %s:\n", minionID)
	}
}

// printMinionCommandsTable prints the table of commands for a minion
func (c *Console) printMinionCommandsTable(ctx context.Context, minionID string) {
	fmt.Println("Command ID                            | Status    | Exit Code | Timestamp  | Command")
	fmt.Println("------------------------------------ | --------- | --------- | ---------- | --------")

	found := false
	for cmdID, status := range c.commandStatus {
		if st, exists := status.Statuses[minionID]; exists {
			found = true
			exitCode := c.getExitCodeForMinion(ctx, cmdID, minionID)
			fmt.Printf("%-36s | %-9s | %-9d | %-10s | %s\n",
				cmdID,
				st,
				exitCode,
				status.Timestamp.Format("15:04:05"),
				"") // command field is empty in original
		}
	}

	if !found {
		c.ui.PrintInfo("No commands found for this minion")
	}
}

// getExitCodeForMinion gets the exit code for a specific minion and command
func (c *Console) getExitCodeForMinion(ctx context.Context, cmdID, minionID string) int {
	req := &pb.ResultRequest{CommandId: cmdID}
	results, err := c.grpc.GetCommandResults(ctx, req)
	if err != nil || len(results.Results) == 0 {
		return -1
	}

	for _, result := range results.Results {
		if result.MinionId == minionID {
			return int(result.ExitCode)
		}
	}
	return -1
}

// initializeMinionStats initializes statistics for all minions
func (c *Console) initializeMinionStats(minions []*pb.HostInfo) map[string]map[string]int {
	minionStats := make(map[string]map[string]int)
	for _, minion := range minions {
		minionStats[minion.Id] = map[string]int{
			"total":     0,
			"completed": 0,
			"failed":    0,
		}
	}
	return minionStats
}

// collectMinionStatistics collects statistics for all minions
func (c *Console) collectMinionStatistics(minionStats map[string]map[string]int) {
	for _, status := range c.commandStatus {
		for minionID, st := range status.Statuses {
			if stats, exists := minionStats[minionID]; exists {
				stats["total"]++
				if st == "COMPLETED" {
					stats["completed"]++
				} else if st == "FAILED" {
					stats["failed"]++
				}
			}
		}
	}
}

// printMinionStats prints statistics for each minion and returns totals
func (c *Console) printMinionStats(minions []*pb.HostInfo, minionStats map[string]map[string]int) (int, int, int) {
	totalCommands := 0
	totalCompleted := 0
	totalFailed := 0

	for _, minion := range minions {
		stats := minionStats[minion.Id]
		successRate := c.calculateSuccessRate(stats["completed"], stats["total"])

		fmt.Printf("%-36s | %-17s | %-5d | %-9d | %-6d | %6.1f%%\n",
			minion.Id,
			minion.Hostname,
			stats["total"],
			stats["completed"],
			stats["failed"],
			successRate)

		totalCommands += stats["total"]
		totalCompleted += stats["completed"]
		totalFailed += stats["failed"]
	}

	return totalCommands, totalCompleted, totalFailed
}

// printStatsTotal prints the total statistics row
func (c *Console) printStatsTotal(totalCommands, totalCompleted, totalFailed int) {
	fmt.Println("------------------------------------ | ----------------- | ----- | --------- | ------ | ------------")
	overallSuccessRate := c.calculateSuccessRate(totalCompleted, totalCommands)
	fmt.Printf("%-36s | %-17s | %-5d | %-9d | %-6d | %6.1f%%\n",
		"TOTAL",
		"",
		totalCommands,
		totalCompleted,
		totalFailed,
		overallSuccessRate)
}

// calculateSuccessRate calculates success rate percentage
func (c *Console) calculateSuccessRate(completed, total int) float64 {
	if total > 0 {
		return float64(completed) / float64(total) * 100
	}
	return 0.0
}

// Backward compatibility methods for tests

// Registry returns the command registry (for test compatibility)
func (c *Console) Registry() *command.Registry {
	if c.ui != nil {
		return c.ui.registry
	}
	return nil
}

// RL returns the readline instance (for test compatibility)
func (c *Console) RL() interface{} {
	if c.ui != nil {
		return c.ui.rl
	}
	return nil
}

// setupReadline sets up readline (for test compatibility)
func (c *Console) setupReadline() {
	if c.ui != nil {
		c.ui.setupReadline()
	}
}

// createCompleter creates a completer (for test compatibility)
func (c *Console) createCompleter() interface{} {
	if c.ui != nil {
		return c.ui.createCompleter()
	}
	return nil
}

// showHistory shows command history (for test compatibility)
func (c *Console) showHistory() {
	if c.ui != nil {
		c.ui.ShowHistory()
	}
}

// clearScreen clears the screen (for test compatibility)
func (c *Console) clearScreen() {
	if c.ui != nil {
		c.ui.ClearScreen()
	}
}

// addToHistory adds to history (for test compatibility)
func (c *Console) addToHistory(cmd string) {
	if c.ui != nil {
		c.ui.AddToHistory(cmd)
	}
}

// isHexString checks if string is hex
func isHexString(s string) bool {
	return util.IsHexString(s)
}

func main() {
	// Check for version flag
	if version.CheckAndHandleVersionFlag("Console") {
		return
	}

	// Check for offline commands that can work without server connection
	if len(os.Args) > 1 {
		command := strings.ToLower(os.Args[1])
		if command == "version" || command == "help" || command == "h" {
			handleOfflineCommand(command, os.Args[2:])
			return
		}
	}

	// Load configuration using the new unified system
	cfg, err := config.LoadConsoleConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Set up logging
	logger, _, err := logging.SetupLogger(cfg.Debug)
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}
	defer logger.Sync()

	logger, start := logging.FuncLogger(logger, "main")
	defer logging.FuncExit(logger, start)

	if cfg.Debug {
		logger.Info("Configuration loaded",
			zap.String("server", cfg.ServerAddr),
			zap.Int("timeout", cfg.ConnectTimeout),
			zap.Bool("debug", cfg.Debug))
	}

	// Display version information
	logger.Info("Starting Console",
		zap.String("version", version.Component("Console")))

	// Create gRPC client
	grpcClient, err := NewGRPCClient(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to connect to server", zap.Error(err))
	}
	defer grpcClient.Close()

	// Create and start console
	console := NewConsole(grpcClient, logger)
	console.Start()
}

// handleOfflineCommand handles commands that can work without server connection
func handleOfflineCommand(command string, args []string) {

	switch command {
	case "version", "v":
		fmt.Printf("Console %s\n", version.Info())
	case "help", "h":
		if len(args) > 0 {
			fmt.Printf("Offline mode: detailed help for '%s' requires server connection\n", args[0])
		} else {
			fmt.Println("=== Console Commands ===")
			fmt.Println("  help, h [command]                          - Show this help message or help for specific command")
			fmt.Println("  version, v                                 - Show version information")
			fmt.Println("  minion-list, lm                            - List all connected minions with last seen time")
			fmt.Println("  tag-list, lt                               - List all available tags")
			fmt.Println("  command-send all <cmd>                     - Send command to all minions")
			fmt.Println("  command-send minion <id> <cmd>             - Send command to specific minion")
			fmt.Println("  command-send tag <key>=<value> <cmd>       - Send command to minions with tag")
			fmt.Println("Command Status:")
			fmt.Println("  command-status all                         - Show status breakdown of all commands")
			fmt.Println("  command-status minion <id>                 - Show detailed status of commands for a minion")
			fmt.Println("  command-status stats                       - Show command execution statistics by minion")
			fmt.Println("  result-get <cmd-id>                        - Get results for a command ID")
			fmt.Println("Tag Management:")
			fmt.Println("  tag-set <minion-id> <key>=<value> [...]    - Set tags for a minion (replaces all)")
			fmt.Println("  tag-update <minion-id> +<key>=<value> -<key> [...] - Update tags for a minion")
			fmt.Println("Other Commands:")
			fmt.Println("  clear                                      - Clear screen")
			fmt.Println("  history                                    - Show command history")
			fmt.Println("  quit, exit                                 - Exit the console")
			fmt.Println()
			fmt.Println("Note: For full interactive mode and command execution, server connection is required.")
		}
	}
}
