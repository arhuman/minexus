package nexus

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"minexus/internal/logging"
	pb "minexus/protogen"

	"go.uber.org/zap"
)

// DatabaseServiceImpl implements the DatabaseService interface for nexus operations.
// It handles all database persistence operations including hosts, commands, and results.
type DatabaseServiceImpl struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewDatabaseService creates a new database service instance.
func NewDatabaseService(db *sql.DB, logger *zap.Logger) *DatabaseServiceImpl {

	logger, start := logging.FuncLogger(logger, "NewDatabaseService")
	defer logging.FuncExit(logger, start)

	service := &DatabaseServiceImpl{
		db:     db,
		logger: logger,
	}

	logger.Debug("Database service created")
	return service
}

// StoreHost persists host information to the database.
func (d *DatabaseServiceImpl) StoreHost(ctx context.Context, hostInfo *pb.HostInfo) error {
	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.StoreHost")
	defer logging.FuncExit(logger, start)

	if d == nil || d.db == nil {
		logger.Debug("Database not available, gracefully degrading")
		return nil // Graceful degradation when database is not available
	}

	// Store in hosts table using simplified schema
	tagsJSON, err := json.Marshal(hostInfo.Tags)
	if err != nil {
		logger.Error("Failed to marshal host tags", zap.String("host_id", hostInfo.Id))
		return fmt.Errorf("failed to marshal host tags: %v", err)
	}

	now := time.Now()
	_, err = d.db.ExecContext(ctx,
		`INSERT INTO hosts (id, hostname, ip, os, first_seen, last_seen, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			hostname = EXCLUDED.hostname,
			ip = EXCLUDED.ip,
			os = EXCLUDED.os,
			last_seen = EXCLUDED.last_seen,
			tags = EXCLUDED.tags`,
		hostInfo.Id, hostInfo.Hostname, hostInfo.Ip, hostInfo.Os, now, now, string(tagsJSON))

	if err != nil {
		logger.Error("Failed to insert host in database", zap.String("host_id", hostInfo.Id))
		return fmt.Errorf("failed to insert host: %v", err)
	}

	logger.Debug("Host stored successfully", zap.String("host_id", hostInfo.Id))
	return nil
}

// UpdateHost updates existing host information in the database.
func (d *DatabaseServiceImpl) UpdateHost(ctx context.Context, hostInfo *pb.HostInfo) error {
	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.UpdateHost")
	defer logging.FuncExit(logger, start)

	if d == nil || d.db == nil {
		logger.Debug("Database not available, gracefully degrading")
		return nil // Graceful degradation when database is not available
	}

	tagsJSON, err := json.Marshal(hostInfo.Tags)
	if err != nil {
		logger.Error("Failed to marshal host tags", zap.String("host_id", hostInfo.Id))
		return fmt.Errorf("failed to marshal host tags: %v", err)
	}

	now := time.Now()
	result, err := d.db.ExecContext(ctx,
		"UPDATE hosts SET hostname=$2, ip=$3, os=$4, last_seen=$5, tags=$6 WHERE id=$1",
		hostInfo.Id, hostInfo.Hostname, hostInfo.Ip, hostInfo.Os, now, string(tagsJSON))

	if err != nil {
		logger.Error("Failed to update host in database", zap.String("host_id", hostInfo.Id))
		return fmt.Errorf("failed to update host: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		logger.Debug("Host not found, inserting instead", zap.String("host_id", hostInfo.Id))
		// Record doesn't exist, insert it
		return d.StoreHost(ctx, hostInfo)
	}

	logger.Debug("Host updated successfully", zap.String("host_id", hostInfo.Id))
	return nil
}

// StoreCommand persists command information to the database.
func (d *DatabaseServiceImpl) StoreCommand(ctx context.Context, commandID, minionID, payload string) error {
	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.StoreCommand")
	defer logging.FuncExit(logger, start)

	if d == nil || d.db == nil {
		logger.Debug("Database not available, gracefully degrading")
		return nil // Graceful degradation when database is not available
	}

	_, err := d.db.ExecContext(ctx,
		"INSERT INTO commands (id, host_id, command, timestamp, direction, status) VALUES ($1, $2, $3, $4, $5, $6)",
		commandID, minionID, payload, time.Now(), "SENT", "PENDING")

	if err != nil {
		logger.Error("Failed to store command in database",
			zap.String("command_id", commandID),
			zap.String("minion_id", minionID))
		return fmt.Errorf("failed to store command: %v", err)
	}

	logger.Debug("Stored command in database",
		zap.String("command_id", commandID),
		zap.String("minion_id", minionID),
		zap.String("payload", payload),
		zap.String("status", "PENDING"))

	return nil
}

// UpdateCommandStatus updates the status of a command in the database.
func (d *DatabaseServiceImpl) UpdateCommandStatus(ctx context.Context, commandID string, status string) error {
	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.UpdateCommandStatus")
	defer logging.FuncExit(logger, start)

	if d == nil || d.db == nil {
		logger.Debug("Database not available, gracefully degrading")
		return nil
	}

	result, err := d.db.ExecContext(ctx,
		"UPDATE commands SET status = $1 WHERE id = $2",
		status, commandID)

	if err != nil {
		logger.Error("Failed to update command status",
			zap.String("command_id", commandID),
			zap.String("status", status))
		return fmt.Errorf("failed to update command status: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected",
			zap.String("command_id", commandID))
		return fmt.Errorf("failed to get rows affected: %v", err)
	}

	if rowsAffected == 0 {
		logger.Warn("No command found to update status",
			zap.String("command_id", commandID),
			zap.String("status", status))
		return fmt.Errorf("command not found: %s", commandID)
	}

	logger.Debug("Updated command status",
		zap.String("command_id", commandID),
		zap.String("status", status))

	return nil
}

// StoreCommandResult persists command execution results to the database.
func (d *DatabaseServiceImpl) StoreCommandResult(ctx context.Context, result *pb.CommandResult) error {

	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.StoreCommandResult")
	defer logging.FuncExit(logger, start)

	if d == nil || d.db == nil {
		logger.Debug("Database not available, gracefully degrading")
		return nil // Graceful degradation when database is not available
	}

	// Check if command exists in commands table first
	var cmdExists bool
	err := d.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM commands WHERE id = $1 AND host_id = $2)",
		result.CommandId, result.MinionId).Scan(&cmdExists)

	if err != nil {
		logger.Error("Failed to check if command exists in commands table",
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId))
	} else {
		logger.Info("Command existence check",
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId),
			zap.Bool("exists_in_commands_table", cmdExists))
	}

	logger.Info("Attempting to store command result in database",
		zap.String("command_id", result.CommandId),
		zap.String("minion_id", result.MinionId),
		zap.Int32("exit_code", result.ExitCode),
		zap.String("stdout", result.Stdout),
		zap.String("stderr", result.Stderr),
		zap.Int64("timestamp", result.Timestamp))

	// Build the SQL query for better logging
	query := "INSERT INTO command_results (command_id, minion_id, exit_code, stdout, stderr, timestamp) VALUES ($1, $2, $3, $4, $5, $6)"

	// Execute the insert
	_, err = d.db.ExecContext(ctx, query,
		result.CommandId, result.MinionId, result.ExitCode, result.Stdout, result.Stderr, time.Unix(result.Timestamp, 0))

	if err != nil {
		logger.Error("Failed to store command result",
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId),
			zap.String("query", query),
			zap.String("error_type", fmt.Sprintf("%T", err)))

		// Check if there are any existing results for this command
		var resultCount int
		countErr := d.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM command_results WHERE command_id = $1",
			result.CommandId).Scan(&resultCount)

		if countErr == nil {
			logger.Info("Current result count for command",
				zap.String("command_id", result.CommandId),
				zap.Int("result_count", resultCount))
		}

		return fmt.Errorf("failed to store command result: %v", err)
	}

	logger.Info("Successfully stored command result in database",
		zap.String("command_id", result.CommandId),
		zap.String("minion_id", result.MinionId),
		zap.Int32("exit_code", result.ExitCode))
	return nil
}

// GetCommandResults retrieves all results for a specific command.
func (d *DatabaseServiceImpl) GetCommandResults(ctx context.Context, commandID string) ([]*pb.CommandResult, error) {
	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.GetCommandResults")
	defer logging.FuncExit(logger, start)

	if d == nil {
		logger.Error("DatabaseServiceImpl is nil")
		return []*pb.CommandResult{}, fmt.Errorf("DatabaseServiceImpl is nil")
	}

	if d.db == nil {
		logger.Error("Database connection is nil")
		return []*pb.CommandResult{}, fmt.Errorf("Database connection is nil")
	}

	logger.Info("Attempting to retrieve command results from database",
		zap.String("command_id", commandID))

	// First, check if the command exists in the commands table
	var cmdCount int
	err := d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM commands WHERE id = $1",
		commandID).Scan(&cmdCount)
	if err != nil {
		logger.Error("Failed to check if command exists",
			zap.String("command_id", commandID))
	} else {
		logger.Info("Command existence check",
			zap.String("command_id", commandID),
			zap.Int("command_count", cmdCount))
	}

	// Query database for command results
	rows, err := d.db.QueryContext(ctx,
		"SELECT command_id, minion_id, exit_code, stdout, stderr, EXTRACT(EPOCH FROM timestamp)::bigint FROM command_results WHERE command_id = $1 ORDER BY timestamp ASC",
		commandID)
	if err != nil {
		logger.Error("Failed to query command results",
			zap.String("command_id", commandID),
			zap.String("query", "SELECT ... FROM command_results WHERE command_id = ..."))
		return nil, fmt.Errorf("failed to query command results: %v", err)
	}
	defer rows.Close()

	var results []*pb.CommandResult
	for rows.Next() {
		var result pb.CommandResult
		var timestamp int64
		err := rows.Scan(&result.CommandId, &result.MinionId, &result.ExitCode, &result.Stdout, &result.Stderr, &timestamp)
		if err != nil {
			logger.Warn("Failed to scan command result row",
				zap.String("command_id", result.CommandId),
				zap.String("minion_id", result.MinionId))
			continue
		}
		result.Timestamp = timestamp
		results = append(results, &result)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Error iterating command result rows",
			zap.String("command_id", commandID))
		return nil, fmt.Errorf("error reading command results: %v", err)
	}

	logger.Debug("Retrieved command results",
		zap.String("command_id", commandID),
		zap.Int("count", len(results)))

	return results, nil
}

// updateHostTags updates the tags for a host in the database.
// This is a helper method used by the registry for tag operations.
func (d *DatabaseServiceImpl) updateHostTags(ctx context.Context, minionID string, hostInfo *pb.HostInfo) error {
	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.updateHostTags")
	defer logging.FuncExit(logger, start)

	if d == nil || d.db == nil {
		logger.Debug("Database not available, gracefully degrading")
		return nil // Graceful degradation when database is not available
	}

	tagsJSON, err := json.Marshal(hostInfo.Tags)
	if err != nil {
		logger.Error("Failed to marshal tags",
			zap.String("minion_id", minionID))
		return fmt.Errorf("failed to marshal tags: %v", err)
	}

	result, err := d.db.ExecContext(ctx,
		"UPDATE hosts SET tags=$2 WHERE id=$1",
		minionID, string(tagsJSON))
	if err != nil {
		logger.Error("Failed to update tags in database",
			zap.String("minion_id", minionID))
		return fmt.Errorf("failed to update tags in database: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// Verify required fields are present before attempting insert
		if hostInfo.Hostname == "" || hostInfo.Ip == "" || hostInfo.Os == "" {
			logger.Error("Missing required fields for host record",
				zap.String("minion_id", minionID),
				zap.String("hostname", hostInfo.Hostname),
				zap.String("ip", hostInfo.Ip),
				zap.String("os", hostInfo.Os))
			return fmt.Errorf("missing required fields for host record: hostname, ip, and os are required")
		}

		logger.Debug("Host not found, storing new host record",
			zap.String("minion_id", minionID),
			zap.String("hostname", hostInfo.Hostname),
			zap.String("ip", hostInfo.Ip),
			zap.String("os", hostInfo.Os),
			zap.Any("tags", hostInfo.Tags))

		// Record doesn't exist, store it properly using StoreHost
		return d.StoreHost(ctx, hostInfo)
	}

	logger.Debug("Host tags updated successfully",
		zap.String("minion_id", minionID))

	return nil
}
