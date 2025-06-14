package nexus

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "minexus/protogen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MinionConnectionImpl implements the MinionConnection interface.
// It represents an active connection to a minion node in the system.
type MinionConnectionImpl struct {
	Info          *pb.HostInfo     // Host information including ID, hostname, IP, OS, and tags
	LastSeen      time.Time        // Timestamp of the last communication from this minion
	CommandCh     chan *pb.Command // Channel for sending commands to this minion
	NextSeqNumber uint64           // Next sequence number to assign to commands for this minion
}

// GetInfo returns the host information for this minion connection.
func (m *MinionConnectionImpl) GetInfo() *pb.HostInfo {
	return m.Info
}

// MinionRegistryImpl manages minion connections and tag operations.
// It provides methods to register minions, manage connections, and perform tag-based operations.
type MinionRegistryImpl struct {
	minions   map[string]*MinionConnectionImpl
	minionsMu sync.RWMutex
	dbService *DatabaseServiceImpl

	// Maps for conflict detection
	fingerprintMap map[string]string // hardware fingerprint -> minion ID
	hostnameMap    map[string]string // hostname -> minion ID
	ipMap          map[string]string // ip -> minion ID
}

// NewMinionRegistry creates a new minion registry instance.
func NewMinionRegistry(dbService *DatabaseServiceImpl) *MinionRegistryImpl {
	return &MinionRegistryImpl{
		minions:        make(map[string]*MinionConnectionImpl),
		dbService:      dbService,
		fingerprintMap: make(map[string]string),
		hostnameMap:    make(map[string]string),
		ipMap:          make(map[string]string),
	}
}

// Register adds or updates a minion in the registry using host information.
func (r *MinionRegistryImpl) Register(hostInfo *pb.HostInfo) (*pb.RegisterResponse, error) {
	// Initialize tags if nil
	if hostInfo.Tags == nil {
		hostInfo.Tags = make(map[string]string)
	}

	// Store minion connection in memory
	r.minionsMu.Lock()
	defer r.minionsMu.Unlock()

	// Check for conflicts
	conflicts := r.detectConflicts(hostInfo)
	if len(conflicts) > 0 {
		// Get the existing minion that has the conflict
		existingID := conflicts["existing_id"]
		existing := r.minions[existingID]

		// Set conflict status on existing minion
		existing.Info.ConflictStatus = "pending"

		// Record conflict in history
		conflict := &pb.RegistrationHistory_Conflict{
			Timestamp:  time.Now().Unix(),
			Type:       conflicts["type"],
			Resolution: "pending",
			Details:    conflicts,
		}
		existing.Info.RegistrationHistory.Conflicts = append(
			existing.Info.RegistrationHistory.Conflicts,
			conflict,
		)

		// Store conflict in database
		if r.dbService != nil {
			// Set conflict status on the host info
			existing.Info.ConflictStatus = "pending"
			if err := r.dbService.StoreHost(context.Background(), existing.Info); err != nil {
				return nil, fmt.Errorf("failed to store conflict: %v", err)
			}
		}

		return &pb.RegisterResponse{
			Success:         false,
			ConflictStatus:  "pending",
			ConflictDetails: conflicts,
			ErrorMessage:    "Registration conflicts detected",
		}, nil
	}

	// Check if minion already exists to preserve existing channel
	if existing, exists := r.minions[hostInfo.Id]; exists {
		// Update registration history
		registration := &pb.RegistrationHistory_Registration{
			Timestamp:           time.Now().Unix(),
			Id:                  hostInfo.Id,
			Ip:                  hostInfo.Ip,
			Hostname:            hostInfo.Hostname,
			HardwareFingerprint: hostInfo.HardwareFingerprint,
		}
		existing.Info.RegistrationHistory.Registrations = append(
			existing.Info.RegistrationHistory.Registrations,
			registration,
		)
		existing.Info.RegistrationHistory.RegistrationCount++

		// Update existing connection but preserve the command channel
		existing.Info = hostInfo
		existing.LastSeen = time.Now()

		// Update maps
		r.updateMaps(hostInfo)

		// Update database if available
		if r.dbService != nil {
			if err := r.dbService.UpdateHost(context.Background(), hostInfo); err != nil {
				return nil, err
			}
		}

		return &pb.RegisterResponse{
			Success:             true,
			AssignedId:          hostInfo.Id,
			RegistrationHistory: existing.Info.RegistrationHistory,
		}, nil
	}

	// Create new connection
	now := time.Now().Unix()
	regHistory := &pb.RegistrationHistory{
		Registrations: []*pb.RegistrationHistory_Registration{
			{
				Timestamp:           now,
				Id:                  hostInfo.Id,
				Ip:                  hostInfo.Ip,
				Hostname:            hostInfo.Hostname,
				HardwareFingerprint: hostInfo.HardwareFingerprint,
			},
		},
		Conflicts:         make([]*pb.RegistrationHistory_Conflict, 0),
		FirstSeen:         now,
		RegistrationCount: 1,
	}

	hostInfo.RegistrationHistory = regHistory
	r.minions[hostInfo.Id] = &MinionConnectionImpl{
		Info:          hostInfo,
		LastSeen:      time.Now(),
		CommandCh:     make(chan *pb.Command, 100),
		NextSeqNumber: 1,
	}

	// Update maps
	r.updateMaps(hostInfo)

	// Store in database if available
	if r.dbService != nil {
		if err := r.dbService.StoreHost(context.Background(), hostInfo); err != nil {
			return nil, err
		}
	}

	return &pb.RegisterResponse{
		Success:             true,
		AssignedId:          hostInfo.Id,
		RegistrationHistory: regHistory,
	}, nil
}

// detectConflicts checks for any conflicts with existing registrations
func (r *MinionRegistryImpl) detectConflicts(hostInfo *pb.HostInfo) map[string]string {
	conflicts := make(map[string]string)

	// Check hardware fingerprint conflicts
	if existingID, exists := r.fingerprintMap[hostInfo.HardwareFingerprint]; exists && existingID != hostInfo.Id {
		conflicts["type"] = "hardware_mismatch"
		conflicts["existing_id"] = existingID
		conflicts["hardware_fingerprint"] = hostInfo.HardwareFingerprint
	}

	// Check hostname conflicts
	if existingID, exists := r.hostnameMap[hostInfo.Hostname]; exists && existingID != hostInfo.Id {
		if len(conflicts) == 0 {
			conflicts["type"] = "duplicate_hostname"
		}
		conflicts["existing_hostname_id"] = existingID
		conflicts["hostname"] = hostInfo.Hostname
	}

	// Check IP conflicts
	if existingID, exists := r.ipMap[hostInfo.Ip]; exists && existingID != hostInfo.Id {
		if len(conflicts) == 0 {
			conflicts["type"] = "duplicate_ip"
		}
		conflicts["existing_ip_id"] = existingID
		conflicts["ip"] = hostInfo.Ip
	}

	return conflicts
}

// updateMaps updates the mapping tables used for conflict detection
func (r *MinionRegistryImpl) updateMaps(hostInfo *pb.HostInfo) {
	r.fingerprintMap[hostInfo.HardwareFingerprint] = hostInfo.Id
	r.hostnameMap[hostInfo.Hostname] = hostInfo.Id
	r.ipMap[hostInfo.Ip] = hostInfo.Id
}

// GetConnection retrieves the connection information for a specific minion.
func (r *MinionRegistryImpl) GetConnection(minionID string) (MinionConnection, bool) {
	r.minionsMu.RLock()
	defer r.minionsMu.RUnlock()

	conn, exists := r.minions[minionID]
	if !exists {
		return nil, false
	}

	return conn, true
}

// GetConnectionImpl retrieves the concrete connection implementation for internal use.
// This method is used internally by the nexus server for direct access to channels.
func (r *MinionRegistryImpl) GetConnectionImpl(minionID string) (*MinionConnectionImpl, bool) {
	r.minionsMu.RLock()
	defer r.minionsMu.RUnlock()

	conn, exists := r.minions[minionID]
	return conn, exists
}

// UpdateLastSeen updates the last seen timestamp for a minion.
func (r *MinionRegistryImpl) UpdateLastSeen(minionID string) {
	r.minionsMu.Lock()
	defer r.minionsMu.Unlock()

	if conn, exists := r.minions[minionID]; exists {
		conn.LastSeen = time.Now()
	}
}

// ListMinions returns a list of all registered minions.
func (r *MinionRegistryImpl) ListMinions() []*pb.HostInfo {
	r.minionsMu.RLock()
	defer r.minionsMu.RUnlock()

	var minions []*pb.HostInfo

	// Use in-memory data to ensure consistency with command targeting
	// This shows only currently connected minions that can receive commands
	for _, conn := range r.minions {
		// Create a copy of the HostInfo to avoid modifying the original
		hostInfo := &pb.HostInfo{
			Id:                  conn.Info.Id,
			Hostname:            conn.Info.Hostname,
			Ip:                  conn.Info.Ip,
			Os:                  conn.Info.Os,
			LastSeen:            conn.LastSeen.Unix(),
			Tags:                make(map[string]string),
			HardwareFingerprint: conn.Info.HardwareFingerprint,
			RegistrationHistory: conn.Info.RegistrationHistory,
			ConflictStatus:      conn.Info.ConflictStatus,
		}

		// Copy tags to avoid modification of original
		for k, v := range conn.Info.Tags {
			hostInfo.Tags[k] = v
		}

		minions = append(minions, hostInfo)
	}

	return minions
}

// FindTargetMinions identifies minions that match the criteria in the command request.
func (r *MinionRegistryImpl) FindTargetMinions(req *pb.CommandRequest) []string {
	r.minionsMu.RLock()
	defer r.minionsMu.RUnlock()

	// If specific minion IDs are provided, use those
	if len(req.MinionIds) > 0 {
		var targets []string
		for _, id := range req.MinionIds {
			if _, exists := r.minions[id]; exists {
				targets = append(targets, id)
			}
		}
		return targets
	}

	// Otherwise, use tag selector to find matching minions
	var targets []string
	for id, conn := range r.minions {
		if r.matchesTags(conn.Info, req.TagSelector) {
			targets = append(targets, id)
		}
	}

	return targets
}

// matchesTags checks if a HostInfo matches the given TagSelector.
func (r *MinionRegistryImpl) matchesTags(info *pb.HostInfo, selector *pb.TagSelector) bool {
	if selector == nil {
		return true
	}

	for _, rule := range selector.Rules {
		switch condition := rule.Condition.(type) {
		case *pb.TagMatch_Equals:
			if value, exists := info.Tags[rule.Key]; !exists || value != condition.Equals {
				return false
			}
		case *pb.TagMatch_Exists:
			if condition.Exists {
				if _, exists := info.Tags[rule.Key]; !exists {
					return false
				}
			}
		case *pb.TagMatch_NotExists:
			if condition.NotExists {
				if _, exists := info.Tags[rule.Key]; exists {
					return false
				}
			}
		}
	}

	return true
}

// UpdateTags adds and removes tags for a specific minion.
func (r *MinionRegistryImpl) UpdateTags(minionID string, add map[string]string, remove []string) error {
	r.minionsMu.Lock()
	defer r.minionsMu.Unlock()

	conn, exists := r.minions[minionID]
	if !exists {
		return status.Error(codes.NotFound, "minion not found")
	}

	// Create a deep copy of the host info to avoid modifying the original
	updatedInfo := &pb.HostInfo{
		Id:                  conn.Info.Id,
		Hostname:            conn.Info.Hostname,
		Ip:                  conn.Info.Ip,
		Os:                  conn.Info.Os,
		Tags:                make(map[string]string),
		HardwareFingerprint: conn.Info.HardwareFingerprint,
		RegistrationHistory: conn.Info.RegistrationHistory,
		ConflictStatus:      conn.Info.ConflictStatus,
	}

	// Copy existing tags first
	for k, v := range conn.Info.Tags {
		updatedInfo.Tags[k] = v
	}

	// Add new tags
	for key, value := range add {
		updatedInfo.Tags[key] = value
		conn.Info.Tags[key] = value
	}

	// Remove specified tags
	for _, key := range remove {
		delete(updatedInfo.Tags, key)
		delete(conn.Info.Tags, key)
	}

	// Update database if available
	if r.dbService != nil {
		return r.dbService.updateHostTags(context.Background(), minionID, updatedInfo)
	}

	return nil
}

// SetTags replaces all tags for a specific minion with the provided tags.
func (r *MinionRegistryImpl) SetTags(minionID string, tags map[string]string) error {
	r.minionsMu.Lock()
	defer r.minionsMu.Unlock()

	conn, exists := r.minions[minionID]
	if !exists {
		return status.Error(codes.NotFound, "minion not found")
	}

	// Create a deep copy of the host info to avoid modifying the original
	updatedInfo := &pb.HostInfo{
		Id:                  conn.Info.Id,
		Hostname:            conn.Info.Hostname,
		Ip:                  conn.Info.Ip,
		Os:                  conn.Info.Os,
		Tags:                make(map[string]string),
		HardwareFingerprint: conn.Info.HardwareFingerprint,
		RegistrationHistory: conn.Info.RegistrationHistory,
		ConflictStatus:      conn.Info.ConflictStatus,
	}

	// Copy existing tags first
	for k, v := range conn.Info.Tags {
		updatedInfo.Tags[k] = v
	}

	// Replace all tags in memory
	for key, value := range tags {
		updatedInfo.Tags[key] = value
		conn.Info.Tags[key] = value
	}

	// Update database if available
	if r.dbService != nil {
		return r.dbService.updateHostTags(context.Background(), minionID, updatedInfo)
	}

	return nil
}

// ListTags returns all available tags in the system.
// Tags are used for grouping and selecting minions for command execution.
func (r *MinionRegistryImpl) ListTags() []string {
	r.minionsMu.RLock()
	defer r.minionsMu.RUnlock()

	tagSet := make(map[string]bool)
	for _, conn := range r.minions {
		for key, value := range conn.Info.Tags {
			tagSet[fmt.Sprintf("%s:%s", key, value)] = true
		}
	}

	var tags []string
	for tag := range tagSet {
		tags = append(tags, tag)
	}

	return tags
}
