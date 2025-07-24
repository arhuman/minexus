package config

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/arhuman/minexus/internal/logging"
)

// Environment constants for MINEXUS_ENV
const (
	EnvProduction = "prod"
	EnvTest       = "test"
)

// DetectEnvironment returns the current environment based on MINEXUS_ENV
// Panics on invalid environment values to prevent configuration errors
func DetectEnvironment() string {
	env := os.Getenv("MINEXUS_ENV")
	if env == "" {
		env = EnvTest // Default to test
	}

	// Strict validation - panic on invalid environment to prevent typos
	validEnvs := map[string]bool{
		EnvProduction: true,
		EnvTest:       true,
	}

	if !validEnvs[env] {
		panic(fmt.Sprintf("Invalid MINEXUS_ENV value: '%s'. Valid values are: %s, %s",
			env, EnvProduction, EnvTest))
	}

	return env
}

// GetEnvironmentFileName returns the environment-specific filename for the current environment
func GetEnvironmentFileName() string {
	env := DetectEnvironment()
	return fmt.Sprintf(".env.%s", env)
}

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Value   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("configuration validation failed for %s=%s: %s", e.Field, e.Value, e.Message)
}

// ConfigLoader provides unified configuration loading with priority handling
type ConfigLoader struct {
	envVars map[string]string
	logger  *zap.Logger
}

// NewConfigLoader creates a new configuration loader
func NewConfigLoader() *ConfigLoader {
	return &ConfigLoader{
		envVars: make(map[string]string),
	}
}

// WithLogger sets the logger for the config loader
func (cl *ConfigLoader) WithLogger(logger *zap.Logger) *ConfigLoader {
	cl.logger = logger
	return cl
}

// LoadEnvFile loads environment variables from .env file (LEGACY - use LoadEnvironmentFile)
func (cl *ConfigLoader) LoadEnvFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		// File doesn't exist, not an error
		if cl.logger != nil {
			cl.logger.Debug("Environment file not found", zap.String("file", filename))
		}
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE format
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			if cl.logger != nil {
				cl.logger.Warn("Invalid line in env file",
					zap.String("file", filename),
					zap.Int("line", lineNum),
					zap.String("content", line))
			}
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		if len(value) >= 2 {
			if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
				(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
				value = value[1 : len(value)-1]
			}
		}

		cl.envVars[key] = value
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading env file %s: %w", filename, err)
	}

	if cl.logger != nil {
		cl.logger.Debug("Loaded environment file",
			zap.String("file", filename),
			zap.Int("variables", len(cl.envVars)))
	}

	return nil
}

// LoadEnvironmentFile loads environment variables from environment-specific file
// Uses strict validation with no fallback - panics if environment file is missing
func (cl *ConfigLoader) LoadEnvironmentFile() error {
	env := DetectEnvironment() // Panics on invalid environment
	filename := GetEnvironmentFileName()

	// Strict loading - no fallback, panic if file doesn't exist
	file, err := os.Open(filename)
	if err != nil {
		panic(fmt.Sprintf("Required environment file '%s' not found for environment '%s'. "+
			"Create this file from env.sample or set MINEXUS_ENV to a valid environment (prod, test)",
			filename, env))
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE format
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			if cl.logger != nil {
				cl.logger.Warn("Invalid line in env file",
					zap.String("file", filename),
					zap.Int("line", lineNum),
					zap.String("content", line))
			}
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		if len(value) >= 2 {
			if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
				(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
				value = value[1 : len(value)-1]
			}
		}

		cl.envVars[key] = value
	}

	if err := scanner.Err(); err != nil {
		panic(fmt.Sprintf("Error reading environment file '%s': %v", filename, err))
	}

	if cl.logger != nil {
		cl.logger.Info("Loaded environment file",
			zap.String("file", filename),
			zap.String("environment", env),
			zap.Int("variables", len(cl.envVars)))
	}

	return nil
}

// GetString gets string value with priority: flags → env → file → default
func (cl *ConfigLoader) GetString(key, defaultValue string) string {
	// Check environment variables first (highest priority after flags)
	if value := os.Getenv(key); value != "" {
		return value
	}

	// Check .env file variables
	if value, exists := cl.envVars[key]; exists {
		return value
	}

	// Return default
	return defaultValue
}

// GetInt gets int value with validation
func (cl *ConfigLoader) GetInt(key string, defaultValue int) (int, error) {
	value := cl.GetString(key, "")
	if value == "" {
		return defaultValue, nil
	}

	intVal, err := strconv.Atoi(value)
	if err != nil {
		return 0, ValidationError{
			Field:   key,
			Value:   value,
			Message: "must be a valid integer",
		}
	}

	return intVal, nil
}

// GetIntInRange gets int value with range validation
func (cl *ConfigLoader) GetIntInRange(key string, defaultValue, min, max int) (int, error) {
	value, err := cl.GetInt(key, defaultValue)
	if err != nil {
		return 0, err
	}

	if value < min || value > max {
		return 0, ValidationError{
			Field:   key,
			Value:   strconv.Itoa(value),
			Message: fmt.Sprintf("must be between %d and %d", min, max),
		}
	}

	return value, nil
}

// GetBool gets bool value with validation
func (cl *ConfigLoader) GetBool(key string, defaultValue bool) (bool, error) {
	value := cl.GetString(key, "")
	if value == "" {
		return defaultValue, nil
	}

	boolVal, err := strconv.ParseBool(value)
	if err != nil {
		return false, ValidationError{
			Field:   key,
			Value:   value,
			Message: "must be true/false, 1/0, or yes/no",
		}
	}

	return boolVal, nil
}

// GetDuration gets duration value with validation
func (cl *ConfigLoader) GetDuration(key string, defaultValue time.Duration) (time.Duration, error) {
	value := cl.GetString(key, "")
	if value == "" {
		return defaultValue, nil
	}

	// Try parsing as duration first
	if duration, err := time.ParseDuration(value); err == nil {
		return duration, nil
	}

	// Try parsing as seconds (for backward compatibility)
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}

	return 0, ValidationError{
		Field:   key,
		Value:   value,
		Message: "must be a valid duration (e.g., '10s', '5m') or number of seconds",
	}
}

// ValidateNetworkAddress validates a network address
func (cl *ConfigLoader) ValidateNetworkAddress(key, value string) error {
	if value == "" {
		return ValidationError{
			Field:   key,
			Value:   value,
			Message: "network address cannot be empty",
		}
	}

	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return ValidationError{
			Field:   key,
			Value:   value,
			Message: "must be in format 'host:port'",
		}
	}

	// Validate port range
	if portNum, err := strconv.Atoi(port); err != nil || portNum < 1 || portNum > 65535 {
		return ValidationError{
			Field:   key,
			Value:   value,
			Message: "port must be between 1 and 65535",
		}
	}

	// Validate host (basic check)
	if host == "" {
		return ValidationError{
			Field:   key,
			Value:   value,
			Message: "host cannot be empty",
		}
	}

	return nil
}

// ValidateHostname validates a hostname (without port)
func (cl *ConfigLoader) ValidateHostname(key, value string) error {
	if value == "" {
		return ValidationError{
			Field:   key,
			Value:   value,
			Message: "hostname cannot be empty",
		}
	}

	// Check if it contains a port (which it shouldn't)
	if strings.Contains(value, ":") {
		return ValidationError{
			Field:   key,
			Value:   value,
			Message: "should contain only hostname, not host:port format",
		}
	}

	return nil
}

// ValidateRequired ensures a required field is not empty
func (cl *ConfigLoader) ValidateRequired(key, value string) error {
	if value == "" {
		return ValidationError{
			Field:   key,
			Value:   value,
			Message: "is required and cannot be empty",
		}
	}
	return nil
}

// ValidateDirectory ensures a directory path is valid
func (cl *ConfigLoader) ValidateDirectory(key, value string) error {
	if value == "" {
		return ValidationError{
			Field:   key,
			Value:   value,
			Message: "directory path cannot be empty",
		}
	}

	info, err := os.Stat(value)
	if err != nil {
		if os.IsNotExist(err) {
			return ValidationError{
				Field:   key,
				Value:   value,
				Message: "directory does not exist",
			}
		}
		return ValidationError{
			Field:   key,
			Value:   value,
			Message: fmt.Sprintf("cannot access directory: %v", err),
		}
	}

	if !info.IsDir() {
		return ValidationError{
			Field:   key,
			Value:   value,
			Message: "path exists but is not a directory",
		}
	}

	return nil
}

// ConsoleConfig holds configuration for the console client
type ConsoleConfig struct {
	ServerAddr     string
	ConnectTimeout int // seconds
	Debug          bool
}

// NexusConfig holds configuration for the Nexus server
type NexusConfig struct {
	MinionPort  int    // Port for minion connections with standard TLS
	ConsolePort int    // Port for console connections with mTLS
	WebPort     int    // Port for HTTP web server
	WebEnabled  bool   // Enable/disable web server
	WebRoot     string // Path to webroot directory (for file system assets)
	DBHost      string
	DBPort      int
	DBUser      string
	DBPassword  string
	DBName      string
	DBSSLMode   string
	Debug       bool
	MaxMsgSize  int
	FileRoot    string
}

// MinionConfig holds configuration for Minion clients
type MinionConfig struct {
	ServerAddr            string
	ID                    string
	Debug                 bool
	ConnectTimeout        int // seconds
	InitialReconnectDelay int // seconds - starting delay for exponential backoff
	MaxReconnectDelay     int // seconds - maximum delay cap for exponential backoff
	HeartbeatInterval     int // seconds
	DefaultShellTimeout   int // seconds - default timeout for shell command execution
	StreamTimeout         int // seconds - timeout for stream operations
}

// DefaultConsoleConfig returns default configuration for Console
func DefaultConsoleConfig() *ConsoleConfig {
	return &ConsoleConfig{
		ServerAddr:     "localhost:11973", // Will be constructed from NEXUS_SERVER + NEXUS_CONSOLE_PORT
		ConnectTimeout: 10,
		Debug:          false,
	}
}

// DefaultNexusConfig returns default configuration for Nexus
func DefaultNexusConfig() *NexusConfig {
	return &NexusConfig{
		MinionPort:  11972,
		ConsolePort: 11973,
		WebPort:     8086,
		WebEnabled:  true,
		WebRoot:     "./webroot",
		DBHost:      "localhost",
		DBPort:      5432,
		DBUser:      "postgres",
		DBPassword:  "postgres",
		DBName:      "minexus",
		DBSSLMode:   "disable",
		Debug:       false,
		MaxMsgSize:  1024 * 1024 * 10, // 10MB
		FileRoot:    "/tmp",
	}
}

// DefaultMinionConfig returns default configuration for Minion
func DefaultMinionConfig() *MinionConfig {
	return &MinionConfig{
		ServerAddr:            "localhost:11972", // Will be constructed from NEXUS_SERVER + NEXUS_MINION_PORT
		ID:                    "",                // Will be auto-generated if empty
		Debug:                 false,
		ConnectTimeout:        3,
		InitialReconnectDelay: 1,   // 1 second initial delay
		MaxReconnectDelay:     300, // 5 minutes maximum delay
		HeartbeatInterval:     30,
		DefaultShellTimeout:   15, // 15 seconds default shell timeout
		StreamTimeout:         30, // 30 seconds stream timeout (reduced from 90s hardcoded)
	}
}

// LoadConsoleConfig loads console configuration with validation
func LoadConsoleConfig() (*ConsoleConfig, error) {
	loader := NewConfigLoader()
	if err := loader.LoadEnvironmentFile(); err != nil {
		return nil, fmt.Errorf("failed to load environment file: %w", err)
	}

	config := DefaultConsoleConfig()
	var validationErrors []error

	// Load and validate server hostname
	nexusServer := loader.GetString("NEXUS_SERVER", "localhost")
	if err := loader.ValidateHostname("NEXUS_SERVER", nexusServer); err != nil {
		validationErrors = append(validationErrors, err)
	}

	// Load and validate console port
	consolePort, err := loader.GetIntInRange("NEXUS_CONSOLE_PORT", 11973, 1, 65535)
	if err != nil {
		validationErrors = append(validationErrors, err)
	}

	// Construct server address from hostname and port
	config.ServerAddr = fmt.Sprintf("%s:%d", nexusServer, consolePort)

	// Load and validate connect timeout
	if timeout, err := loader.GetIntInRange("CONNECT_TIMEOUT", config.ConnectTimeout, 1, 300); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.ConnectTimeout = timeout
	}

	// Load debug flag
	if debug, err := loader.GetBool("DEBUG", config.Debug); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.Debug = debug
	}

	// Handle manual flag parsing for console (to avoid conflicts with other flag parsers)
	if len(os.Args) > 1 {
		for i, arg := range os.Args[1:] {
			switch arg {
			case "-server", "--server":
				if i+1 < len(os.Args)-1 {
					addr := os.Args[i+2]
					// For backward compatibility, still accept host:port format in command line flags
					if err := loader.ValidateNetworkAddress("server", addr); err != nil {
						validationErrors = append(validationErrors, err)
					} else {
						config.ServerAddr = addr
					}
				}
			case "-debug", "--debug":
				config.Debug = true
			case "-timeout", "--timeout":
				if i+1 < len(os.Args)-1 {
					if t, err := strconv.Atoi(os.Args[i+2]); err == nil {
						if t < 1 || t > 300 {
							validationErrors = append(validationErrors, ValidationError{
								Field:   "timeout",
								Value:   strconv.Itoa(t),
								Message: "must be between 1 and 300 seconds",
							})
						} else {
							config.ConnectTimeout = t
						}
					} else {
						validationErrors = append(validationErrors, ValidationError{
							Field:   "timeout",
							Value:   os.Args[i+2],
							Message: "must be a valid integer",
						})
					}
				}
			}
		}
	}

	// Return validation errors if any
	if len(validationErrors) > 0 {
		var errMsg strings.Builder
		errMsg.WriteString("Configuration validation failed:\n")
		for _, err := range validationErrors {
			errMsg.WriteString(fmt.Sprintf("  - %s\n", err.Error()))
		}
		return nil, fmt.Errorf("%s", errMsg.String())
	}

	return config, nil
}

// LoadNexusConfig loads Nexus configuration with validation
func LoadNexusConfig() (*NexusConfig, error) {
	// Create a simple logger for configuration loading diagnostics
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	logger, start := logging.FuncLogger(logger, "LoadNexusConfig")
	defer logging.FuncExit(logger, start)

	loader := NewConfigLoader().WithLogger(logger)
	if err := loader.LoadEnvironmentFile(); err != nil {
		return nil, fmt.Errorf("failed to load environment file: %w", err)
	}

	config := DefaultNexusConfig()
	var validationErrors []error

	// Load and validate port (allow 0 for system-assigned port)
	if port, err := loader.GetIntInRange("NEXUS_MINION_PORT", config.MinionPort, 0, 65535); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.MinionPort = port
	}

	// Load and validate console port
	if consolePort, err := loader.GetIntInRange("NEXUS_CONSOLE_PORT", config.ConsolePort, 0, 65535); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.ConsolePort = consolePort
	}

	// Load and validate web port
	if webPort, err := loader.GetIntInRange("NEXUS_WEB_PORT", config.WebPort, 0, 65535); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.WebPort = webPort
	}

	// Load web enabled flag
	if webEnabled, err := loader.GetBool("NEXUS_WEB_ENABLED", config.WebEnabled); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.WebEnabled = webEnabled
	}

	// Load web root directory
	config.WebRoot = loader.GetString("NEXUS_WEB_ROOT", config.WebRoot)

	// Load database configuration
	config.DBHost = loader.GetString("DBHOST", config.DBHost)
	if err := loader.ValidateRequired("DBHOST", config.DBHost); err != nil {
		validationErrors = append(validationErrors, err)
	}

	if dbPort, err := loader.GetIntInRange("DBPORT", config.DBPort, 1, 65535); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.DBPort = dbPort
	}

	config.DBUser = loader.GetString("DBUSER", config.DBUser)
	config.DBPassword = loader.GetString("DBPASS", config.DBPassword)
	config.DBName = loader.GetString("DBNAME", config.DBName)
	config.DBSSLMode = loader.GetString("DBSSLMODE", config.DBSSLMode)

	// Load debug flag
	if debug, err := loader.GetBool("DEBUG", config.Debug); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.Debug = debug
	}

	// Load and validate max message size
	if maxMsgSize, err := loader.GetIntInRange("MAX_MSG_SIZE", config.MaxMsgSize, 1024, 1024*1024*100); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.MaxMsgSize = maxMsgSize
	}

	// Load and validate file root
	config.FileRoot = loader.GetString("FILEROOT", config.FileRoot)

	// Parse command line flags (highest priority)
	minionPort := flag.Int("minion-port", config.MinionPort, "Port to listen on for minion connections")
	consolePort := flag.Int("console-port", config.ConsolePort, "Console port for mTLS connections")
	webPort := flag.Int("web-port", config.WebPort, "Port for HTTP web server")
	webEnabled := flag.Bool("web-enabled", config.WebEnabled, "Enable/disable web server")
	webRoot := flag.String("web-root", config.WebRoot, "Path to webroot directory")
	dbHost := flag.String("db-host", config.DBHost, "Database host")
	dbPort := flag.Int("db-port", config.DBPort, "Database port")
	dbUser := flag.String("db-user", config.DBUser, "Database user")
	dbPassword := flag.String("db-password", config.DBPassword, "Database password")
	dbName := flag.String("db-name", config.DBName, "Database name")
	dbSSLMode := flag.String("db-sslmode", config.DBSSLMode, "Database SSL mode")
	debug := flag.Bool("debug", config.Debug, "Enable debug mode")
	maxMsgSize := flag.Int("max-msg-size", config.MaxMsgSize, "Maximum message size in bytes")
	fileRoot := flag.String("file-root", config.FileRoot, "File root directory")

	flag.Parse()

	// Apply and validate command line flags
	if *minionPort < 0 || *minionPort > 65535 {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "minion-port",
			Value:   strconv.Itoa(*minionPort),
			Message: "must be between 0 and 65535 (0 for system-assigned)",
		})
	} else {
		config.MinionPort = *minionPort
	}

	if *consolePort < 0 || *consolePort > 65535 {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "console-port",
			Value:   strconv.Itoa(*consolePort),
			Message: "must be between 0 and 65535 (0 for system-assigned)",
		})
	} else {
		config.ConsolePort = *consolePort
	}

	// Apply and validate web port
	if *webPort < 0 || *webPort > 65535 {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "web-port",
			Value:   strconv.Itoa(*webPort),
			Message: "must be between 0 and 65535 (0 for system-assigned)",
		})
	} else {
		config.WebPort = *webPort
	}

	// Apply web enabled flag and web root
	config.WebEnabled = *webEnabled
	config.WebRoot = *webRoot

	config.DBHost = *dbHost
	config.DBPort = *dbPort
	config.DBUser = *dbUser
	config.DBPassword = *dbPassword
	config.DBName = *dbName
	config.DBSSLMode = *dbSSLMode
	config.Debug = *debug

	if *maxMsgSize < 1024 || *maxMsgSize > 1024*1024*100 {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "max-msg-size",
			Value:   strconv.Itoa(*maxMsgSize),
			Message: "must be between 1KB and 100MB",
		})
	} else {
		config.MaxMsgSize = *maxMsgSize
	}

	config.FileRoot = *fileRoot

	// Return validation errors if any
	if len(validationErrors) > 0 {
		var errMsg strings.Builder
		errMsg.WriteString("Configuration validation failed:\n")
		for _, err := range validationErrors {
			errMsg.WriteString(fmt.Sprintf("  - %s\n", err.Error()))
		}
		return nil, fmt.Errorf("%s", errMsg.String())
	}

	return config, nil
}

// LoadMinionConfig loads Minion configuration with validation
func LoadMinionConfig() (*MinionConfig, error) {
	loader := NewConfigLoader()
	if err := loader.LoadEnvironmentFile(); err != nil {
		return nil, fmt.Errorf("failed to load environment file: %w", err)
	}

	config := DefaultMinionConfig()
	var validationErrors []error

	// Load configuration from environment variables
	loadMinionEnvConfig(loader, config, &validationErrors)

	// Parse and apply command line flags
	flagValues := parseMinionFlags(config)
	flag.Parse()
	applyMinionFlags(loader, config, flagValues, &validationErrors)

	// Perform final validations
	validateMinionConfigConsistency(config, &validationErrors)

	return finalizeMinionConfig(config, validationErrors)
}

// loadMinionEnvConfig loads configuration from environment variables
func loadMinionEnvConfig(loader *ConfigLoader, config *MinionConfig, validationErrors *[]error) {
	// Load and validate server hostname
	nexusServer := loader.GetString("NEXUS_SERVER", "localhost")
	if err := loader.ValidateHostname("NEXUS_SERVER", nexusServer); err != nil {
		*validationErrors = append(*validationErrors, err)
	}

	// Load and validate nexus port
	nexusPort, err := loader.GetIntInRange("NEXUS_MINION_PORT", 11972, 1, 65535)
	if err != nil {
		*validationErrors = append(*validationErrors, err)
	}

	// Construct server address from hostname and port
	config.ServerAddr = fmt.Sprintf("%s:%d", nexusServer, nexusPort)

	// Load minion ID (optional)
	config.ID = loader.GetString("MINION_ID", config.ID)

	// Load debug flag
	if debug, err := loader.GetBool("DEBUG", config.Debug); err != nil {
		*validationErrors = append(*validationErrors, err)
	} else {
		config.Debug = debug
	}

	// Load timeout configurations
	loadMinionTimeouts(loader, config, validationErrors)
}

// loadMinionTimeouts loads timeout-related configuration from environment variables
func loadMinionTimeouts(loader *ConfigLoader, config *MinionConfig, validationErrors *[]error) {
	timeoutConfigs := []struct {
		envVar   string
		target   *int
		min, max int
	}{
		{"CONNECT_TIMEOUT", &config.ConnectTimeout, 1, 300},
		{"INITIAL_RECONNECT_DELAY", &config.InitialReconnectDelay, 1, 3600},
		{"MAX_RECONNECT_DELAY", &config.MaxReconnectDelay, 1, 3600},
		{"HEARTBEAT_INTERVAL", &config.HeartbeatInterval, 5, 300},
		{"DEFAULT_SHELL_TIMEOUT", &config.DefaultShellTimeout, 5, 300},
		{"STREAM_TIMEOUT", &config.StreamTimeout, 10, 300},
	}

	for _, tc := range timeoutConfigs {
		if value, err := loader.GetIntInRange(tc.envVar, *tc.target, tc.min, tc.max); err != nil {
			*validationErrors = append(*validationErrors, err)
		} else {
			*tc.target = value
		}
	}
}

// minionFlagValues holds the parsed command line flag values
type minionFlagValues struct {
	serverAddr            *string
	id                    *string
	debug                 *bool
	connectTimeout        *int
	initialReconnectDelay *int
	maxReconnectDelay     *int
	heartbeatInterval     *int
	defaultShellTimeout   *int
	streamTimeout         *int
}

// parseMinionFlags parses command line flags and returns the flag pointers
func parseMinionFlags(config *MinionConfig) *minionFlagValues {
	return &minionFlagValues{
		serverAddr:            flag.String("server", config.ServerAddr, "Nexus server address"),
		id:                    flag.String("id", config.ID, "Minion ID (optional, will be generated if not provided)"),
		debug:                 flag.Bool("debug", config.Debug, "Enable debug mode"),
		connectTimeout:        flag.Int("connect-timeout", config.ConnectTimeout, "Connection timeout in seconds"),
		initialReconnectDelay: flag.Int("initial-reconnect-delay", config.InitialReconnectDelay, "Initial reconnection delay in seconds (exponential backoff starting point)"),
		maxReconnectDelay:     flag.Int("max-reconnect-delay", config.MaxReconnectDelay, "Maximum reconnection delay in seconds (exponential backoff cap)"),
		heartbeatInterval:     flag.Int("heartbeat-interval", config.HeartbeatInterval, "Heartbeat interval in seconds"),
		defaultShellTimeout:   flag.Int("default-shell-timeout", config.DefaultShellTimeout, "Default timeout for shell command execution in seconds"),
		streamTimeout:         flag.Int("stream-timeout", config.StreamTimeout, "Timeout for stream operations in seconds"),
	}
}

// applyMinionFlags applies command line flag values to the configuration
func applyMinionFlags(loader *ConfigLoader, config *MinionConfig, flags *minionFlagValues, validationErrors *[]error) {
	// Apply and validate server address
	if err := loader.ValidateNetworkAddress("server", *flags.serverAddr); err != nil {
		*validationErrors = append(*validationErrors, err)
	} else {
		config.ServerAddr = *flags.serverAddr
	}

	// Apply simple flags
	config.ID = *flags.id
	config.Debug = *flags.debug

	// Apply and validate timeout flags
	applyMinionTimeoutFlags(config, flags, validationErrors)
}

// applyMinionTimeoutFlags applies and validates timeout-related command line flags
func applyMinionTimeoutFlags(config *MinionConfig, flags *minionFlagValues, validationErrors *[]error) {
	timeoutValidations := []struct {
		name     string
		value    int
		target   *int
		min, max int
	}{
		{"connect-timeout", *flags.connectTimeout, &config.ConnectTimeout, 1, 300},
		{"initial-reconnect-delay", *flags.initialReconnectDelay, &config.InitialReconnectDelay, 1, 3600},
		{"max-reconnect-delay", *flags.maxReconnectDelay, &config.MaxReconnectDelay, 1, 3600},
		{"heartbeat-interval", *flags.heartbeatInterval, &config.HeartbeatInterval, 5, 300},
		{"default-shell-timeout", *flags.defaultShellTimeout, &config.DefaultShellTimeout, 5, 300},
		{"stream-timeout", *flags.streamTimeout, &config.StreamTimeout, 10, 300},
	}

	for _, tv := range timeoutValidations {
		if tv.value < tv.min || tv.value > tv.max {
			*validationErrors = append(*validationErrors, ValidationError{
				Field:   tv.name,
				Value:   strconv.Itoa(tv.value),
				Message: fmt.Sprintf("must be between %d and %d seconds", tv.min, tv.max),
			})
		} else {
			*tv.target = tv.value
		}
	}
}

// validateMinionConfigConsistency performs consistency validations
func validateMinionConfigConsistency(config *MinionConfig, validationErrors *[]error) {
	if config.InitialReconnectDelay > config.MaxReconnectDelay {
		*validationErrors = append(*validationErrors, ValidationError{
			Field:   "reconnect-delays",
			Value:   fmt.Sprintf("initial=%d, max=%d", config.InitialReconnectDelay, config.MaxReconnectDelay),
			Message: "initial reconnect delay cannot be greater than max reconnect delay",
		})
	}
}

// finalizeMinionConfig finalizes the configuration and returns errors if any
func finalizeMinionConfig(config *MinionConfig, validationErrors []error) (*MinionConfig, error) {
	if len(validationErrors) > 0 {
		var errMsg strings.Builder
		errMsg.WriteString("Configuration validation failed:\n")
		for _, err := range validationErrors {
			errMsg.WriteString(fmt.Sprintf("  - %s\n", err.Error()))
		}
		return nil, fmt.Errorf("%s", errMsg.String())
	}

	return config, nil
}

// DBConnectionString builds a PostgreSQL connection string from config
func (c *NexusConfig) DBConnectionString() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

// LogConfig logs the configuration (masks sensitive data)
func (c *NexusConfig) LogConfig(logger *zap.Logger) {
	logger.Info("Configuration loaded",
		zap.Int("minion_port", c.MinionPort),
		zap.Int("console_port", c.ConsolePort),
		zap.Int("web_port", c.WebPort),
		zap.Bool("web_enabled", c.WebEnabled),
		zap.String("web_root", c.WebRoot),
		zap.String("db_host", c.DBHost),
		zap.Int("db_port", c.DBPort),
		zap.String("db_name", c.DBName),
		zap.String("db_user", c.DBUser),
		zap.Bool("debug", c.Debug),
		zap.Int("max_msg_size", c.MaxMsgSize),
		zap.String("file_root", c.FileRoot))
}

// LogConfig logs the minion configuration
func (c *MinionConfig) LogConfig(logger *zap.Logger) {
	logger.Info("Configuration loaded",
		zap.String("server", c.ServerAddr),
		zap.String("id", c.ID),
		zap.Bool("debug", c.Debug),
		zap.Int("connect_timeout", c.ConnectTimeout),
		zap.Int("initial_reconnect_delay", c.InitialReconnectDelay),
		zap.Int("max_reconnect_delay", c.MaxReconnectDelay),
		zap.Int("heartbeat_interval", c.HeartbeatInterval),
		zap.Int("default_shell_timeout", c.DefaultShellTimeout),
		zap.Int("stream_timeout", c.StreamTimeout))
}

// LogConfig logs the console configuration
func (c *ConsoleConfig) LogConfig(logger *zap.Logger) {
	logger.Info("Configuration loaded",
		zap.String("server", c.ServerAddr),
		zap.Int("connect_timeout", c.ConnectTimeout),
		zap.Bool("debug", c.Debug))
}
