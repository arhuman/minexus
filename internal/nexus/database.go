package nexus

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/arhuman/minexus/internal/logging"
	pb "github.com/arhuman/minexus/protogen"

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
	if d == nil || d.db == nil {
		// DIAGNOSIS: Log when database service is unavailable
		if d == nil {
			fmt.Printf("DIAGNOSIS: DatabaseServiceImpl is nil when trying to store host %s\n", hostInfo.Id)
		} else if d.db == nil {
			d.logger.Error("DIAGNOSIS: Database connection is nil when trying to store host",
				zap.String("host_id", hostInfo.Id))
		}
		return fmt.Errorf("database service unavailable - cannot store host %s", hostInfo.Id)
	}

	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.StoreHost")
	defer logging.FuncExit(logger, start)

	logger.Info("DIAGNOSIS: Attempting to store host in database",
		zap.String("host_id", hostInfo.Id),
		zap.String("hostname", hostInfo.Hostname),
		zap.String("ip", hostInfo.Ip))

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
	if d == nil || d.db == nil {
		return fmt.Errorf("database service unavailable - cannot update host %s", hostInfo.Id)
	}

	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.UpdateHost")
	defer logging.FuncExit(logger, start)

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
	if d == nil || d.db == nil {
		return fmt.Errorf("database service unavailable - cannot store command %s for minion %s", commandID, minionID)
	}

	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.StoreCommand")
	defer logging.FuncExit(logger, start)

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
	if d == nil || d.db == nil {
		return fmt.Errorf("database service unavailable - cannot update status for command %s to %s", commandID, status)
	}

	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.UpdateCommandStatus")
	defer logging.FuncExit(logger, start)

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

// GetCommandResults retrieves all results for a specific command.
func (d *DatabaseServiceImpl) GetCommandResults(ctx context.Context, commandID string) ([]*pb.CommandResult, error) {
	if d == nil {
		return []*pb.CommandResult{}, fmt.Errorf("DatabaseServiceImpl is nil")
	}

	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.GetCommandResults")
	defer logging.FuncExit(logger, start)

	if d.db == nil {
		logger.Error("Database connection is nil")
		return []*pb.CommandResult{}, fmt.Errorf("database connection is nil")
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
	logger.Info("DIAGNOSIS: Executing query for command results",
		zap.String("command_id", commandID),
		zap.String("query", "SELECT command_id, minion_id, exit_code, stdout, stderr, EXTRACT(EPOCH FROM timestamp)::bigint FROM command_results WHERE command_id = $1 ORDER BY timestamp ASC"))

	rows, err := d.db.QueryContext(ctx,
		"SELECT command_id, minion_id, exit_code, stdout, stderr, EXTRACT(EPOCH FROM timestamp)::bigint FROM command_results WHERE command_id = $1 ORDER BY timestamp ASC",
		commandID)
	if err != nil {
		logger.Error("DIAGNOSIS: Failed to query command results - database connection failed",
			zap.String("command_id", commandID),
			zap.String("query", "SELECT ... FROM command_results WHERE command_id = ..."),
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.Error(err))
		return nil, fmt.Errorf("failed to query command results: database connection failed")
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
	if d == nil || d.db == nil {
		return fmt.Errorf("database service unavailable - cannot update tags for host %s", minionID)
	}

	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.updateHostTags")
	defer logging.FuncExit(logger, start)

	tagsJSON, err := json.Marshal(hostInfo.Tags)
	if err != nil {
		logger.Error("Failed to marshal tags",
			zap.String("minion_id", minionID))
		return fmt.Errorf("failed to marshal tags: %v", err)
	}

	logger.Info("DIAGNOSIS: Attempting to update host tags in database",
		zap.String("minion_id", minionID),
		zap.Any("tags", hostInfo.Tags))

	result, err := d.db.ExecContext(ctx,
		"UPDATE hosts SET tags=$2 WHERE id=$1",
		minionID, string(tagsJSON))
	if err != nil {
		logger.Error("DIAGNOSIS: Failed to update tags in database - connection or table issue",
			zap.String("minion_id", minionID),
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.Error(err))
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

// StoreCommandResult persists command execution results to the database with transaction safety.
func (d *DatabaseServiceImpl) StoreCommandResult(ctx context.Context, result *pb.CommandResult) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("database service unavailable - cannot store command result for command %s", result.CommandId)
	}

	logger, start := logging.FuncLogger(d.logger, "DatabaseServiceImpl.StoreCommandResult")
	defer logging.FuncExit(logger, start)

	// Use optimized retry settings (formerly test-only, now default)
	maxRetries := 1
	baseDelay := 1 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := d.storeResultWithRetry(ctx, result, attempt, baseDelay, logger); err != nil {
			if attempt == maxRetries-1 {
				return fmt.Errorf("failed to store command result after %d attempts: %v", maxRetries, err)
			}
			continue
		}
		return nil
	}

	return fmt.Errorf("failed to store command result after %d attempts", maxRetries)
}

// storeResultWithRetry handles a single attempt to store the command result
func (d *DatabaseServiceImpl) storeResultWithRetry(ctx context.Context, result *pb.CommandResult, attempt int, baseDelay time.Duration, logger *zap.Logger) error {
	if attempt > 0 {
		delay := time.Duration(attempt*attempt) * baseDelay
		logger.Warn("HARDENING: Retrying result storage after delay",
			zap.String("command_id", result.CommandId),
			zap.Int("attempt", attempt+1),
			zap.Duration("delay", delay))
		time.Sleep(delay)
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		logger.Error("HARDENING: Failed to begin transaction for result storage",
			zap.String("command_id", result.CommandId),
			zap.Int("attempt", attempt+1),
			zap.Error(err))
		return err
	}
	defer tx.Rollback() // Will be a no-op if transaction is committed

	return d.executeStoreTransaction(ctx, tx, result, attempt, logger)
}

// executeStoreTransaction executes the complete store transaction
func (d *DatabaseServiceImpl) executeStoreTransaction(ctx context.Context, tx *sql.Tx, result *pb.CommandResult, attempt int, logger *zap.Logger) error {
	if err := d.checkCommandExists(ctx, tx, result, attempt, logger); err != nil {
		return err
	}

	if err := d.insertCommandResult(ctx, tx, result, attempt, logger); err != nil {
		return err
	}

	if err := d.updateCommandStatusInTx(ctx, tx, result, attempt, logger); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		logger.Error("HARDENING: Failed to commit result storage transaction",
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId),
			zap.Int("attempt", attempt+1),
			zap.Error(err))
		return err
	}

	logger.Info("HARDENING: Successfully stored command result with transaction safety",
		zap.String("command_id", result.CommandId),
		zap.String("minion_id", result.MinionId),
		zap.Int32("exit_code", result.ExitCode),
		zap.Int("attempt", attempt+1))
	return nil
}

// checkCommandExists verifies that the command exists in the commands table
func (d *DatabaseServiceImpl) checkCommandExists(ctx context.Context, tx *sql.Tx, result *pb.CommandResult, attempt int, logger *zap.Logger) error {
	var cmdExists bool
	err := tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM commands WHERE id = $1 AND host_id = $2)",
		result.CommandId, result.MinionId).Scan(&cmdExists)

	if err != nil {
		logger.Error("HARDENING: Failed to check if command exists",
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId),
			zap.Int("attempt", attempt+1),
			zap.Error(err))
		return err
	}

	logger.Info("HARDENING: Command existence check in transaction",
		zap.String("command_id", result.CommandId),
		zap.String("minion_id", result.MinionId),
		zap.Bool("exists_in_commands_table", cmdExists),
		zap.Int("attempt", attempt+1))

	if !cmdExists {
		d.logCommandDiagnostics(ctx, tx, result, logger)
	}

	return nil
}

// logCommandDiagnostics logs diagnostic information when command is not found
func (d *DatabaseServiceImpl) logCommandDiagnostics(ctx context.Context, tx *sql.Tx, result *pb.CommandResult, logger *zap.Logger) {
	var existingCommands []string
	rows, err := tx.QueryContext(ctx, "SELECT id FROM commands LIMIT 10")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var cmdID string
		if rows.Scan(&cmdID) == nil {
			existingCommands = append(existingCommands, cmdID)
		}
	}

	logger.Error("DIAGNOSTIC: Command not found in database - this may cause 0 results",
		zap.String("target_command_id", result.CommandId),
		zap.String("target_minion_id", result.MinionId),
		zap.Strings("existing_command_ids", existingCommands),
		zap.Int("existing_count", len(existingCommands)))
}

// insertCommandResult inserts the command result into the database
func (d *DatabaseServiceImpl) insertCommandResult(ctx context.Context, tx *sql.Tx, result *pb.CommandResult, attempt int, logger *zap.Logger) error {
	query := "INSERT INTO command_results (command_id, minion_id, exit_code, stdout, stderr, timestamp) VALUES ($1, $2, $3, $4, $5, $6)"
	_, err := tx.ExecContext(ctx, query,
		result.CommandId, result.MinionId, result.ExitCode, result.Stdout, result.Stderr, time.Unix(result.Timestamp, 0))

	if err != nil {
		logger.Error("HARDENING: Failed to insert command result in transaction",
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId),
			zap.String("query", query),
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.Int("attempt", attempt+1),
			zap.Error(err))
		return err
	}

	return nil
}

// updateCommandStatusInTx updates the command status within a transaction
func (d *DatabaseServiceImpl) updateCommandStatusInTx(ctx context.Context, tx *sql.Tx, result *pb.CommandResult, attempt int, logger *zap.Logger) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE commands SET status = $1 WHERE id = $2 AND host_id = $3",
		"COMPLETED", result.CommandId, result.MinionId)

	if err != nil {
		logger.Error("HARDENING: Failed to update command status in transaction",
			zap.String("command_id", result.CommandId),
			zap.String("minion_id", result.MinionId),
			zap.Int("attempt", attempt+1),
			zap.Error(err))
		return err
	}

	return nil
}
