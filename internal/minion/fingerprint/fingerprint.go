package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"go.uber.org/zap"
)

// Generator handles hardware fingerprint generation
type Generator struct {
	logger *zap.Logger
}

// NewGenerator creates a new fingerprint generator
func NewGenerator(logger *zap.Logger) *Generator {
	return &Generator{
		logger: logger,
	}
}

// Generate creates a unique hardware fingerprint
func (g *Generator) Generate() (string, error) {
	g.logger.Debug("Generating hardware fingerprint")

	var identifiers []string

	switch runtime.GOOS {
	case "linux":
		identifiers = append(identifiers, g.getLinuxIdentifiers()...)
	case "darwin":
		identifiers = append(identifiers, g.getDarwinIdentifiers()...)
	case "windows":
		identifiers = append(identifiers, g.getWindowsIdentifiers()...)
	default:
		g.logger.Warn("Unsupported OS for detailed hardware fingerprinting",
			zap.String("os", runtime.GOOS))
	}

	// Add common identifiers
	commonIds, err := g.getCommonIdentifiers()
	if err != nil {
		g.logger.Error("Failed to get common identifiers", zap.Error(err))
	} else {
		identifiers = append(identifiers, commonIds...)
	}

	// If we couldn't get any hardware identifiers, use basic system info
	if len(identifiers) == 0 {
		g.logger.Warn("No hardware identifiers found, using basic system info")
		identifiers = append(identifiers,
			runtime.GOOS,
			runtime.GOARCH,
			g.getHostname(),
		)
	}

	// Generate hash from all identifiers
	hash := sha256.New()
	for _, id := range identifiers {
		hash.Write([]byte(id))
	}

	fingerprint := hex.EncodeToString(hash.Sum(nil))
	g.logger.Debug("Generated hardware fingerprint",
		zap.String("fingerprint", fingerprint))

	return fingerprint, nil
}

// getLinuxIdentifiers gets hardware identifiers specific to Linux
func (g *Generator) getLinuxIdentifiers() []string {
	var identifiers []string

	// CPU info
	if cpuInfo, err := exec.Command("cat", "/proc/cpuinfo").Output(); err == nil {
		identifiers = append(identifiers, string(cpuInfo))
	}

	// System UUID
	if systemUUID, err := exec.Command("cat", "/sys/class/dmi/id/product_uuid").Output(); err == nil {
		identifiers = append(identifiers, string(systemUUID))
	}

	// Motherboard serial
	if boardSerial, err := exec.Command("cat", "/sys/class/dmi/id/board_serial").Output(); err == nil {
		identifiers = append(identifiers, string(boardSerial))
	}

	return identifiers
}

// getDarwinIdentifiers gets hardware identifiers specific to macOS
func (g *Generator) getDarwinIdentifiers() []string {
	var identifiers []string

	// System profile
	if profileOut, err := exec.Command("system_profiler", "SPHardwareDataType").Output(); err == nil {
		identifiers = append(identifiers, string(profileOut))
	}

	// IOPlatform UUID
	if uuidOut, err := exec.Command("ioreg", "-d2", "-c", "IOPlatformExpertDevice").Output(); err == nil {
		identifiers = append(identifiers, string(uuidOut))
	}

	return identifiers
}

// getWindowsIdentifiers gets hardware identifiers specific to Windows
func (g *Generator) getWindowsIdentifiers() []string {
	var identifiers []string

	// WMIC queries for hardware info
	queries := []string{
		"csproduct get uuid",         // System UUID
		"bios get serialnumber",      // BIOS serial
		"baseboard get serialnumber", // Motherboard serial
		"cpu get processorid",        // CPU ID
	}

	for _, query := range queries {
		if out, err := exec.Command("wmic", strings.Split(query, " ")...).Output(); err == nil {
			identifiers = append(identifiers, string(out))
		}
	}

	return identifiers
}

// getCommonIdentifiers gets hardware identifiers common to all platforms
func (g *Generator) getCommonIdentifiers() ([]string, error) {
	var identifiers []string

	// Get network interfaces
	netOut, err := exec.Command("ip", "link").Output()
	if err == nil {
		identifiers = append(identifiers, string(netOut))
	}

	// Get disk info
	diskOut, err := exec.Command("df").Output()
	if err == nil {
		identifiers = append(identifiers, string(diskOut))
	}

	return identifiers, nil
}

// getHostname gets the system hostname
func (g *Generator) getHostname() string {
	hostname, err := exec.Command("hostname").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(hostname))
}

// ValidateFingerprint checks if a fingerprint matches the current hardware
func (g *Generator) ValidateFingerprint(storedFingerprint string) (bool, error) {
	currentFingerprint, err := g.Generate()
	if err != nil {
		return false, fmt.Errorf("failed to generate current fingerprint: %v", err)
	}

	return currentFingerprint == storedFingerprint, nil
}
