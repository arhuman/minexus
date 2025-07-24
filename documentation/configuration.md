# Configuration System

Minexus uses a unified configuration system with priority-based loading and comprehensive validation. This system provides a single source of truth for configuration across all components.

## Configuration Priority

The configuration system follows a strict priority order:

1. **Command Line Flags** (highest priority)
2. **Environment Variables**
3. **Environment-Specific Configuration Files** (`.env.prod`, `.env.test`)
4. **Default Values** (lowest priority)

## Environment Detection

Minexus uses the `MINEXUS_ENV` environment variable to determine which configuration file to load:

- `MINEXUS_ENV=test` (default) → loads `.env.test`
- `MINEXUS_ENV=prod` → loads `.env.prod`

**Important:** The system will panic if:
- `MINEXUS_ENV` contains an invalid value (only `prod`, `test` are allowed)
- The required environment-specific configuration file is missing

## Dual-Port Architecture

Minexus uses a dual-port architecture for enhanced security:

- **Port 11972** (`NEXUS_MINION_PORT`) - Standard TLS for minion connections
- **Port 11973** (`NEXUS_CONSOLE_PORT`) - Mutual TLS (mTLS) for console connections

This separation ensures that:
- Minions use standard TLS authentication (server-only certificates)
- Console connections use mutual TLS authentication (both client and server certificates)
- Different security policies can be applied to each type of connection

## Configuration Architecture

The unified configuration system is built around the `ConfigLoader` architecture:

```
ConfigLoader
├── LoadEnvironmentFile() - Load variables from environment-specific file
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
- `NEXUS_MINION_PORT` - Minion server port (default: 11972, range: 1-65535)
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
- `-minion-port` - Minion server listening port
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
- `NEXUS_MINION_PORT` - Nexus server port for minions (default: 11972, range: 1-65535)
- `MINION_ID` - Minion ID (optional, auto-generated if empty)
- `DEBUG` - Enable debug mode (default: false)
- `CONNECT_TIMEOUT` - Connection timeout (default: 3, range: 1-300)
- `INITIAL_RECONNECT_DELAY` - Initial reconnection delay (default: 1, range: 1-3600)
- `MAX_RECONNECT_DELAY` - Maximum reconnection delay (default: 3600, range: 1-86400)
- `HEARTBEAT_INTERVAL` - Heartbeat interval (default: 60, range: 5-300)

**Command Line Flags:**
- `-server` - Nexus server address (backward compatible with host:port format)
- `-id` - Minion ID
- `-debug` - Enable debug mode
- `-connect-timeout` - Connection timeout in seconds
- `-initial-reconnect-delay` - Initial reconnection delay
- `-max-reconnect-delay` - Maximum reconnection delay
- `-heartbeat-interval` - Heartbeat interval

## Configuration File Format

Environment-specific configuration files support standard environment variable format:

**Production environment (`.env.prod`):**
```bash
# Nexus server configuration
NEXUS_SERVER=nexus.example.com
NEXUS_MINION_PORT=11972
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
  - configuration validation failed for NEXUS_MINION_PORT=70000: must be between 1 and 65535
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

## Building with Environment-Specific Configuration

The `MINEXUS_ENV` variable is embedded into binaries at build time for version information and affects the runtime configuration loading.

### Make Build Targets

```bash
# Standard build (should default to production)
make build

# Environment-specific local builds
make build-prod-local      # Production binaries (nexus-prod, minion-prod, console-prod)
make build-test-local      # Test binaries (nexus-test, minion-test, console-test)

# Docker builds (for containerized deployment)
make build-prod           # Production Docker images
make build-test           # Test Docker images
```

### Manual Build with Environment Control

```bash
# Production build (recommended for deployment)
MINEXUS_ENV=prod go build -ldflags "..." -o nexus-prod ./cmd/nexus/

# Test build
MINEXUS_ENV=test go build -ldflags "..." -o nexus-test ./cmd/nexus/
```

**Important Build Considerations:**
- The `MINEXUS_ENV` value is embedded into the binary at build time
- Runtime configuration still requires the appropriate `.env.<environment>` file
- All build targets now default to `MINEXUS_ENV=prod` for consistent production builds
- Override with `MINEXUS_ENV=test` for non-production builds

### Custom Configuration Loading

```go
// Create a custom loader
loader := config.NewConfigLoader().WithLogger(logger)

// Load environment-specific file (uses MINEXUS_ENV to determine which file)
if err := loader.LoadEnvironmentFile(); err != nil {
    log.Fatalf("Failed to load environment file: %v", err)
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
export NEXUS_MINION_PORT=11972
export NEXUS_CONSOLE_PORT=11973
export DBHOST=localhost

# Production
export DEBUG=false
export NEXUS_SERVER=prod-nexus.example.com
export NEXUS_MINION_PORT=443
export NEXUS_CONSOLE_PORT=8443
export DBHOST=prod-db.example.com
export DBSSLMODE=require
```

## Environment-Specific Configuration

Minexus requires environment-specific configurations using dedicated `.env` files determined by the `MINEXUS_ENV` variable. This enables you to maintain separate configurations for production and test environments.

### Creating Environment-Specific Files

Create environment-specific configuration files from the sample template:

```bash
# Create production environment configuration (MINEXUS_ENV=prod)
cp env.sample .env.prod

# Create test environment configuration (MINEXUS_ENV=test)
cp env.sample .env.test
```

### Environment File Format

Each environment file follows the same format. The system automatically loads the appropriate file based on `MINEXUS_ENV`:

**Test Environment (`.env.test`):**
```bash
# Test-specific settings (default)
DEBUG=true
NEXUS_SERVER=localhost
NEXUS_MINION_PORT=11972
NEXUS_CONSOLE_PORT=11973
NEXUS_WEB_PORT=8086

# Test database
DBHOST=localhost
DBPORT=5432
DBUSER=postgres
DBPASS=postgres
DBNAME=minexus_test
DBSSLMODE=disable

# Test-specific minion settings
MINION_ID=test-minion
HEARTBEAT_INTERVAL=30
CONNECT_TIMEOUT=5
```

**Production Environment (`.env.prod`):**
```bash
# Production-specific settings
DEBUG=false
NEXUS_SERVER=prod-nexus.example.com
NEXUS_MINION_PORT=11972
NEXUS_CONSOLE_PORT=11973
NEXUS_WEB_PORT=8086

# Production database with SSL
DBHOST=prod-db.example.com
DBPORT=5432
DBUSER=minexus_prod
DBPASS=secure_production_password
DBNAME=minexus_prod
DBSSLMODE=require

# Production-specific minion settings
MINION_ID=prod-minion-01
HEARTBEAT_INTERVAL=60
CONNECT_TIMEOUT=3
INITIAL_RECONNECT_DELAY=5
MAX_RECONNECT_DELAY=3600
```

### Using Environment-Specific Configurations

#### With Docker Compose

Use the environment-specific make targets for Docker deployments. Each target automatically sets `MINEXUS_ENV`:

```bash
# Production environment (MINEXUS_ENV=prod, uses .env.prod)
make run-prod    # Uses .env.prod
make build-prod  # Build with production settings
make stop-prod   # Stop production environment
make logs-prod   # View production logs

# Test environment (MINEXUS_ENV=test, uses .env.test)
make run-test    # Uses .env.test
make build-test  # Build with test settings
make stop-test   # Stop test environment
make logs-test   # View test logs
```

#### Manual Docker Compose Commands

If you prefer manual control, you can use docker-compose directly with `MINEXUS_ENV`:

```bash
# Production
MINEXUS_ENV=prod docker compose up -d
MINEXUS_ENV=prod docker compose build
MINEXUS_ENV=prod docker compose down

# Test
MINEXUS_ENV=test docker compose up -d
MINEXUS_ENV=test docker compose build
MINEXUS_ENV=test docker compose down
```

#### With Binary Execution

Load environment-specific settings before running binaries by setting `MINEXUS_ENV`:

```bash
# Production
MINEXUS_ENV=prod ./nexus

# Test (default)
MINEXUS_ENV=test ./nexus

# Or export for multiple commands
export MINEXUS_ENV=prod
./nexus
./minion
./console
```

### Security Considerations

**File Permissions:**
```bash
# Restrict access to environment files
chmod 600 .env.prod .env.test
```

**Git Exclusion:**
Environment files are automatically excluded from version control:
```gitignore
# Environment-specific configuration files
.env
.env.*
!.env.sample
```

**Environment Variable Requirements:**
- Set `MINEXUS_ENV` to control which configuration file is loaded
- Valid values: `test` (default), `prod`
- System will panic if invalid value or missing configuration file

**Best Practices:**
- Never commit environment files with sensitive data
- Use different database credentials for each environment
- Enable SSL/TLS in production (`DBSSLMODE=require`)
- Use strong passwords for production environments
- Regularly rotate production credentials
- Consider using external secret management for production

### Environment Validation

The configuration system validates environment-specific settings:

```bash
# Test configuration loading
DEBUG=true ./nexus --help  # Shows loaded configuration

# Validate database connection
./nexus --db-host prod-db.example.com --db-user test_user
```

### Migration Between Environments

When moving between environments, ensure all required variables are set:

```bash
# Compare environment files
diff .env.test .env.prod

# Validate required variables
grep -E "^[A-Z_]+=.+" .env.prod | wc -l
```

## Migration from Legacy System

The unified system maintains backward compatibility:

### Deprecated Functions
- `getEnvString()` - Use `ConfigLoader.GetString()`
- `getEnvInt()` - Use `ConfigLoader.GetInt()`
- `getEnvBool()` - Use `ConfigLoader.GetBool()`

### Legacy Support
- All existing environment variables continue to work
- Command line flags remain unchanged
- Legacy database connection string is supported

## Best Practices

### Security
- Never commit environment-specific files (`.env.prod`, `.env.test`) with sensitive data
- Use environment variables for production secrets
- Validate all configuration values before use
- Ensure `MINEXUS_ENV` is set correctly in production environments

### Development
- Use environment-specific files (`.env.prod`, `.env.test`) for development
- Set reasonable defaults for all values
- Provide clear error messages for invalid configuration
- Always set `MINEXUS_ENV` appropriately for your target environment

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
- Check that the environment-specific file exists (`.env.prod`, `.env.test`)
- Verify `MINEXUS_ENV` is set to a valid value (`prod`, `test`)
- Verify file format (KEY=VALUE)
- Check for syntax errors in the file
- Ensure the file is readable with proper permissions

**"Invalid MINEXUS_ENV value"**
- Verify `MINEXUS_ENV` is set to one of: `prod`, `test`
- Check for typos in the environment variable value

**"Required environment file not found"**
- Create the missing environment file: `cp env.sample .env.<environment>`
- Verify the file exists in the correct location
- Check file permissions are readable

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
