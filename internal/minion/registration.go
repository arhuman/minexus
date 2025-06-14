package minion

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"minexus/internal/minion/fingerprint"
	pb "minexus/protogen"

	"go.uber.org/zap"
)

// registrationManager implements the RegistrationManager interface
type registrationManager struct {
	id                string
	service           pb.MinionServiceClient
	logger            *zap.Logger
	fingerprintGen    *fingerprint.Generator
	registrationCount int32
}

// NewRegistrationManager creates a new registration manager
func NewRegistrationManager(id string, service pb.MinionServiceClient, logger *zap.Logger) *registrationManager {
	return &registrationManager{
		id:             id,
		service:        service,
		logger:         logger,
		fingerprintGen: fingerprint.NewGenerator(logger),
	}
}

// Register performs initial registration with the nexus server using host information
func (rm *registrationManager) Register(ctx context.Context, hostInfo *pb.HostInfo) (*pb.RegisterResponse, error) {
	rm.logger.Debug("Creating host info for registration")
	if hostInfo == nil {
		var err error
		hostInfo, err = rm.createHostInfo()
		if err != nil {
			rm.logger.Error("Failed to create host info", zap.Error(err))
			return nil, err
		}
	}

	rm.logger.Debug("Calling Register gRPC method")
	resp, err := rm.service.Register(ctx, hostInfo)
	if err != nil {
		rm.logger.Error("Failed to register minion", zap.Error(err))
		return nil, err
	}

	if !resp.Success {
		rm.logger.Error("Registration unsuccessful",
			zap.String("error", resp.ErrorMessage),
			zap.String("conflict_status", resp.ConflictStatus))

		// Handle conflict status
		if resp.ConflictStatus != "" {
			rm.logger.Info("Registration conflict detected",
				zap.String("status", resp.ConflictStatus),
				zap.Any("details", resp.ConflictDetails))
		}

		return resp, nil
	}

	rm.logger.Debug("Registration successful")

	// Update registration history
	if resp.RegistrationHistory != nil {
		rm.registrationCount = resp.RegistrationHistory.RegistrationCount
	}

	// If server assigned a new ID, update it
	if resp.AssignedId != "" && resp.AssignedId != rm.id {
		rm.id = resp.AssignedId
		rm.logger.Info("Using server-assigned ID", zap.String("id", rm.id))
	}

	return resp, nil
}

// PeriodicRegister performs periodic registration heartbeats at the specified interval
func (rm *registrationManager) PeriodicRegister(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	rm.logger.Debug("Starting periodic registration",
		zap.Duration("interval", interval),
		zap.String("minion_id", rm.id))

	for {
		select {
		case <-ctx.Done():
			rm.logger.Debug("Context cancelled, stopping periodic registration")
			return ctx.Err()
		case <-ticker.C:
			// Create updated host info for heartbeat
			hostInfo, err := rm.createHostInfo()
			if err != nil {
				rm.logger.Error("Failed to create host info for periodic registration", zap.Error(err))
				continue
			}

			rm.logger.Debug("Performing periodic registration",
				zap.String("minion_id", rm.id))

			// Attempt to register
			resp, err := rm.service.Register(ctx, hostInfo)
			if err != nil {
				rm.logger.Error("Periodic registration failed", zap.Error(err))
				continue
			}

			if !resp.Success {
				rm.logger.Error("Periodic registration unsuccessful",
					zap.String("error", resp.ErrorMessage),
					zap.String("conflict_status", resp.ConflictStatus))

				// Handle conflict status
				if resp.ConflictStatus != "" {
					rm.logger.Info("Registration conflict detected",
						zap.String("status", resp.ConflictStatus),
						zap.Any("details", resp.ConflictDetails))
				}
				continue
			}

			// Update registration history
			if resp.RegistrationHistory != nil {
				rm.registrationCount = resp.RegistrationHistory.RegistrationCount
			}

			rm.logger.Debug("Periodic registration successful",
				zap.String("minion_id", rm.id),
				zap.Int32("registration_count", rm.registrationCount))
		}
	}
}

// createHostInfo creates host information for registration
func (rm *registrationManager) createHostInfo() (*pb.HostInfo, error) {
	// Generate hardware fingerprint
	hwFingerprint, err := rm.fingerprintGen.Generate()
	if err != nil {
		return nil, err
	}

	// Create registration history if this is first registration
	var regHistory *pb.RegistrationHistory
	if rm.registrationCount == 0 {
		now := time.Now().Unix()
		regHistory = &pb.RegistrationHistory{
			Registrations:     make([]*pb.RegistrationHistory_Registration, 0),
			Conflicts:         make([]*pb.RegistrationHistory_Conflict, 0),
			FirstSeen:         now,
			RegistrationCount: 0,
		}

		// Add initial registration entry
		initialReg := &pb.RegistrationHistory_Registration{
			Timestamp:           now,
			Id:                  rm.id,
			Ip:                  getIPAddress(),
			Hostname:            getHostname(),
			HardwareFingerprint: hwFingerprint,
		}
		regHistory.Registrations = append(regHistory.Registrations, initialReg)
	}

	return &pb.HostInfo{
		Id:                  rm.id,
		Hostname:            getHostname(),
		Ip:                  getIPAddress(),
		Os:                  runtime.GOOS,
		Tags:                make(map[string]string),
		HardwareFingerprint: hwFingerprint,
		RegistrationHistory: regHistory,
	}, nil
}

// GetMinionID returns the current minion ID
func (rm *registrationManager) GetMinionID() string {
	return rm.id
}

// UpdateMinionID updates the minion ID used for registration
func (rm *registrationManager) UpdateMinionID(newID string) {
	rm.id = newID
}

// Helper functions
func getHostname() string {
	hostname, err := exec.Command("hostname").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(hostname))
}

func getIPAddress() string {
	// This is a simplified implementation
	// In production, you'd want a more robust method
	return "127.0.0.1"
}
