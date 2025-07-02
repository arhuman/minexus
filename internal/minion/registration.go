package minion

import (
	"context"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"

	pb "github.com/arhuman/minexus/protogen"

	"go.uber.org/zap"
	
	"github.com/arhuman/minexus/internal/logging"
)

// registrationManager implements the RegistrationManager interface
type registrationManager struct {
	id            string
	service       pb.MinionServiceClient
	connectionMgr ConnectionManager
	logger        *zap.Logger
}

// NewRegistrationManager creates a new registration manager
func NewRegistrationManager(id string, service pb.MinionServiceClient, connMgr ConnectionManager, logger *zap.Logger) *registrationManager {
	logger, start := logging.FuncLogger(logger, "NewRegistrationManager")
	defer logging.FuncExit(logger, start)
	
	return &registrationManager{
		id:            id,
		service:       service,
		connectionMgr: connMgr,
		logger:        logger,
	}
}

// Register performs initial registration with the nexus server using host information
func (rm *registrationManager) Register(ctx context.Context, hostInfo *pb.HostInfo) (*pb.RegisterResponse, error) {
	logger, start := logging.FuncLogger(rm.logger, "registrationManager.Register")
	defer logging.FuncExit(logger, start)
	
	logger.Debug("Creating host info for registration")
	if hostInfo == nil {
		var err error
		hostInfo, err = rm.createHostInfo()
		if err != nil {
			logger.Error("Failed to create host info", zap.Error(err))
			return nil, err
		}
	}

	logger.Debug("Calling Register gRPC method")
	resp, err := rm.service.Register(ctx, hostInfo)
	if err != nil {
		logger.Error("Failed to register minion", zap.Error(err))
		return nil, err
	}

	if !resp.Success {
		logger.Error("Registration unsuccessful",
			zap.String("error", resp.ErrorMessage))

		return resp, nil
	}

	logger.Debug("Registration successful")

	// If server assigned a new ID, update it
	if resp.AssignedId != "" && resp.AssignedId != rm.id {
		rm.id = resp.AssignedId
		logger.Info("Using server-assigned ID", zap.String("id", rm.id))
	}

	return resp, nil
}

// PeriodicRegister performs periodic registration heartbeats at the specified interval
func (rm *registrationManager) PeriodicRegister(ctx context.Context, interval time.Duration) error {
	logger, start := logging.FuncLogger(rm.logger, "registrationManager.PeriodicRegister")
	defer logging.FuncExit(logger, start)
	
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Debug("Starting periodic registration",
		zap.Duration("interval", interval),
		zap.String("minion_id", rm.id))

	for {
		select {
		case <-ctx.Done():
			logger.Debug("Context cancelled, stopping periodic registration")
			return ctx.Err()
		case <-ticker.C:
			// Create updated host info for heartbeat
			hostInfo, err := rm.createHostInfo()
			if err != nil {
				logger.Error("Failed to create host info for periodic registration", zap.Error(err))
				continue
			}

			logger.Debug("Performing periodic registration",
				zap.String("minion_id", rm.id))

			// Attempt to register
			resp, err := rm.service.Register(ctx, hostInfo)
			if err != nil {
				logger.Error("Periodic registration failed", zap.Error(err))
				continue
			}

			if !resp.Success {
				logger.Error("Periodic registration unsuccessful",
					zap.String("error", resp.ErrorMessage))
				continue
			}

			logger.Debug("Periodic registration successful",
				zap.String("minion_id", rm.id))
		}
	}
}

// createHostInfo creates host information for registration
func (rm *registrationManager) createHostInfo() (*pb.HostInfo, error) {

	return &pb.HostInfo{
		Id:       rm.id,
		Hostname: getHostname(),
		Ip:       rm.getIPAddress(),
		Os:       runtime.GOOS,
		Tags:     make(map[string]string),
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

// getIPAddress returns the IP address used for connecting to the nexus server.
// It first tries to get the local address if connected, then falls back to network interface detection.
func (rm *registrationManager) getIPAddress() string {
	logger, start := logging.FuncLogger(rm.logger, "registrationManager.getIPAddress")
	defer logging.FuncExit(logger, start)
	// Try to get the local address if we're connected
	if rm.connectionMgr.IsConnected() {
		// Create a UDP connection to determine the outbound IP
		conn, err := net.Dial("udp", "8.8.8.8:80")
		if err == nil {
			defer conn.Close()
			if localAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
				if !localAddr.IP.IsLoopback() && localAddr.IP.To4() != nil {
					logger.Debug("Using IP from active connection", zap.String("ip", localAddr.IP.String()))
					return localAddr.IP.String()
				}
			}
		}
	}

	// Fallback to interface detection
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		logger.Error("Failed to get interface addresses", zap.Error(err))
		return "unknown"
	}

	// Look for non-loopback IPv4 address
	for _, addr := range addrs {
		if addr == nil {
			continue
		}

		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.IsUnspecified() {
			continue
		}

		if ip4 := ipNet.IP.To4(); ip4 != nil {
			logger.Debug("Using IP from network interface", zap.String("ip", ip4.String()))
			return ip4.String()
		}
	}

	logger.Warn("No suitable network interface found")
	return "unknown"
}
