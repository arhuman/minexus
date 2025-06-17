# Configuration System

Minexus uses a unified configuration system with priority-based loading and comprehensive validation. This system provides a single source of truth for configuration across all components.

## Configuration Priority

The configuration system follows a strict priority order:

1. **Command Line Flags** (highest priority)
2. **Environment Variables**
3. **Configuration Files** (`.env`)
4. **Default Values** (lowest priority)

## Dual-Port Architecture

Minexus uses a dual-port architecture for enhanced security:

- **Port 11972** (`NEXUS_PORT`) - Standard TLS for minion connections
- **Port 11973** (`NEXUS_CONSOLE_PORT`) - Mutual TLS (mTLS) for console connections

This separation ensures that:
- Minions use standard TLS authentication (server-only certificates)
- Console connections use mutual TLS authentication (both client and server certificates)
- Different security policies can be applied to each type of connection

## Configuration Architecture

The unified configuration system is built around the `ConfigLoader` architecture:

```
ConfigLoader
├── LoadEnvFile() - Load variables from .env file
├── GetString() - Get string values with priority handling
├── GetInt() - Get integer values with validation
├── GetBool() - Get boolean values with validation
├── GetDuration() - Get duration values with validation
└── Validation Methods
    ├── ValidateNetworkAddress()
    ├── ValidateRequired()
    └── ValidateDirectory()
```

## Component Configurations

### Console Configuration

**Configuration Structure:**
```go
type ConsoleConfig struct {
    ServerAddr     string // Nexus server address (host:port)
    ConnectTimeout int    // Connection timeout in seconds
    Debug          bool   // Enable debug logging
}
```

**Environment Variables:**
- `NEXUS_SERVER` - Nexus server hostname (default: "localhost")
- `NEXUS_CONSOLE_PORT` - Console server port for mTLS (default: 11973, range: 1-65535)
- `CONNECT_TIMEOUT` - Connection timeout in seconds (default: 3, range: 1-300)
- `DEBUG` - Enable debug mode (default: false)

**Command Line Flags:**
- `-server`, `--server` - Nexus server address
- `-debug`, `--debug` - Enable debug mode
- `-timeout`, `--timeout` - Connection timeout in seconds

**Usage Example:**
```bash
# Using environment variables
export NEXUS_SERVER=nexus.example.com
export NEXUS_CONSOLE_PORT=11973
export DEBUG=true
./console

# Using command line flags (backward compatible with host:port format)
./console --server nexus.example.com:11973 --debug
```

### Nexus Configuration

**Configuration Structure:**
```go
type NexusConfig struct {
    Port               int    // Server listening port
    DBHost             string // Database host
    DBPort             int    // Database port
    DBUser             string // Database username
    DBPassword         string // Database password
    DBName             string // Database name
    DBSSLMode          string // Database SSL mode
    Debug              bool   // Enable debug logging
    MaxMsgSize         int    // Maximum message size in bytes
    FileRoot           string // File root directory
    LegacyDBConnString string // Legacy database connection string
}
```

**Environment Variables:**
- `NEXUS_PORT` - Minion server port (default: 11972, range: 1-65535)
- `NEXUS_CONSOLE_PORT` - Console server port with mTLS (default: 11973, range: 1-65535)
- `DBHOST` - Database host (default: "localhost")
- `DBPORT` - Database port (default: 5432, range: 1-65535)
- `DBUSER` - Database user (default: "postgres")
- `DBPASS` - Database password (default: "postgres")
- `DBNAME` - Database name (default: "minexus")
- `DBSSLMODE` - Database SSL mode (default: "disable")
- `DEBUG` - Enable debug mode (default: false)
- `MAX_MSG_SIZE` - Maximum message size (default: 10MB, range: 1KB-100MB)
- `FILEROOT` - File root directory (default: "/tmp")

**Command Line Flags:**
- `-port` - Minion server listening port
- `-console-port` - Console server listening port (mTLS)
- `-db-host` - Database host
- `-db-port` - Database port
- `-db-user` - Database username
- `-db-password` - Database password
- `-db-name` - Database name
- `-db-sslmode` - Database SSL mode
- `-debug` - Enable debug mode
- `-max-msg-size` - Maximum message size in bytes
- `-file-root` - File root directory
- `-db` - Legacy database connection string (overrides individual DB settings)

### Minion Configuration

**Configuration Structure:**
```go
type MinionConfig struct {
    ServerAddr            string // Nexus server address
    ID                    string // Minion ID (optional, auto-generated if empty)
    Debug                 bool   // Enable debug logging
    ConnectTimeout        int    // Connection timeout in seconds
    InitialReconnectDelay int    // Initial reconnection delay (exponential backoff start)
    MaxReconnectDelay     int    // Maximum reconnection delay (exponential backoff cap)
    HeartbeatInterval     int    // Heartbeat interval in seconds
}
```

**Environment Variables:**
- `NEXUS_SERVER` - Nexus server hostname (default: "localhost")
- `NEXUS_PORT` - Nexus server port for minions (default: 11972, range: 1-65535)
- `MINION_ID` - Minion ID (optional, auto-generated if empty)
- `DEBUG` - Enable debug mode (default: false)
- `CONNECT_TIMEOUT` - Connection timeout (default: 3, range: 1-300)
- `INITIAL_RECONNECT_DELAY` - Initial reconnection delay (default: 1, range: 1-3600)
- `MAX_RECONNECT_DELAY` - Maximum reconnection delay (default: 3600, range: 1-86400)
- `HEARTBEAT_INTERVAL` - Heartbeat interval (default: 60, range: 5-300)
- `RECONNECT_DELAY` - Legacy reconnection delay (deprecated, sets both initial and max)

**Command Line Flags:**
- `-server` - Nexus server address (backward compatible with host:port format)
- `-id` - Minion ID
- `-debug` - Enable debug mode
- `-connect-timeout` - Connection timeout in seconds
- `-initial-reconnect-delay` - Initial reconnection delay
- `-max-reconnect-delay` - Maximum reconnection delay
- `-heartbeat-interval` - Heartbeat interval
- `-reconnect-delay` - Legacy reconnection delay (deprecated)

## Configuration File Format

The `.env` file supports standard environment variable format:

```bash
# Nexus server configuration
NEXUS_SERVER=nexus.example.com
NEXUS_PORT=11972
NEXUS_CONSOLE_PORT=11973
DBHOST=database.example.com
DBPORT=5432
DBUSER=minexus_user
DBPASS=secure_password
DBNAME=minexus_prod
DBSSLMODE=require

# Minion configuration
MINION_ID=worker-01
CONNECT_TIMEOUT=3
INITIAL_RECONNECT_DELAY=5
MAX_RECONNECT_DELAY=3600
HEARTBEAT_INTERVAL=60

# Global settings
DEBUG=false
```

**File Format Rules:**
- Lines starting with `#` are comments
- Empty lines are ignored
- Format: `KEY=VALUE`
- Values can be quoted with single or double quotes
- Quotes are automatically stripped

## Validation Rules

The configuration system includes comprehensive validation:

### Network Addresses
- NEXUS_SERVER must contain only hostname (no port)
- Ports must be between 1 and 65535
- Hostname cannot be empty
- Command line flags still accept host:port format for backward compatibility

### Integer Ranges
- **Ports**: 1-65535
- **Timeouts**: 1-300 seconds
- **Reconnect delays**: 1-3600 seconds (initial), 1-86400 seconds (max)
- **Heartbeat intervals**: 5-300 seconds
- **Message sizes**: 1KB-100MB

### Required Fields
- Database host must not be empty
- Network addresses must be valid

### Directory Validation
- Directory paths must exist and be accessible
- Must be actual directories (not files)

## Error Handling

Configuration errors are reported with detailed messages:

```
Configuration validation failed:
  - configuration validation failed for NEXUS_PORT=70000: must be between 1 and 65535
  - configuration validation failed for CONNECT_TIMEOUT=invalid: must be a valid integer
  - configuration validation failed for NEXUS_SERVER=invalid-address: must be in format 'host:port'
```

## Usage Examples

### Basic Usage

```go
// Load Nexus configuration
cfg, err := config.LoadNexusConfig()
if err != nil {
    log.Fatalf("Configuration error: %v", err)
}

// Log configuration (sensitive data is masked)
cfg.LogConfig(logger)
```

### Custom Configuration Loading

```go
// Create a custom loader
loader := config.NewConfigLoader().WithLogger(logger)

// Load environment file
if err := loader.LoadEnvFile(".env.production"); err != nil {
    log.Fatalf("Failed to load env file: %v", err)
}

// Get values with validation
port, err := loader.GetIntInRange("CUSTOM_PORT", 8080, 1, 65535)
if err != nil {
    log.Fatalf("Invalid port: %v", err)
}
```

### Environment-Specific Configuration

```bash
# Development
export DEBUG=true
export NEXUS_SERVER=localhost
export NEXUS_PORT=11972
export NEXUS_CONSOLE_PORT=11973
export DBHOST=localhost

# Production
export DEBUG=false
export NEXUS_SERVER=prod-nexus.example.com
export NEXUS_PORT=443
export NEXUS_CONSOLE_PORT=8443
export DBHOST=prod-db.example.com
export DBSSLMODE=require
```

## Migration from Legacy System

The unified system maintains backward compatibility:

### Deprecated Functions
- `LoadEnvFile()` - Use `ConfigLoader.LoadEnvFile()`
- `getEnvString()` - Use `ConfigLoader.GetString()`
- `getEnvInt()` - Use `ConfigLoader.GetInt()`
- `getEnvBool()` - Use `ConfigLoader.GetBool()`

### Legacy Support
- All existing environment variables continue to work
- Command line flags remain unchanged
- `.env` file format is compatible
- Legacy database connection string is supported

## Best Practices

### Security
- Never commit `.env` files with sensitive data
- Use environment variables for production secrets
- Validate all configuration values before use

### Development
- Use `.env` files for local development
- Set reasonable defaults for all values
- Provide clear error messages for invalid configuration

### Production
- Use environment variables for configuration
- Validate configuration on startup
- Log configuration values (with sensitive data masked)

### Testing
- Use the unified configuration system in tests
- Create test-specific configuration files
- Validate error handling for invalid configurations

## Troubleshooting

### Common Issues

**"Configuration validation failed"**
- Check that all required values are provided
- Verify value formats and ranges
- Ensure NEXUS_SERVER contains only hostname (no port)
- Ensure ports are within valid ranges (1-65535)

**"Failed to load environment file"**
- Check that the `.env` file exists and is readable
- Verify file format (KEY=VALUE)
- Check for syntax errors in the file

**"Connection failed"**
- Verify network addresses are correct and reachable
- Check firewall settings
- Validate port numbers and availability

### Debug Mode
Enable debug mode to see detailed configuration loading:

```bash
export DEBUG=true
./nexus
```

This will show:
- Which configuration sources are used
- Validation results
- Final configuration values (with sensitive data masked)
