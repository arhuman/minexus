package nexus

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"minexus/internal/command"
	pb "minexus/protogen"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// createTestServer creates a new Server instance for testing
func createTestServer(db *sql.DB) *Server {
	logger, _ := zap.NewDevelopment()

	var dbService DatabaseService // Interface type instead of concrete type
	if db != nil {
		dbService = NewDatabaseService(db, logger)
	}

	// Pass the concrete type to NewMinionRegistry after type assertion
	var dbServiceImpl *DatabaseServiceImpl
	if dbService != nil {
		dbServiceImpl = dbService.(*DatabaseServiceImpl)
	}
	minionRegistry := NewMinionRegistry(dbServiceImpl, logger)

	return &Server{
		logger:          logger,
		dbService:       dbService, // Will be a proper nil interface when db is nil
		minionRegistry:  minionRegistry,
		pendingCommands: make(map[string]*CommandTracker),
		commandRegistry: command.SetupCommands(15 * time.Second),
	}
}

func TestListMinionsInMemory(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add test minions to in-memory storage using the registry
	registry := server.GetMinionRegistryImpl()
	registry.minions["minion-1"] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:       "minion-1",
			Hostname: "host1",
			Ip:       "192.168.1.1",
			Os:       "linux",
			Tags:     map[string]string{"env": "test"},
		},
		LastSeen:  time.Now(),
		CommandCh: make(chan *pb.Command, 100),
	}

	registry.minions["minion-2"] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:       "minion-2",
			Hostname: "host2",
			Ip:       "192.168.1.2",
			Os:       "windows",
			Tags:     map[string]string{"env": "prod"},
		},
		LastSeen:  time.Now(),
		CommandCh: make(chan *pb.Command, 100),
	}

	list, err := server.ListMinions(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("ListMinions failed: %v", err)
	}

	if len(list.Minions) != 2 {
		t.Errorf("Expected 2 minions, got %d", len(list.Minions))
	}

	// Verify minion data
	found := make(map[string]bool)
	for _, minion := range list.Minions {
		found[minion.Id] = true

		if minion.Id == "minion-1" {
			if minion.Hostname != "host1" {
				t.Errorf("Expected hostname 'host1', got '%s'", minion.Hostname)
			}
			if minion.Ip != "192.168.1.1" {
				t.Errorf("Expected IP '192.168.1.1', got '%s'", minion.Ip)
			}
			if minion.Os != "linux" {
				t.Errorf("Expected OS 'linux', got '%s'", minion.Os)
			}
			if minion.Tags["env"] != "test" {
				t.Errorf("Expected tag env='test', got env='%s'", minion.Tags["env"])
			}
		} else if minion.Id == "minion-2" {
			if minion.Hostname != "host2" {
				t.Errorf("Expected hostname 'host2', got '%s'", minion.Hostname)
			}
			if minion.Ip != "192.168.1.2" {
				t.Errorf("Expected IP '192.168.1.2', got '%s'", minion.Ip)
			}
			if minion.Os != "windows" {
				t.Errorf("Expected OS 'windows', got '%s'", minion.Os)
			}
			if minion.Tags["env"] != "prod" {
				t.Errorf("Expected tag env='prod', got env='%s'", minion.Tags["env"])
			}
		}

		if minion.LastSeen == 0 {
			t.Error("Expected last seen timestamp to be set")
		}
	}

	if !found["minion-1"] || !found["minion-2"] {
		t.Error("Not all expected minions were found in the list")
	}
}

func TestListMinionsEmpty(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// No minions added to in-memory storage

	list, err := server.ListMinions(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("ListMinions failed: %v", err)
	}

	if len(list.Minions) != 0 {
		t.Errorf("Expected 0 minions, got %d", len(list.Minions))
	}
}

// TestSetTagsWithMissingDatabaseRecord tests the scenario where a minion exists
// in memory but not in the database, requiring an INSERT after UPDATE fails
func TestSetTagsWithMissingDatabaseRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add a minion connection to in-memory store (simulating a connected minion)
	minionID := "test-minion-123"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:       minionID,
			Hostname: "test-host",
			Ip:       "192.168.1.100",
			Os:       "linux",
			Tags:     make(map[string]string),
		},
		LastSeen:  time.Now(),
		CommandCh: make(chan *pb.Command, 100),
	}

	// Mock the UPDATE operation to return 0 rows affected (record doesn't exist)
	mock.ExpectExec("UPDATE hosts SET tags=\\$2 WHERE id=\\$1").
		WithArgs(minionID, `{"env":"test"}`).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected

	// Mock the INSERT operation that should follow
	mock.ExpectExec("INSERT INTO hosts \\(id, hostname, ip, os, first_seen, last_seen, tags\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6, \\$7\\) ON CONFLICT \\(id\\) DO UPDATE SET hostname = EXCLUDED.hostname, ip = EXCLUDED.ip, os = EXCLUDED.os, last_seen = EXCLUDED.last_seen, tags = EXCLUDED.tags").
		WithArgs(minionID, "test-host", "192.168.1.100", "linux", sqlmock.AnyArg(), sqlmock.AnyArg(), `{"env":"test"}`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// PHASE 3: Registration history operations removed
	// mock.ExpectExec("INSERT INTO registration_history") - NO LONGER NEEDED

	// Create the SetTags request
	req := &pb.SetTagsRequest{
		MinionId: minionID,
		Tags: map[string]string{
			"env": "test",
		},
	}

	// Call SetTags
	response, err := server.SetTags(context.Background(), req)
	if err != nil {
		t.Fatalf("SetTags failed: %v", err)
	}

	if !response.Success {
		t.Error("Expected SetTags to succeed")
	}

	// Verify the in-memory tags were updated
	conn := server.GetMinionRegistryImpl().minions[minionID]
	if conn.Info.Tags["env"] != "test" {
		t.Errorf("Expected in-memory tag env=test, got env=%s", conn.Info.Tags["env"])
	}

	// Verify all database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestUpdateTagsWithMissingDatabaseRecord tests the scenario where a minion exists
// in memory but not in the database during tag updates
func TestUpdateTagsWithMissingDatabaseRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add a minion connection to in-memory store with existing tags
	minionID := "test-minion-456"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:       minionID,
			Hostname: "test-host-2",
			Ip:       "192.168.1.101",
			Os:       "darwin",
			Tags: map[string]string{
				"existing": "tag",
			},
		},
		LastSeen:  time.Now(),
		CommandCh: make(chan *pb.Command, 100),
	}

	// Mock the UPDATE operation to return 0 rows affected (record doesn't exist)
	mock.ExpectExec("UPDATE hosts SET tags=\\$2 WHERE id=\\$1").
		WithArgs(minionID, `{"env":"production","existing":"tag"}`).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected

	// Mock the INSERT operation that should follow
	mock.ExpectExec("INSERT INTO hosts \\(id, hostname, ip, os, first_seen, last_seen, tags\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6, \\$7\\) ON CONFLICT \\(id\\) DO UPDATE SET hostname = EXCLUDED.hostname, ip = EXCLUDED.ip, os = EXCLUDED.os, last_seen = EXCLUDED.last_seen, tags = EXCLUDED.tags").
		WithArgs(minionID, "test-host-2", "192.168.1.101", "darwin", sqlmock.AnyArg(), sqlmock.AnyArg(), `{"env":"production","existing":"tag"}`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// PHASE 3: Registration history operations removed
	// mock.ExpectExec("INSERT INTO registration_history") - NO LONGER NEEDED

	// Create the UpdateTags request
	req := &pb.UpdateTagsRequest{
		MinionId: minionID,
		Add: map[string]string{
			"env": "production",
		},
		RemoveKeys: []string{}, // No tags to remove
	}

	// Call UpdateTags
	response, err := server.UpdateTags(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateTags failed: %v", err)
	}

	if !response.Success {
		t.Error("Expected UpdateTags to succeed")
	}

	// Verify the in-memory tags were updated
	conn := server.GetMinionRegistryImpl().minions[minionID]
	if conn.Info.Tags["env"] != "production" {
		t.Errorf("Expected in-memory tag env=production, got env=%s", conn.Info.Tags["env"])
	}
	if conn.Info.Tags["existing"] != "tag" {
		t.Errorf("Expected existing tag to remain, got existing=%s", conn.Info.Tags["existing"])
	}

	// Verify all database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestSendCommandWithNonExistentCommand tests that non-existent commands are rejected
func TestSendCommandWithNonExistentCommand(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add a test minion to target
	minionID := "test-minion-123"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:       minionID,
			Hostname: "test-host",
			Ip:       "192.168.1.100",
			Os:       "linux",
			Tags:     make(map[string]string),
		},
		LastSeen:  time.Now(),
		CommandCh: make(chan *pb.Command, 100),
	}

	tests := []struct {
		name        string
		command     *pb.Command
		shouldError bool
		errorMsg    string
	}{
		{
			name: "invalid system command",
			command: &pb.Command{
				Type:    pb.CommandType_SYSTEM,
				Payload: "system:osddsfdsf",
			},
			shouldError: true,
			errorMsg:    "invalid command: unknown command: system:osddsfdsf",
		},
		{
			name: "invalid file command",
			command: &pb.Command{
				Type:    pb.CommandType_SYSTEM,
				Payload: "file:nonexistent",
			},
			shouldError: true,
			errorMsg:    "invalid command: unknown command: file:nonexistent",
		},
		{
			name: "valid system command",
			command: &pb.Command{
				Type:    pb.CommandType_SYSTEM,
				Payload: "system:info",
			},
			shouldError: false,
		},
		{
			name: "valid shell command",
			command: &pb.Command{
				Type:    pb.CommandType_SYSTEM,
				Payload: "ls -la",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For valid commands, expect a database insert
			if !tt.shouldError {
				mock.ExpectExec("INSERT INTO commands \\(id, host_id, command, timestamp, direction, status\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6\\)").
					WithArgs(sqlmock.AnyArg(), minionID, tt.command.Payload, sqlmock.AnyArg(), "SENT", "PENDING").
					WillReturnResult(sqlmock.NewResult(1, 1))
			}

			req := &pb.CommandRequest{
				MinionIds: []string{minionID},
				Command:   tt.command,
			}

			response, err := server.SendCommand(context.Background(), req)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain '%s', got '%s'", tt.errorMsg, err.Error())
				}
				if response.Accepted {
					t.Error("Expected command to be rejected but it was accepted")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if !response.Accepted {
					t.Error("Expected command to be accepted but it was rejected")
				}
				if response.CommandId == "" {
					t.Error("Expected command ID to be generated for accepted command")
				}
			}
		})
	}

	// Verify all database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestSendCommandWithEmptyPayload tests that commands with empty payloads are rejected
func TestSendCommandWithEmptyPayload(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add a test minion to target
	minionID := "test-minion-123"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:       minionID,
			Hostname: "test-host",
			Ip:       "192.168.1.100",
			Os:       "linux",
			Tags:     make(map[string]string),
		},
		LastSeen:  time.Now(),
		CommandCh: make(chan *pb.Command, 100),
	}

	req := &pb.CommandRequest{
		MinionIds: []string{minionID},
		Command: &pb.Command{
			Type:    pb.CommandType_SYSTEM,
			Payload: "",
		},
	}

	response, err := server.SendCommand(context.Background(), req)
	if err == nil {
		t.Error("Expected error for empty command payload but got none")
	}
	if !strings.Contains(err.Error(), "command payload is empty") {
		t.Errorf("Expected error about empty payload, got: %v", err)
	}
	if response.Accepted {
		t.Error("Expected command with empty payload to be rejected")
	}

	// Verify no database operations were attempted since validation failed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestMinionRegistrationDataIntegrity tests that minion registration stores
// data in the correct database columns and would catch schema issues like
// hostname/id field confusion
func TestMinionRegistrationDataIntegrity(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Test data
	testMinionID := "minion-docker-01"
	testHostname := "71d3ac55397a"
	testIP := "127.0.0.1"
	testOS := "linux"

	// Create host info as it would come from a minion
	hostInfo := &pb.HostInfo{
		Id:       testMinionID,
		Hostname: testHostname,
		Ip:       testIP,
		Os:       testOS,
		Tags:     make(map[string]string),
	}

	// Mock the database operations for registration
	// New architecture calls StoreHost directly for new minions
	mock.ExpectExec("INSERT INTO hosts \\(id, hostname, ip, os, first_seen, last_seen, tags\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6, \\$7\\) ON CONFLICT \\(id\\) DO UPDATE SET hostname = EXCLUDED.hostname, ip = EXCLUDED.ip, os = EXCLUDED.os, last_seen = EXCLUDED.last_seen, tags = EXCLUDED.tags").
		WithArgs(testMinionID, testHostname, testIP, testOS, sqlmock.AnyArg(), sqlmock.AnyArg(), "{}").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// PHASE 3: Registration history operations removed
	// mock.ExpectExec("INSERT INTO registration_history") - NO LONGER NEEDED

	// Call Register
	response, err := server.Register(context.Background(), hostInfo)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if !response.Success {
		t.Error("Expected registration to succeed")
	}

	if response.AssignedId != testMinionID {
		t.Errorf("Expected assigned ID to be %s, got %s", testMinionID, response.AssignedId)
	}

	// Verify the in-memory state has correct data mapping
	registry := server.GetMinionRegistryImpl()
	registry.minionsMu.RLock()
	conn, exists := registry.minions[testMinionID]
	registry.minionsMu.RUnlock()

	if !exists {
		t.Fatal("Expected minion to be stored in memory")
	}

	// Critical validation: ensure ID and hostname are in correct fields
	if conn.Info.Id != testMinionID {
		t.Errorf("Expected minion ID to be %s, got %s", testMinionID, conn.Info.Id)
	}

	if conn.Info.Hostname != testHostname {
		t.Errorf("Expected hostname to be %s, got %s", testHostname, conn.Info.Hostname)
	}

	// Verify all database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestDatabaseSchemaConsistency tests that the database schema is consistent
// and would catch issues where columns are misnamed or misused
func TestDatabaseSchemaConsistency(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Test case: Register a minion and then list it to verify data consistency
	testMinionID := "test-minion-123"
	testHostname := "test-host-name"
	testIP := "192.168.1.100"
	testOS := "linux"

	// Step 1: Register minion
	hostInfo := &pb.HostInfo{
		Id:       testMinionID,
		Hostname: testHostname,
		Ip:       testIP,
		Os:       testOS,
		Tags:     make(map[string]string),
	}

	// Mock registration database operations - new architecture calls StoreHost directly
	mock.ExpectExec("INSERT INTO hosts \\(id, hostname, ip, os, first_seen, last_seen, tags\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6, \\$7\\) ON CONFLICT \\(id\\) DO UPDATE SET hostname = EXCLUDED.hostname, ip = EXCLUDED.ip, os = EXCLUDED.os, last_seen = EXCLUDED.last_seen, tags = EXCLUDED.tags").
		WithArgs(testMinionID, testHostname, testIP, testOS, sqlmock.AnyArg(), sqlmock.AnyArg(), "{}").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// PHASE 3: Registration history operations removed
	// mock.ExpectExec("INSERT INTO registration_history") - NO LONGER NEEDED

	_, err = server.Register(context.Background(), hostInfo)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Step 2: List minions from in-memory storage and verify the data consistency
	// This would fail if there was data confusion during registration
	list, err := server.ListMinions(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("ListMinions failed: %v", err)
	}

	if len(list.Minions) != 1 {
		t.Fatalf("Expected 1 minion, got %d", len(list.Minions))
	}

	minion := list.Minions[0]

	// Critical test: verify that data round-trips correctly through registration and listing
	// This would catch issues where id/hostname fields get confused during registration
	if minion.Id != testMinionID {
		t.Errorf("Data integrity error: Expected minion.Id=%s, got %s", testMinionID, minion.Id)
		t.Errorf("This indicates a registration issue where ID and hostname may be swapped")
	}

	if minion.Hostname != testHostname {
		t.Errorf("Data integrity error: Expected minion.Hostname=%s, got %s", testHostname, minion.Hostname)
		t.Errorf("This indicates a registration issue where ID and hostname may be swapped")
	}

	if minion.Ip != testIP {
		t.Errorf("Expected minion.Ip=%s, got %s", testIP, minion.Ip)
	}

	if minion.Os != testOS {
		t.Errorf("Expected minion.Os=%s, got %s", testOS, minion.Os)
	}

	// Verify all database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestMinionRegistrationWithPredefinedID tests registration when minion
// already has an ID assigned to ensure data mapping is correct
func TestMinionRegistrationWithPredefinedID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Simulate the exact scenario from the bug report
	predefinedMinionID := "minion-docker-01" // This should go in the id column
	actualHostname := "71d3ac55397a"         // This should go in the hostname column
	actualIP := "127.0.0.1"
	actualOS := "linux"

	hostInfo := &pb.HostInfo{
		Id:       predefinedMinionID,
		Hostname: actualHostname,
		Ip:       actualIP,
		Os:       actualOS,
		Tags:     make(map[string]string),
	}

	// Mock database operations - new architecture calls StoreHost directly
	mock.ExpectExec("INSERT INTO hosts \\(id, hostname, ip, os, first_seen, last_seen, tags\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6, \\$7\\) ON CONFLICT \\(id\\) DO UPDATE SET hostname = EXCLUDED.hostname, ip = EXCLUDED.ip, os = EXCLUDED.os, last_seen = EXCLUDED.last_seen, tags = EXCLUDED.tags").
		WithArgs(predefinedMinionID, actualHostname, actualIP, actualOS, sqlmock.AnyArg(), sqlmock.AnyArg(), "{}").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// PHASE 3: Registration history operations removed
	// mock.ExpectExec("INSERT INTO registration_history") - NO LONGER NEEDED

	// Register the minion
	response, err := server.Register(context.Background(), hostInfo)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if !response.Success {
		t.Error("Expected registration to succeed")
	}

	// The key test: verify that the assigned ID matches the original ID
	// and not the hostname (which was the symptom of the original bug)
	if response.AssignedId != predefinedMinionID {
		t.Errorf("Critical error: Expected AssignedId=%s, got %s", predefinedMinionID, response.AssignedId)
		t.Error("This suggests the minion ID and hostname are being confused in the database")
	}

	// Verify in-memory storage is correct
	registry := server.GetMinionRegistryImpl()
	registry.minionsMu.RLock()
	conn, exists := registry.minions[predefinedMinionID]
	registry.minionsMu.RUnlock()

	if !exists {
		t.Fatal("Minion should exist in memory with the correct ID")
	}

	if conn.Info.Id != predefinedMinionID {
		t.Errorf("In-memory minion ID is wrong: expected %s, got %s", predefinedMinionID, conn.Info.Id)
	}

	if conn.Info.Hostname != actualHostname {
		t.Errorf("In-memory hostname is wrong: expected %s, got %s", actualHostname, conn.Info.Hostname)
	}

	// Verify all database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestNewServer tests the server constructor function
func TestNewServer(t *testing.T) {
	tests := []struct {
		name            string
		dbConnectionStr string
		expectDBSet     bool
		expectError     bool
	}{
		{
			name:            "server without database",
			dbConnectionStr: "",
			expectDBSet:     false,
			expectError:     false,
		},
		{
			name:            "server with valid database connection string",
			dbConnectionStr: "postgres://user:pass@localhost/dbname?sslmode=disable",
			expectDBSet:     true,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := zap.NewDevelopment()
			server, err := NewServer(tt.dbConnectionStr, logger)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if server == nil {
				t.Fatal("Expected server to be created")
			}

			if tt.expectDBSet && server.dbService == nil {
				t.Error("Expected database service to be set")
			}

			if !tt.expectDBSet && server.dbService != nil {
				t.Error("Expected database service to be nil")
			}

			// Verify initialization
			if server.logger == nil {
				t.Error("Expected logger to be set")
			}

			if server.minionRegistry == nil {
				t.Error("Expected minion registry to be initialized")
			}

			if server.pendingCommands == nil {
				t.Error("Expected pendingCommands map to be initialized")
			}

			if server.commandRegistry == nil {
				t.Error("Expected command registry to be initialized")
			}

			// Test shutdown
			server.Shutdown()
		})
	}
}

// TestShutdown tests the server shutdown function
func TestShutdown(t *testing.T) {
	t.Run("shutdown with database", func(t *testing.T) {
		db, _, err := sqlmock.New()
		if err != nil {
			t.Fatalf("Failed to create mock database: %v", err)
		}

		server := createTestServer(db)

		// Should not panic
		server.Shutdown()
	})

	t.Run("shutdown without database", func(t *testing.T) {
		server := createTestServer(nil)

		// Should not panic
		server.Shutdown()
	})
}

// TestGenerateMinionID tests the ID generation function
func TestGenerateMinionID(t *testing.T) {
	// Generate multiple IDs and verify they are unique and have correct format
	ids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id := generateMinionID()

		// Check format (should be hex string of length 16)
		if len(id) != 16 {
			t.Errorf("Expected ID length 16, got %d", len(id))
		}

		// Check uniqueness
		if ids[id] {
			t.Errorf("Generated duplicate ID: %s", id)
		}
		ids[id] = true

		// Check it's valid hex
		if _, err := hex.DecodeString(id); err != nil {
			t.Errorf("Generated invalid hex ID: %s, error: %v", id, err)
		}
	}
}

// TestGetMinionIDFromContext tests context metadata extraction
func TestGetMinionIDFromContext(t *testing.T) {
	tests := []struct {
		name         string
		setupContext func() context.Context
		expectedID   string
	}{
		{
			name: "context with minion ID",
			setupContext: func() context.Context {
				md := metadata.New(map[string]string{"minion-id": "test-minion-123"})
				return metadata.NewIncomingContext(context.Background(), md)
			},
			expectedID: "test-minion-123",
		},
		{
			name: "context without metadata",
			setupContext: func() context.Context {
				return context.Background()
			},
			expectedID: "",
		},
		{
			name: "context with empty minion ID",
			setupContext: func() context.Context {
				md := metadata.New(map[string]string{})
				return metadata.NewIncomingContext(context.Background(), md)
			},
			expectedID: "",
		},
		{
			name: "context with multiple minion IDs",
			setupContext: func() context.Context {
				md := metadata.MD{
					"minion-id": []string{"first-id", "second-id"},
				}
				return metadata.NewIncomingContext(context.Background(), md)
			},
			expectedID: "first-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupContext()
			result := GetMinionIDFromContext(ctx)

			if result != tt.expectedID {
				t.Errorf("Expected ID '%s', got '%s'", tt.expectedID, result)
			}
		})
	}
}

// TestStreamCommandsWithResults tests bidirectional streaming with command results
func TestStreamCommandsWithResults(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add test minion
	minionID := "test-minion"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info:      &pb.HostInfo{Id: minionID},
		CommandCh: make(chan *pb.Command, 10),
		LastSeen:  time.Now(),
	}

	// Mock complete StoreCommandResult flow expectations:
	// 1. Begin transaction
	mock.ExpectBegin()

	// 2. Check if command exists
	mock.ExpectQuery("SELECT EXISTS\\(SELECT 1 FROM commands WHERE id = \\$1 AND host_id = \\$2\\)").
		WithArgs("cmd-123", minionID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	// 3. Insert result
	mock.ExpectExec("INSERT INTO command_results \\(command_id, minion_id, exit_code, stdout, stderr, timestamp\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6\\)").
		WithArgs("cmd-123", minionID, int32(0), "success output", "", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// 4. Update command status to COMPLETED
	mock.ExpectExec("UPDATE commands SET status = \\$1 WHERE id = \\$2 AND host_id = \\$3").
		WithArgs("COMPLETED", "cmd-123", minionID).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// 5. Commit transaction
	mock.ExpectCommit()

	// Create test messages
	result := &pb.CommandResult{
		CommandId: "cmd-123",
		MinionId:  minionID,
		ExitCode:  0,
		Stdout:    "success output",
		Stderr:    "",
		Timestamp: time.Now().Unix(),
	}

	recvMsgs := []*pb.CommandStreamMessage{
		{
			Message: &pb.CommandStreamMessage_Result{
				Result: result,
			},
		},
	}

	md := metadata.New(map[string]string{"minion-id": minionID})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	stream := &MockStreamServer{
		ctx:      ctx,
		recvMsgs: recvMsgs,
	}

	err = server.StreamCommands(stream)
	if err != nil && err != io.EOF {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestListTags tests tag listing functionality
func TestListTags(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add test minions with various tags
	server.GetMinionRegistryImpl().minions["minion-1"] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:       "minion-1",
			Hostname: "host1",
			Tags: map[string]string{
				"env":  "production",
				"role": "web",
			},
		},
		LastSeen: time.Now(),
	}

	server.GetMinionRegistryImpl().minions["minion-2"] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:       "minion-2",
			Hostname: "host2",
			Tags: map[string]string{
				"env":  "staging",
				"role": "web",
			},
		},
		LastSeen: time.Now(),
	}

	server.GetMinionRegistryImpl().minions["minion-3"] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:       "minion-3",
			Hostname: "host3",
			Tags: map[string]string{
				"env":  "production",
				"role": "database",
			},
		},
		LastSeen: time.Now(),
	}

	list, err := server.ListTags(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("ListTags failed: %v", err)
	}

	expectedTags := map[string]bool{
		"env:production": true,
		"env:staging":    true,
		"role:web":       true,
		"role:database":  true,
	}

	if len(list.Tags) != len(expectedTags) {
		t.Errorf("Expected %d tags, got %d", len(expectedTags), len(list.Tags))
	}

	for _, tag := range list.Tags {
		if !expectedTags[tag] {
			t.Errorf("Unexpected tag: %s", tag)
		}
		delete(expectedTags, tag)
	}

	if len(expectedTags) > 0 {
		t.Errorf("Missing expected tags: %v", expectedTags)
	}
}

// TestListTagsEmpty tests tag listing with no minions
func TestListTagsEmpty(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	list, err := server.ListTags(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatalf("ListTags failed: %v", err)
	}

	if len(list.Tags) != 0 {
		t.Errorf("Expected 0 tags, got %d", len(list.Tags))
	}
}

// TestMatchesTags tests tag matching logic
func TestMatchesTags(t *testing.T) {
	hostInfo := &pb.HostInfo{
		Tags: map[string]string{
			"env":     "production",
			"role":    "web",
			"version": "1.2.3",
		},
	}

	tests := []struct {
		name     string
		selector *pb.TagSelector
		expected bool
	}{
		{
			name:     "nil selector matches all",
			selector: nil,
			expected: true,
		},
		{
			name: "exact match",
			selector: &pb.TagSelector{
				Rules: []*pb.TagMatch{
					{
						Key: "env",
						Condition: &pb.TagMatch_Equals{
							Equals: "production",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "exact match fails",
			selector: &pb.TagSelector{
				Rules: []*pb.TagMatch{
					{
						Key: "env",
						Condition: &pb.TagMatch_Equals{
							Equals: "staging",
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "key exists",
			selector: &pb.TagSelector{
				Rules: []*pb.TagMatch{
					{
						Key: "role",
						Condition: &pb.TagMatch_Exists{
							Exists: true,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "key exists fails",
			selector: &pb.TagSelector{
				Rules: []*pb.TagMatch{
					{
						Key: "nonexistent",
						Condition: &pb.TagMatch_Exists{
							Exists: true,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "key not exists",
			selector: &pb.TagSelector{
				Rules: []*pb.TagMatch{
					{
						Key: "nonexistent",
						Condition: &pb.TagMatch_NotExists{
							NotExists: true,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "key not exists fails",
			selector: &pb.TagSelector{
				Rules: []*pb.TagMatch{
					{
						Key: "env",
						Condition: &pb.TagMatch_NotExists{
							NotExists: true,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "multiple rules all match",
			selector: &pb.TagSelector{
				Rules: []*pb.TagMatch{
					{
						Key: "env",
						Condition: &pb.TagMatch_Equals{
							Equals: "production",
						},
					},
					{
						Key: "role",
						Condition: &pb.TagMatch_Exists{
							Exists: true,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "multiple rules partial match",
			selector: &pb.TagSelector{
				Rules: []*pb.TagMatch{
					{
						Key: "env",
						Condition: &pb.TagMatch_Equals{
							Equals: "production",
						},
					},
					{
						Key: "nonexistent",
						Condition: &pb.TagMatch_Exists{
							Exists: true,
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchesTags(hostInfo, tt.selector)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestFindTargetMinions tests minion targeting logic
func TestFindTargetMinions(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add test minions
	server.GetMinionRegistryImpl().minions["minion-1"] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:   "minion-1",
			Tags: map[string]string{"env": "production"},
		},
	}
	server.GetMinionRegistryImpl().minions["minion-2"] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:   "minion-2",
			Tags: map[string]string{"env": "staging"},
		},
	}
	server.GetMinionRegistryImpl().minions["minion-3"] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:   "minion-3",
			Tags: map[string]string{"env": "production"},
		},
	}

	tests := []struct {
		name     string
		request  *pb.CommandRequest
		expected []string
	}{
		{
			name: "target specific minions",
			request: &pb.CommandRequest{
				MinionIds: []string{"minion-1", "minion-3", "nonexistent"},
			},
			expected: []string{"minion-1", "minion-3"},
		},
		{
			name: "target by tag selector",
			request: &pb.CommandRequest{
				TagSelector: &pb.TagSelector{
					Rules: []*pb.TagMatch{
						{
							Key: "env",
							Condition: &pb.TagMatch_Equals{
								Equals: "production",
							},
						},
					},
				},
			},
			expected: []string{"minion-1", "minion-3"},
		},
		{
			name: "target by tag selector no matches",
			request: &pb.CommandRequest{
				TagSelector: &pb.TagSelector{
					Rules: []*pb.TagMatch{
						{
							Key: "env",
							Condition: &pb.TagMatch_Equals{
								Equals: "development",
							},
						},
					},
				},
			},
			expected: []string{},
		},
		{
			name:     "no targeting criteria",
			request:  &pb.CommandRequest{},
			expected: []string{"minion-1", "minion-2", "minion-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := server.FindTargetMinions(tt.request)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d targets, got %d", len(tt.expected), len(result))
				return
			}

			// Convert to maps for easier comparison (order doesn't matter)
			resultMap := make(map[string]bool)
			for _, id := range result {
				resultMap[id] = true
			}

			for _, expectedID := range tt.expected {
				if !resultMap[expectedID] {
					t.Errorf("Expected target %s not found in result", expectedID)
				}
			}
		})
	}
}

// TestGetCommandResults tests command result retrieval
func TestGetCommandResults(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func(sqlmock.Sqlmock)
		commandID   string
		expectError bool
		expectCount int
	}{
		{
			name: "successful retrieval with results",
			setupMock: func(mock sqlmock.Sqlmock) {
				// Mock the command existence check query first
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM commands WHERE id = \\$1").
					WithArgs("cmd-123").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{"command_id", "minion_id", "exit_code", "stdout", "stderr", "timestamp"}).
					AddRow("cmd-123", "minion-1", 0, "output1", "", 1640995200).
					AddRow("cmd-123", "minion-2", 1, "output2", "error2", 1640995201)

				mock.ExpectQuery("SELECT command_id, minion_id, exit_code, stdout, stderr, EXTRACT\\(EPOCH FROM timestamp\\)::bigint FROM command_results WHERE command_id = \\$1 ORDER BY timestamp ASC").
					WithArgs("cmd-123").
					WillReturnRows(rows)
			},
			commandID:   "cmd-123",
			expectError: false,
			expectCount: 2,
		},
		{
			name: "no results found",
			setupMock: func(mock sqlmock.Sqlmock) {
				// Mock the command existence check query first
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM commands WHERE id = \\$1").
					WithArgs("cmd-456").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{"command_id", "minion_id", "exit_code", "stdout", "stderr", "timestamp"})

				mock.ExpectQuery("SELECT command_id, minion_id, exit_code, stdout, stderr, EXTRACT\\(EPOCH FROM timestamp\\)::bigint FROM command_results WHERE command_id = \\$1 ORDER BY timestamp ASC").
					WithArgs("cmd-456").
					WillReturnRows(rows)
			},
			commandID:   "cmd-456",
			expectError: false,
			expectCount: 0,
		},
		{
			name: "database query error",
			setupMock: func(mock sqlmock.Sqlmock) {
				// Mock the command existence check query first
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM commands WHERE id = \\$1").
					WithArgs("cmd-789").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				mock.ExpectQuery("SELECT command_id, minion_id, exit_code, stdout, stderr, EXTRACT\\(EPOCH FROM timestamp\\)::bigint FROM command_results WHERE command_id = \\$1 ORDER BY timestamp ASC").
					WithArgs("cmd-789").
					WillReturnError(fmt.Errorf("database connection failed"))
			},
			commandID:   "cmd-789",
			expectError: true,
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock database: %v", err)
			}
			defer db.Close()

			server := createTestServer(db)
			tt.setupMock(mock)

			req := &pb.ResultRequest{CommandId: tt.commandID}
			results, err := server.GetCommandResults(context.Background(), req)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if len(results.Results) != tt.expectCount {
					t.Errorf("Expected %d results, got %d", tt.expectCount, len(results.Results))
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unfulfilled mock expectations: %v", err)
			}
		})
	}
}

// TestGetCommandResultsWithoutDatabase tests result retrieval without database
func TestGetCommandResultsWithoutDatabase(t *testing.T) {
	server := createTestServer(nil) // No database

	req := &pb.ResultRequest{CommandId: "cmd-123"}
	results, err := server.GetCommandResults(context.Background(), req)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(results.Results) != 0 {
		t.Errorf("Expected 0 results without database, got %d", len(results.Results))
	}
}

// TestSetTagsWithExistingRecord tests SetTags when database record exists
func TestSetTagsWithExistingRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add a minion connection to in-memory store
	minionID := "test-minion-existing"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:       minionID,
			Hostname: "test-host",
			Ip:       "192.168.1.100",
			Os:       "linux",
			Tags:     make(map[string]string),
		},
		LastSeen:  time.Now(),
		CommandCh: make(chan *pb.Command, 100),
	}

	// Mock the UPDATE operation to succeed (1 row affected)
	mock.ExpectExec("UPDATE hosts SET tags=\\$2 WHERE id=\\$1").
		WithArgs(minionID, `{"env":"production","region":"us-west"}`).
		WillReturnResult(sqlmock.NewResult(0, 1)) // 1 row affected

	// Create the SetTags request
	req := &pb.SetTagsRequest{
		MinionId: minionID,
		Tags: map[string]string{
			"env":    "production",
			"region": "us-west",
		},
	}

	// Call SetTags
	response, err := server.SetTags(context.Background(), req)
	if err != nil {
		t.Fatalf("SetTags failed: %v", err)
	}

	if !response.Success {
		t.Error("Expected SetTags to succeed")
	}

	// Verify the in-memory tags were updated
	conn := server.GetMinionRegistryImpl().minions[minionID]
	if conn.Info.Tags["env"] != "production" {
		t.Errorf("Expected in-memory tag env=production, got env=%s", conn.Info.Tags["env"])
	}
	if conn.Info.Tags["region"] != "us-west" {
		t.Errorf("Expected in-memory tag region=us-west, got region=%s", conn.Info.Tags["region"])
	}

	// Verify all database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestUpdateTagsWithExistingRecord tests UpdateTags when database record exists
func TestUpdateTagsWithExistingRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add a minion connection to in-memory store with existing tags
	minionID := "test-minion-update"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:       minionID,
			Hostname: "test-host",
			Ip:       "192.168.1.100",
			Os:       "linux",
			Tags: map[string]string{
				"env":     "staging",
				"version": "1.0.0",
				"remove":  "me",
			},
		},
		LastSeen:  time.Now(),
		CommandCh: make(chan *pb.Command, 100),
	}

	// Mock the UPDATE operation to succeed (1 row affected)
	mock.ExpectExec("UPDATE hosts SET tags=\\$2 WHERE id=\\$1").
		WithArgs(minionID, `{"env":"production","version":"2.0.0"}`).
		WillReturnResult(sqlmock.NewResult(0, 1)) // 1 row affected

	// Create the UpdateTags request
	req := &pb.UpdateTagsRequest{
		MinionId: minionID,
		Add: map[string]string{
			"env":     "production", // Override existing
			"version": "2.0.0",      // Override existing
		},
		RemoveKeys: []string{"remove"}, // Remove existing tag
	}

	// Call UpdateTags
	response, err := server.UpdateTags(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateTags failed: %v", err)
	}

	if !response.Success {
		t.Error("Expected UpdateTags to succeed")
	}

	// Verify the in-memory tags were updated correctly
	conn := server.GetMinionRegistryImpl().minions[minionID]
	if conn.Info.Tags["env"] != "production" {
		t.Errorf("Expected updated tag env=production, got env=%s", conn.Info.Tags["env"])
	}
	if conn.Info.Tags["version"] != "2.0.0" {
		t.Errorf("Expected updated tag version=2.0.0, got version=%s", conn.Info.Tags["version"])
	}
	if _, exists := conn.Info.Tags["remove"]; exists {
		t.Error("Expected 'remove' tag to be deleted")
	}

	// Verify all database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestSetTagsNonExistentMinion tests SetTags with non-existent minion
func TestSetTagsNonExistentMinion(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	req := &pb.SetTagsRequest{
		MinionId: "non-existent-minion",
		Tags:     map[string]string{"env": "test"},
	}

	response, err := server.SetTags(context.Background(), req)
	if err == nil {
		t.Error("Expected error for non-existent minion")
	}
	if response.Success {
		t.Error("Expected SetTags to fail for non-existent minion")
	}
}

// TestUpdateTagsNonExistentMinion tests UpdateTags with non-existent minion
func TestUpdateTagsNonExistentMinion(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	req := &pb.UpdateTagsRequest{
		MinionId: "non-existent-minion",
		Add:      map[string]string{"env": "test"},
	}

	response, err := server.UpdateTags(context.Background(), req)
	if err == nil {
		t.Error("Expected error for non-existent minion")
	}
	if response.Success {
		t.Error("Expected UpdateTags to fail for non-existent minion")
	}
}

// TestRegisterWithExistingRecord tests Register when database record exists
func TestRegisterWithExistingRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	testMinionID := "existing-minion"
	testHostname := "updated-hostname"
	testIP := "192.168.1.200"
	testOS := "windows"

	hostInfo := &pb.HostInfo{
		Id:       testMinionID,
		Hostname: testHostname,
		Ip:       testIP,
		Os:       testOS,
		Tags:     map[string]string{"env": "test"},
	}

	// Mock the INSERT operation for new registration (new architecture calls StoreHost)
	mock.ExpectExec("INSERT INTO hosts \\(id, hostname, ip, os, first_seen, last_seen, tags\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6, \\$7\\) ON CONFLICT \\(id\\) DO UPDATE SET hostname = EXCLUDED.hostname, ip = EXCLUDED.ip, os = EXCLUDED.os, last_seen = EXCLUDED.last_seen, tags = EXCLUDED.tags").
		WithArgs(testMinionID, testHostname, testIP, testOS, sqlmock.AnyArg(), sqlmock.AnyArg(), `{"env":"test"}`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// PHASE 3: Registration history operations removed
	// mock.ExpectExec("INSERT INTO registration_history") - NO LONGER NEEDED

	response, err := server.Register(context.Background(), hostInfo)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if !response.Success {
		t.Error("Expected registration to succeed")
	}

	if response.AssignedId != testMinionID {
		t.Errorf("Expected assigned ID to be %s, got %s", testMinionID, response.AssignedId)
	}

	// Verify in-memory storage
	registry := server.GetMinionRegistryImpl()
	registry.minionsMu.RLock()
	conn, exists := registry.minions[testMinionID]
	registry.minionsMu.RUnlock()

	if !exists {
		t.Fatal("Expected minion to be stored in memory")
	}

	if conn.Info.Hostname != testHostname {
		t.Errorf("Expected hostname %s, got %s", testHostname, conn.Info.Hostname)
	}

	// Verify all database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestRegisterWithoutID tests registration when no ID is provided
func TestRegisterWithoutID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	hostInfo := &pb.HostInfo{
		Id:       "", // No ID provided
		Hostname: "new-host",
		Ip:       "192.168.1.150",
		Os:       "linux",
		Tags:     nil, // Will be initialized
	}

	// Expect INSERT for new registration (new architecture calls StoreHost)
	mock.ExpectExec("INSERT INTO hosts \\(id, hostname, ip, os, first_seen, last_seen, tags\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6, \\$7\\) ON CONFLICT \\(id\\) DO UPDATE SET hostname = EXCLUDED.hostname, ip = EXCLUDED.ip, os = EXCLUDED.os, last_seen = EXCLUDED.last_seen, tags = EXCLUDED.tags").
		WithArgs(sqlmock.AnyArg(), "new-host", "192.168.1.150", "linux", sqlmock.AnyArg(), sqlmock.AnyArg(), "{}").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// PHASE 3: Registration history operations removed
	// mock.ExpectExec("INSERT INTO registration_history") - NO LONGER NEEDED

	response, err := server.Register(context.Background(), hostInfo)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if !response.Success {
		t.Error("Expected registration to succeed")
	}

	if response.AssignedId == "" {
		t.Error("Expected an ID to be generated and assigned")
	}

	if len(response.AssignedId) != 16 {
		t.Errorf("Expected generated ID to be 16 characters, got %d", len(response.AssignedId))
	}

	// Verify tags were initialized
	if hostInfo.Tags == nil {
		t.Error("Expected tags to be initialized")
	}

	// Verify all database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestSendCommandSuccessful tests successful command dispatch
func TestSendCommandSuccessful(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add test minions
	minionID1 := "minion-1"
	minionID2 := "minion-2"

	server.GetMinionRegistryImpl().minions[minionID1] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:   minionID1,
			Tags: map[string]string{"env": "production"},
		},
		CommandCh: make(chan *pb.Command, 100),
	}

	server.GetMinionRegistryImpl().minions[minionID2] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:   minionID2,
			Tags: map[string]string{"env": "production"},
		},
		CommandCh: make(chan *pb.Command, 100),
	}

	// Mock database inserts for both minions
	mock.ExpectExec("INSERT INTO commands \\(id, host_id, command, timestamp, direction, status\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6\\)").
		WithArgs(sqlmock.AnyArg(), minionID1, "ls -la", sqlmock.AnyArg(), "SENT", "PENDING").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT INTO commands \\(id, host_id, command, timestamp, direction, status\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6\\)").
		WithArgs(sqlmock.AnyArg(), minionID2, "ls -la", sqlmock.AnyArg(), "SENT", "PENDING").
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := &pb.CommandRequest{
		MinionIds: []string{minionID1, minionID2},
		Command: &pb.Command{
			Type:    pb.CommandType_SYSTEM,
			Payload: "ls -la",
		},
	}

	response, err := server.SendCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("SendCommand failed: %v", err)
	}

	if !response.Accepted {
		t.Error("Expected command to be accepted")
	}

	if response.CommandId == "" {
		t.Error("Expected command ID to be generated")
	}

	// Verify commands were sent to minions
	select {
	case cmd := <-server.GetMinionRegistryImpl().minions[minionID1].CommandCh:
		if cmd.Payload != "ls -la" {
			t.Errorf("Expected payload 'ls -la', got '%s'", cmd.Payload)
		}
		if cmd.Id != response.CommandId {
			t.Errorf("Expected command ID %s, got %s", response.CommandId, cmd.Id)
		}
	default:
		t.Error("Expected command to be sent to minion-1")
	}

	select {
	case cmd := <-server.GetMinionRegistryImpl().minions[minionID2].CommandCh:
		if cmd.Payload != "ls -la" {
			t.Errorf("Expected payload 'ls -la', got '%s'", cmd.Payload)
		}
	default:
		t.Error("Expected command to be sent to minion-2")
	}

	// Verify all database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestSendCommandNoTargets tests command dispatch with no target minions
func TestSendCommandNoTargets(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	req := &pb.CommandRequest{
		MinionIds: []string{"non-existent-minion"},
		Command: &pb.Command{
			Type:    pb.CommandType_SYSTEM,
			Payload: "ls -la",
		},
	}

	response, err := server.SendCommand(context.Background(), req)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if response.Accepted {
		t.Error("Expected command to be rejected when no targets found")
	}

	if response.CommandId != "" {
		t.Error("Expected empty command ID when no targets found")
	}
}

// TestValidateCommandInternal tests command validation logic
func TestValidateCommandInternal(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	tests := []struct {
		name        string
		command     *pb.Command
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil command",
			command:     nil,
			expectError: true,
			errorMsg:    "command is nil",
		},
		{
			name: "empty payload",
			command: &pb.Command{
				Type:    pb.CommandType_SYSTEM,
				Payload: "",
			},
			expectError: true,
			errorMsg:    "command payload is empty",
		},
		{
			name: "valid shell command",
			command: &pb.Command{
				Type:    pb.CommandType_SYSTEM,
				Payload: "echo hello",
			},
			expectError: false,
		},
		{
			name: "valid system command",
			command: &pb.Command{
				Type:    pb.CommandType_SYSTEM,
				Payload: "system:info",
			},
			expectError: false,
		},
		{
			name: "invalid system command",
			command: &pb.Command{
				Type:    pb.CommandType_SYSTEM,
				Payload: "system:nonexistent",
			},
			expectError: true,
			errorMsg:    "unknown command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := server.validateCommand(tt.command)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// MockStreamServer implements pb.MinionService_StreamCommandsServer for testing
type MockStreamServer struct {
	ctx       context.Context
	sentMsgs  []*pb.CommandStreamMessage
	recvMsgs  []*pb.CommandStreamMessage
	recvIndex int
	sendErr   error
	recvErr   error
}

func (m *MockStreamServer) Send(msg *pb.CommandStreamMessage) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sentMsgs = append(m.sentMsgs, msg)
	return nil
}

func (m *MockStreamServer) Recv() (*pb.CommandStreamMessage, error) {
	if m.recvErr != nil {
		return nil, m.recvErr
	}
	if m.recvIndex >= len(m.recvMsgs) {
		return nil, io.EOF
	}
	msg := m.recvMsgs[m.recvIndex]
	m.recvIndex++
	return msg, nil
}

func (m *MockStreamServer) Context() context.Context {
	return m.ctx
}

func (m *MockStreamServer) SendMsg(msg interface{}) error {
	return nil
}

func (m *MockStreamServer) RecvMsg(msg interface{}) error {
	return nil
}

func (m *MockStreamServer) SetHeader(metadata.MD) error {
	return nil
}

func (m *MockStreamServer) SendHeader(metadata.MD) error {
	return nil
}

func (m *MockStreamServer) SetTrailer(metadata.MD) {}

// TestStreamCommands tests the bidirectional streaming command method
func TestStreamCommands(t *testing.T) {
	tests := []struct {
		name        string
		setupCtx    func() context.Context
		setupServer func(*Server) string
		setupStream func(*MockStreamServer)
		expectError bool
		errorCode   codes.Code
		verify      func(*testing.T, *Server, string, *MockStreamServer)
	}{
		{
			name: "no minion ID in context",
			setupCtx: func() context.Context {
				return context.Background()
			},
			setupServer: func(s *Server) string {
				return ""
			},
			setupStream: func(s *MockStreamServer) {},
			expectError: true,
			errorCode:   codes.Unauthenticated,
			verify:      func(t *testing.T, s *Server, id string, stream *MockStreamServer) {},
		},
		{
			name: "minion not found",
			setupCtx: func() context.Context {
				md := metadata.New(map[string]string{"minion-id": "non-existent"})
				return metadata.NewIncomingContext(context.Background(), md)
			},
			setupServer: func(s *Server) string {
				return "non-existent"
			},
			setupStream: func(s *MockStreamServer) {},
			expectError: true,
			errorCode:   codes.NotFound,
			verify:      func(t *testing.T, s *Server, id string, stream *MockStreamServer) {},
		},
		{
			name: "successful streaming with command and status",
			setupCtx: func() context.Context {
				md := metadata.New(map[string]string{"minion-id": "test-minion"})
				return metadata.NewIncomingContext(context.Background(), md)
			},
			setupServer: func(s *Server) string {
				minionID := "test-minion"
				registry := s.GetMinionRegistryImpl()
				registry.minions[minionID] = &MinionConnectionImpl{
					Info:      &pb.HostInfo{Id: minionID},
					CommandCh: make(chan *pb.Command, 10),
					LastSeen:  time.Now(),
				}

				// Pre-populate the command channel
				registry.minions[minionID].CommandCh <- &pb.Command{
					Id:      "cmd-1",
					Payload: "test command",
				}

				return minionID
			},
			setupStream: func(s *MockStreamServer) {
				s.recvMsgs = []*pb.CommandStreamMessage{
					{
						Message: &pb.CommandStreamMessage_Status{
							Status: &pb.CommandStatusUpdate{
								CommandId: "cmd-1",
								MinionId:  "test-minion",
								Status:    "EXECUTING",
								Timestamp: time.Now().Unix(),
							},
						},
					},
					{
						Message: &pb.CommandStreamMessage_Result{
							Result: &pb.CommandResult{
								CommandId: "cmd-1",
								MinionId:  "test-minion",
								ExitCode:  0,
								Stdout:    "test output",
							},
						},
					},
				}
			},
			expectError: false,
			verify: func(t *testing.T, s *Server, minionID string, stream *MockStreamServer) {
				// Verify last seen was updated
				registry := s.GetMinionRegistryImpl()
				registry.minionsMu.RLock()
				conn := registry.minions[minionID]
				registry.minionsMu.RUnlock()

				if time.Since(conn.LastSeen) > time.Second {
					t.Error("Expected LastSeen to be updated")
				}

				// Verify command was sent
				if len(stream.sentMsgs) != 1 {
					t.Errorf("Expected 1 command to be sent, got %d", len(stream.sentMsgs))
				} else {
					msg := stream.sentMsgs[0]
					cmd := msg.GetCommand()
					if cmd == nil {
						t.Error("Expected CommandStreamMessage to contain a Command")
					} else if cmd.Payload != "test command" {
						t.Errorf("Expected command payload 'test command', got '%s'", cmd.Payload)
					}
				}

				// Verify status and result messages were processed
				if len(stream.recvMsgs) != 2 {
					t.Errorf("Expected 2 messages to be received, got %d", len(stream.recvMsgs))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock database: %v", err)
			}
			defer db.Close()

			server := createTestServer(db)
			minionID := tt.setupServer(server)

			// Set up complete mock expectations for status updates and result storage
			if tt.name == "successful streaming with command and status" {
				// Expect status update for EXECUTING
				mock.ExpectExec("UPDATE commands SET status = \\$1 WHERE id = \\$2").
					WithArgs("EXECUTING", "cmd-1").
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Complete StoreCommandResult flow expectations:
				// 1. Begin transaction
				mock.ExpectBegin()

				// 2. Check if command exists
				mock.ExpectQuery("SELECT EXISTS\\(SELECT 1 FROM commands WHERE id = \\$1 AND host_id = \\$2\\)").
					WithArgs("cmd-1", "test-minion").
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

				// 3. Insert result
				mock.ExpectExec("INSERT INTO command_results \\(command_id, minion_id, exit_code, stdout, stderr, timestamp\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6\\)").
					WithArgs("cmd-1", "test-minion", int32(0), "test output", "", sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))

				// 4. Update command status to COMPLETED
				mock.ExpectExec("UPDATE commands SET status = \\$1 WHERE id = \\$2 AND host_id = \\$3").
					WithArgs("COMPLETED", "cmd-1", "test-minion").
					WillReturnResult(sqlmock.NewResult(1, 1))

				// 5. Commit transaction
				mock.ExpectCommit()
			}

			stream := &MockStreamServer{
				ctx: tt.setupCtx(),
			}
			tt.setupStream(stream)

			err = server.StreamCommands(stream)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else {
					st, ok := status.FromError(err)
					if !ok {
						t.Errorf("Expected gRPC status error, got %T", err)
					} else if st.Code() != tt.errorCode {
						t.Errorf("Expected error code %s, got %s", tt.errorCode, st.Code())
					}
				}
			} else {
				if err != nil && err != io.EOF {
					t.Errorf("Unexpected error: %v", err)
				}
				tt.verify(t, server, minionID, stream)
			}
		})
	}
}

// TestStreamCommandsError tests streaming with send error
func TestStreamCommandsError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	minionID := "test-minion"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info:      &pb.HostInfo{Id: minionID},
		CommandCh: make(chan *pb.Command, 10),
		LastSeen:  time.Now(),
	}

	// Send a command and close the channel
	go func() {
		server.GetMinionRegistryImpl().minions[minionID].CommandCh <- &pb.Command{
			Id:      "cmd-1",
			Payload: "test command",
		}
		close(server.GetMinionRegistryImpl().minions[minionID].CommandCh)
	}()

	md := metadata.New(map[string]string{"minion-id": minionID})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	stream := &MockStreamServer{
		ctx:     ctx,
		sendErr: fmt.Errorf("stream send error"),
	}

	err = server.StreamCommands(stream)
	if err == nil {
		t.Error("Expected error from stream send failure")
	}
}

// TestSendCommandChannelFull tests command dispatch when minion channel is full
func TestSendCommandChannelFull(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	minionID := "test-minion"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id: minionID,
		},
		CommandCh: make(chan *pb.Command, 1), // Small buffer
		LastSeen:  time.Now(),
	}

	// Fill the channel
	server.GetMinionRegistryImpl().minions[minionID].CommandCh <- &pb.Command{Id: "existing"}

	// Mock database insert
	mock.ExpectExec("INSERT INTO commands \\(id, host_id, command, timestamp, direction, status\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6\\)").
		WithArgs(sqlmock.AnyArg(), minionID, "ls -la", sqlmock.AnyArg(), "SENT", "PENDING").
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := &pb.CommandRequest{
		MinionIds: []string{minionID},
		Command: &pb.Command{
			Type:    pb.CommandType_SYSTEM,
			Payload: "ls -la",
		},
	}

	response, err := server.SendCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("SendCommand failed: %v", err)
	}

	if !response.Accepted {
		t.Error("Expected command to be accepted even if channel is full")
	}

	// The command should still be logged to database but not sent to the full channel
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}

// TestDatabaseErrors tests various database error scenarios
func TestDatabaseErrors(t *testing.T) {
	t.Run("register update error", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("Failed to create mock database: %v", err)
		}
		defer db.Close()

		server := createTestServer(db)

		mock.ExpectExec("INSERT INTO hosts \\(id, hostname, ip, os, first_seen, last_seen, tags\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6, \\$7\\) ON CONFLICT \\(id\\) DO UPDATE SET hostname = EXCLUDED.hostname, ip = EXCLUDED.ip, os = EXCLUDED.os, last_seen = EXCLUDED.last_seen, tags = EXCLUDED.tags").
			WillReturnError(fmt.Errorf("database connection failed"))

		hostInfo := &pb.HostInfo{
			Id:       "test-minion",
			Hostname: "test-host",
			Ip:       "192.168.1.100",
			Os:       "linux",
			Tags:     make(map[string]string),
		}

		_, err = server.Register(context.Background(), hostInfo)
		if err == nil {
			t.Error("Expected error from database update failure")
		}
	})

	t.Run("register insert error", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("Failed to create mock database: %v", err)
		}
		defer db.Close()

		server := createTestServer(db)

		mock.ExpectExec("INSERT INTO hosts \\(id, hostname, ip, os, first_seen, last_seen, tags\\) VALUES \\(\\$1, \\$2, \\$3, \\$4, \\$5, \\$6, \\$7\\) ON CONFLICT \\(id\\) DO UPDATE SET hostname = EXCLUDED.hostname, ip = EXCLUDED.ip, os = EXCLUDED.os, last_seen = EXCLUDED.last_seen, tags = EXCLUDED.tags").
			WillReturnError(fmt.Errorf("insert failed"))

		hostInfo := &pb.HostInfo{
			Id:       "test-minion",
			Hostname: "test-host",
			Ip:       "192.168.1.100",
			Os:       "linux",
			Tags:     make(map[string]string),
		}

		_, err = server.Register(context.Background(), hostInfo)
		if err == nil {
			t.Error("Expected error from database insert failure")
		}
	})

	t.Run("set tags database update error", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("Failed to create mock database: %v", err)
		}
		defer db.Close()

		server := createTestServer(db)

		minionID := "test-minion"
		server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
			Info: &pb.HostInfo{
				Id:   minionID,
				Tags: make(map[string]string),
			},
		}

		// Mock database update error
		mock.ExpectExec("UPDATE hosts SET tags=\\$2 WHERE id=\\$1").
			WithArgs(minionID, `{"test":"value"}`).
			WillReturnError(fmt.Errorf("database update failed"))

		req := &pb.SetTagsRequest{
			MinionId: minionID,
			Tags: map[string]string{
				"test": "value",
			},
		}

		response, err := server.SetTags(context.Background(), req)
		if err == nil {
			t.Error("Expected error from database update failure")
		}
		if response.Success {
			t.Error("Expected SetTags to fail due to database error")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled mock expectations: %v", err)
		}
	})
}

// TestRegisterWithoutDatabase tests registration without database connection
func TestRegisterWithoutDatabase(t *testing.T) {
	server := createTestServer(nil) // No database

	hostInfo := &pb.HostInfo{
		Id:       "test-minion",
		Hostname: "test-host",
		Ip:       "192.168.1.100",
		Os:       "linux",
		Tags:     make(map[string]string),
	}

	response, err := server.Register(context.Background(), hostInfo)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if !response.Success {
		t.Error("Expected registration to succeed without database")
	}

	// Verify in-memory storage works
	registry := server.GetMinionRegistryImpl()
	registry.minionsMu.RLock()
	_, exists := registry.minions[hostInfo.Id]
	registry.minionsMu.RUnlock()

	if !exists {
		t.Error("Expected minion to be stored in memory")
	}
}

// TestSetTagsWithoutDatabase tests tag setting without database
func TestSetTagsWithoutDatabase(t *testing.T) {
	server := createTestServer(nil) // No database

	minionID := "test-minion"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info: &pb.HostInfo{
			Id:   minionID,
			Tags: make(map[string]string),
		},
	}

	req := &pb.SetTagsRequest{
		MinionId: minionID,
		Tags:     map[string]string{"env": "test"},
	}

	response, err := server.SetTags(context.Background(), req)
	if err != nil {
		t.Fatalf("SetTags failed: %v", err)
	}

	if !response.Success {
		t.Error("Expected SetTags to succeed without database")
	}

	// Verify in-memory update
	if server.GetMinionRegistryImpl().minions[minionID].Info.Tags["env"] != "test" {
		t.Error("Expected in-memory tags to be updated")
	}
}

// TestSendCommandWithoutDatabase tests command sending without database
func TestSendCommandWithoutDatabase(t *testing.T) {
	server := createTestServer(nil) // No database

	minionID := "test-minion"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info:      &pb.HostInfo{Id: minionID},
		CommandCh: make(chan *pb.Command, 100),
	}

	req := &pb.CommandRequest{
		MinionIds: []string{minionID},
		Command: &pb.Command{
			Type:    pb.CommandType_SYSTEM,
			Payload: "echo hello",
		},
	}

	response, err := server.SendCommand(context.Background(), req)
	if err != nil {
		t.Fatalf("SendCommand failed: %v", err)
	}

	if !response.Accepted {
		t.Error("Expected command to be accepted without database")
	}

	// Verify command was sent
	select {
	case cmd := <-server.GetMinionRegistryImpl().minions[minionID].CommandCh:
		if cmd.Payload != "echo hello" {
			t.Errorf("Expected payload 'echo hello', got '%s'", cmd.Payload)
		}
	default:
		t.Error("Expected command to be sent to minion")
	}
}

// TestConcurrentAccess tests concurrent access to server state
func TestConcurrentAccess(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add some minions
	for i := 0; i < 10; i++ {
		minionID := fmt.Sprintf("minion-%d", i)
		server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
			Info: &pb.HostInfo{
				Id:   minionID,
				Tags: map[string]string{"index": fmt.Sprintf("%d", i)},
			},
			CommandCh: make(chan *pb.Command, 100),
			LastSeen:  time.Now(),
		}
	}

	// Run concurrent operations
	var wg sync.WaitGroup

	// Concurrent ListMinions calls
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := server.ListMinions(context.Background(), &pb.Empty{})
			if err != nil {
				t.Errorf("ListMinions failed: %v", err)
			}
		}()
	}

	// Concurrent ListTags calls
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := server.ListTags(context.Background(), &pb.Empty{})
			if err != nil {
				t.Errorf("ListTags failed: %v", err)
			}
		}()
	}

	// Concurrent FindTargetMinions calls
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			req := &pb.CommandRequest{
				TagSelector: &pb.TagSelector{
					Rules: []*pb.TagMatch{
						{
							Key: "index",
							Condition: &pb.TagMatch_Equals{
								Equals: fmt.Sprintf("%d", index),
							},
						},
					},
				},
			}
			targets := server.FindTargetMinions(req)
			if len(targets) != 1 {
				t.Errorf("Expected 1 target, got %d", len(targets))
			}
		}(i)
	}

	wg.Wait()
}

// TestStreamCommandsWithStatusUpdates tests handling of command status updates through the stream
func TestStreamCommandsWithStatusUpdates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	server := createTestServer(db)

	// Add test minion
	minionID := "test-minion"
	server.GetMinionRegistryImpl().minions[minionID] = &MinionConnectionImpl{
		Info:      &pb.HostInfo{Id: minionID},
		CommandCh: make(chan *pb.Command, 10),
		LastSeen:  time.Now(),
	}

	// Mock database operations for status update
	mock.ExpectExec("UPDATE commands SET status = \\$1 WHERE id = \\$2").
		WithArgs("EXECUTING", "cmd-123").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("UPDATE commands SET status = \\$1 WHERE id = \\$2").
		WithArgs("COMPLETED", "cmd-123").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Create test messages
	recvMsgs := []*pb.CommandStreamMessage{
		{
			Message: &pb.CommandStreamMessage_Status{
				Status: &pb.CommandStatusUpdate{
					CommandId: "cmd-123",
					MinionId:  minionID,
					Status:    "EXECUTING",
					Timestamp: time.Now().Unix(),
				},
			},
		},
		{
			Message: &pb.CommandStreamMessage_Status{
				Status: &pb.CommandStatusUpdate{
					CommandId: "cmd-123",
					MinionId:  minionID,
					Status:    "COMPLETED",
					Timestamp: time.Now().Unix(),
				},
			},
		},
	}

	md := metadata.New(map[string]string{"minion-id": minionID})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	stream := &MockStreamServer{
		ctx:      ctx,
		recvMsgs: recvMsgs,
	}

	err = server.StreamCommands(stream)
	if err != nil && err != io.EOF {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify database expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled mock expectations: %v", err)
	}
}
