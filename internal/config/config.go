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
)

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

// LoadEnvFile loads environment variables from .env file
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
	Port               int
	DBHost             string
	DBPort             int
	DBUser             string
	DBPassword         string
	DBName             string
	DBSSLMode          string
	Debug              bool
	MaxMsgSize         int
	FileRoot           string
	LegacyDBConnString string // For backward compatibility with -db flag
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
}

// DefaultConsoleConfig returns default configuration for Console
func DefaultConsoleConfig() *ConsoleConfig {
	return &ConsoleConfig{
		ServerAddr:     "localhost:11972",
		ConnectTimeout: 10,
		Debug:          false,
	}
}

// DefaultNexusConfig returns default configuration for Nexus
func DefaultNexusConfig() *NexusConfig {
	return &NexusConfig{
		Port:       11972,
		DBHost:     "localhost",
		DBPort:     5432,
		DBUser:     "postgres",
		DBPassword: "postgres",
		DBName:     "minexus",
		DBSSLMode:  "disable",
		Debug:      false,
		MaxMsgSize: 1024 * 1024 * 10, // 10MB
		FileRoot:   "/tmp",
	}
}

// DefaultMinionConfig returns default configuration for Minion
func DefaultMinionConfig() *MinionConfig {
	return &MinionConfig{
		ServerAddr:            "localhost:11972",
		ID:                    "", // Will be auto-generated if empty
		Debug:                 false,
		ConnectTimeout:        3,
		InitialReconnectDelay: 1,    // 1 second initial delay
		MaxReconnectDelay:     3600, // 1 hour maximum delay
		HeartbeatInterval:     30,
	}
}

// LoadConsoleConfig loads console configuration with validation
func LoadConsoleConfig() (*ConsoleConfig, error) {
	loader := NewConfigLoader()
	if err := loader.LoadEnvFile(".env"); err != nil {
		return nil, fmt.Errorf("failed to load environment file: %w", err)
	}

	config := DefaultConsoleConfig()
	var validationErrors []error

	// Load and validate server address
	config.ServerAddr = loader.GetString("NEXUS_SERVER", config.ServerAddr)
	if err := loader.ValidateNetworkAddress("NEXUS_SERVER", config.ServerAddr); err != nil {
		validationErrors = append(validationErrors, err)
	}

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
		return nil, fmt.Errorf(errMsg.String())
	}

	return config, nil
}

// LoadNexusConfig loads Nexus configuration with validation
func LoadNexusConfig() (*NexusConfig, error) {
	loader := NewConfigLoader()
	if err := loader.LoadEnvFile(".env"); err != nil {
		return nil, fmt.Errorf("failed to load environment file: %w", err)
	}

	config := DefaultNexusConfig()
	var validationErrors []error

	// Load and validate port (allow 0 for system-assigned port)
	if port, err := loader.GetIntInRange("NEXUS_PORT", config.Port, 0, 65535); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.Port = port
	}

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
	port := flag.Int("port", config.Port, "Port to listen on")
	dbHost := flag.String("db-host", config.DBHost, "Database host")
	dbPort := flag.Int("db-port", config.DBPort, "Database port")
	dbUser := flag.String("db-user", config.DBUser, "Database user")
	dbPassword := flag.String("db-password", config.DBPassword, "Database password")
	dbName := flag.String("db-name", config.DBName, "Database name")
	dbSSLMode := flag.String("db-sslmode", config.DBSSLMode, "Database SSL mode")
	debug := flag.Bool("debug", config.Debug, "Enable debug mode")
	maxMsgSize := flag.Int("max-msg-size", config.MaxMsgSize, "Maximum message size in bytes")
	fileRoot := flag.String("file-root", config.FileRoot, "File root directory")

	// Legacy flag for backward compatibility
	dbConnString := flag.String("db", "", "Database connection string (legacy, overrides individual db settings)")

	flag.Parse()

	// Apply and validate command line flags
	if *port < 0 || *port > 65535 {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "port",
			Value:   strconv.Itoa(*port),
			Message: "must be between 0 and 65535 (0 for system-assigned)",
		})
	} else {
		config.Port = *port
	}

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

	// Handle legacy db connection string flag
	if *dbConnString != "" {
		config.LegacyDBConnString = *dbConnString
	}

	// Return validation errors if any
	if len(validationErrors) > 0 {
		var errMsg strings.Builder
		errMsg.WriteString("Configuration validation failed:\n")
		for _, err := range validationErrors {
			errMsg.WriteString(fmt.Sprintf("  - %s\n", err.Error()))
		}
		return nil, fmt.Errorf(errMsg.String())
	}

	return config, nil
}

// LoadMinionConfig loads Minion configuration with validation
func LoadMinionConfig() (*MinionConfig, error) {
	loader := NewConfigLoader()
	if err := loader.LoadEnvFile(".env"); err != nil {
		return nil, fmt.Errorf("failed to load environment file: %w", err)
	}

	config := DefaultMinionConfig()
	var validationErrors []error

	// Load and validate server address
	config.ServerAddr = loader.GetString("NEXUS_SERVER", config.ServerAddr)
	if err := loader.ValidateNetworkAddress("NEXUS_SERVER", config.ServerAddr); err != nil {
		validationErrors = append(validationErrors, err)
	}

	// Load minion ID (optional)
	config.ID = loader.GetString("MINION_ID", config.ID)

	// Load debug flag
	if debug, err := loader.GetBool("DEBUG", config.Debug); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.Debug = debug
	}

	// Load and validate timeouts
	if connectTimeout, err := loader.GetIntInRange("CONNECT_TIMEOUT", config.ConnectTimeout, 1, 300); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.ConnectTimeout = connectTimeout
	}

	if initialDelay, err := loader.GetIntInRange("INITIAL_RECONNECT_DELAY", config.InitialReconnectDelay, 1, 3600); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.InitialReconnectDelay = initialDelay
	}

	if maxDelay, err := loader.GetIntInRange("MAX_RECONNECT_DELAY", config.MaxReconnectDelay, 1, 86400); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.MaxReconnectDelay = maxDelay
	}

	if heartbeat, err := loader.GetIntInRange("HEARTBEAT_INTERVAL", config.HeartbeatInterval, 5, 300); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.HeartbeatInterval = heartbeat
	}

	// Maintain backward compatibility with RECONNECT_DELAY
	if reconnectDelay, err := loader.GetInt("RECONNECT_DELAY", -1); err == nil && reconnectDelay != -1 {
		if reconnectDelay < 1 || reconnectDelay > 3600 {
			validationErrors = append(validationErrors, ValidationError{
				Field:   "RECONNECT_DELAY",
				Value:   strconv.Itoa(reconnectDelay),
				Message: "must be between 1 and 3600 seconds",
			})
		} else {
			config.InitialReconnectDelay = reconnectDelay
			config.MaxReconnectDelay = reconnectDelay
		}
	} else if err != nil {
		validationErrors = append(validationErrors, err)
	}

	// Parse command line flags (highest priority)
	serverAddr := flag.String("server", config.ServerAddr, "Nexus server address")
	id := flag.String("id", config.ID, "Minion ID (optional, will be generated if not provided)")
	debug := flag.Bool("debug", config.Debug, "Enable debug mode")
	connectTimeout := flag.Int("connect-timeout", config.ConnectTimeout, "Connection timeout in seconds")
	initialReconnectDelay := flag.Int("initial-reconnect-delay", config.InitialReconnectDelay, "Initial reconnection delay in seconds (exponential backoff starting point)")
	maxReconnectDelay := flag.Int("max-reconnect-delay", config.MaxReconnectDelay, "Maximum reconnection delay in seconds (exponential backoff cap)")
	heartbeatInterval := flag.Int("heartbeat-interval", config.HeartbeatInterval, "Heartbeat interval in seconds")

	// Maintain backward compatibility with the old reconnect-delay flag
	reconnectDelay := flag.Int("reconnect-delay", -1, "Reconnection delay in seconds (deprecated, use initial-reconnect-delay and max-reconnect-delay)")

	flag.Parse()

	// Apply and validate command line flags
	if err := loader.ValidateNetworkAddress("server", *serverAddr); err != nil {
		validationErrors = append(validationErrors, err)
	} else {
		config.ServerAddr = *serverAddr
	}

	config.ID = *id
	config.Debug = *debug

	if *connectTimeout < 1 || *connectTimeout > 300 {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "connect-timeout",
			Value:   strconv.Itoa(*connectTimeout),
			Message: "must be between 1 and 300 seconds",
		})
	} else {
		config.ConnectTimeout = *connectTimeout
	}

	if *initialReconnectDelay < 1 || *initialReconnectDelay > 3600 {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "initial-reconnect-delay",
			Value:   strconv.Itoa(*initialReconnectDelay),
			Message: "must be between 1 and 3600 seconds",
		})
	} else {
		config.InitialReconnectDelay = *initialReconnectDelay
	}

	if *maxReconnectDelay < 1 || *maxReconnectDelay > 86400 {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "max-reconnect-delay",
			Value:   strconv.Itoa(*maxReconnectDelay),
			Message: "must be between 1 and 86400 seconds",
		})
	} else {
		config.MaxReconnectDelay = *maxReconnectDelay
	}

	if *heartbeatInterval < 5 || *heartbeatInterval > 300 {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "heartbeat-interval",
			Value:   strconv.Itoa(*heartbeatInterval),
			Message: "must be between 5 and 300 seconds",
		})
	} else {
		config.HeartbeatInterval = *heartbeatInterval
	}

	// Handle backward compatibility with old reconnect-delay flag
	if *reconnectDelay != -1 {
		if *reconnectDelay < 1 || *reconnectDelay > 3600 {
			validationErrors = append(validationErrors, ValidationError{
				Field:   "reconnect-delay",
				Value:   strconv.Itoa(*reconnectDelay),
				Message: "must be between 1 and 3600 seconds",
			})
		} else {
			config.InitialReconnectDelay = *reconnectDelay
			config.MaxReconnectDelay = *reconnectDelay
		}
	}

	// Validate that initial delay is not greater than max delay
	if config.InitialReconnectDelay > config.MaxReconnectDelay {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "reconnect-delays",
			Value:   fmt.Sprintf("initial=%d, max=%d", config.InitialReconnectDelay, config.MaxReconnectDelay),
			Message: "initial reconnect delay cannot be greater than max reconnect delay",
		})
	}

	// Return validation errors if any
	if len(validationErrors) > 0 {
		var errMsg strings.Builder
		errMsg.WriteString("Configuration validation failed:\n")
		for _, err := range validationErrors {
			errMsg.WriteString(fmt.Sprintf("  - %s\n", err.Error()))
		}
		return nil, fmt.Errorf(errMsg.String())
	}

	return config, nil
}

// DBConnectionString builds a PostgreSQL connection string from config
func (c *NexusConfig) DBConnectionString() string {
	if c.LegacyDBConnString != "" {
		return c.LegacyDBConnString
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

// LogConfig logs the configuration (masks sensitive data)
func (c *NexusConfig) LogConfig(logger *zap.Logger) {
	logger.Info("Configuration loaded",
		zap.Int("port", c.Port),
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
		zap.Int("heartbeat_interval", c.HeartbeatInterval))
}

// LogConfig logs the console configuration
func (c *ConsoleConfig) LogConfig(logger *zap.Logger) {
	logger.Info("Configuration loaded",
		zap.String("server", c.ServerAddr),
		zap.Int("connect_timeout", c.ConnectTimeout),
		zap.Bool("debug", c.Debug))
}
