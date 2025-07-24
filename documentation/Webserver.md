# Minexus Web Server

## Overview

The Minexus web server provides a user-friendly HTTP interface for monitoring and managing the Minexus system. It runs alongside the existing gRPC servers in a triple-server architecture, offering dashboard views, REST API endpoints, and binary download capabilities.

## Architecture

### Triple-Server Design

The Minexus nexus now operates with three concurrent servers:

- **Minion Server** (port 11972): TLS gRPC server for minion connections
- **Console Server** (port 11973): mTLS gRPC server for console connections  
- **Web Server** (port 8086): HTTP server for web interface and API

All three servers run concurrently and can be configured independently.

## Configuration

### Default Settings

```bash
# Environment Variables
NEXUS_WEB_PORT=8086          # HTTP server port
NEXUS_WEB_ENABLED=true       # Enable/disable web server
NEXUS_WEB_ROOT=./webroot     # Path to web assets directory
```

### Command Line Flags

```bash
./nexus --web-port=8086 --web-enabled=true --web-root=./webroot
```

### Configuration Fields

The [`NexusConfig`](../internal/config/config.go) struct includes:

- `WebPort int` - Port for HTTP web server (default: 8086)
- `WebEnabled bool` - Enable/disable web server (default: true)
- `WebRoot string` - Path to webroot directory (default: "./webroot")

## Docker Usage

The web server is automatically included when running Minexus with Docker Compose:

```bash
# Start all services including web server
docker compose up -d

# Access web interface at http://localhost:8086
```

**Port Configuration:**
- The web server port is exposed and configurable via `NEXUS_WEB_PORT` environment variable
- Default port: 8086
- The port is automatically mapped from container to host

**Environment Variables:**
Configure in your `.env` file:
```env
# Web server port (mapped to host)
NEXUS_WEB_PORT=8086
# Enable/disable web server
NEXUS_WEB_ENABLED=true
# Web assets directory
NEXUS_WEB_ROOT=webroot
```

**Docker Health Check:**
The nexus_server container health check verifies all three ports are accessible:
- NEXUS_MINION_PORT (default: 11972) - gRPC minions
- NEXUS_CONSOLE_PORT (default: 11973) - gRPC console
- NEXUS_WEB_PORT (default: 8086) - HTTP web server

**Accessing the Web Interface:**
After `docker compose up`, the web interface will be available at:
- Dashboard: http://localhost:8086/
- API Health: http://localhost:8086/api/health
- Downloads: http://localhost:8086/download/

## Web Interface Features

### Dashboard (`GET /`)

The main dashboard provides:

- **System Information**
  - Nexus version and build information
  - Server uptime display
  - Current system status indicator

- **Connected Minions**
  - Total minion count
  - Individual minion details with IDs and status
  - Connection timestamps

- **Server Status**
  - All three server port statuses
  - Database connection status
  - Health indicators

- **Quick Actions**
  - Links to binary downloads
  - Direct access to API endpoints

### Binary Downloads (`GET /download/`)

Provides access to pre-built binaries:

#### Minion Binaries
- `/download/minion/linux-amd64` - Linux x64 minion binary
- `/download/minion/linux-arm64` - Linux ARM64 minion binary  
- `/download/minion/windows-amd64.exe` - Windows x64 minion executable
- `/download/minion/windows-arm64.exe` - Windows ARM64 minion executable
- `/download/minion/darwin-amd64` - macOS x64 minion binary
- `/download/minion/darwin-arm64` - macOS ARM64 minion binary

#### Console Binaries
- `/download/console/linux-amd64` - Linux x64 console binary
- `/download/console/linux-arm64` - Linux ARM64 console binary
- `/download/console/windows-amd64.exe` - Windows x64 console executable
- `/download/console/windows-arm64.exe` - Windows ARM64 console executable
- `/download/console/darwin-amd64` - macOS x64 console binary
- `/download/console/darwin-arm64` - macOS ARM64 console binary

**Note**: Binary serving currently returns informational messages. For production use, place pre-built binaries in a `binaries/` directory structure.

## REST API

### System Status (`GET /api/status`)

Returns comprehensive system information:

```json
{
  "version": "1.0.0",
  "uptime": "2h 45m 12s", 
  "timestamp": "2024-01-15T10:30:00Z",
  "servers": {
    "minion": {
      "port": 11972,
      "status": "running",
      "connections": 5
    },
    "console": {
      "port": 11973, 
      "status": "running",
      "connections": 0
    },
    "web": {
      "port": 8086,
      "status": "running"
    }
  },
  "database": {
    "status": "connected",
    "host": "localhost:5432"
  }
}
```

### Minions Information (`GET /api/minions`)

Returns details about connected minions:

```json
{
  "count": 2,
  "minions": [
    {
      "id": "minion-001",
      "connected_at": "2024-01-15T08:15:30Z",
      "last_seen": "2024-01-15T10:29:45Z", 
      "status": "active"
    }
  ]
}
```

### Health Check (`GET /api/health`)

Simple health endpoint for monitoring:

```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

## Security Features

### HTTP Security Headers

All responses include appropriate security headers:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `X-XSS-Protection: 1; mode=block`
- `Referrer-Policy: strict-origin-when-cross-origin`

### CORS Configuration

API endpoints support cross-origin requests:

- `Access-Control-Allow-Origin: *`
- `Access-Control-Allow-Methods: GET, OPTIONS`
- `Access-Control-Allow-Headers: Content-Type`

### Binary Download Security

- Proper Content-Type headers for binary downloads
- Content-Disposition headers for safe filename handling
- Input validation for component and platform names
- Request logging for download tracking

## Implementation Details

### File System Assets

The web server uses file system assets for maximum flexibility:

- Templates: [`webroot/templates/`](../webroot/templates/)
- Static assets: [`webroot/static/`](../webroot/static/)
- Assets are loaded from the file system at runtime
- Webroot directory can be customized via `NEXUS_WEB_ROOT` configuration
- Allows for easy customization without recompiling the binary

### Data Integration

- Real-time minion data from gRPC [`ListMinions`](../internal/nexus/nexus.go) calls
- Database health checks via nexus server connectivity
- Uptime tracking from server start time
- Live connection counts and status

### Logging and Monitoring

All HTTP requests are logged with:

- Request method and path
- Client remote address and user agent
- Response status code and duration
- Structured logging via zap logger

### Error Handling

- Graceful degradation when nexus server unavailable
- Proper HTTP status codes for all conditions
- JSON error responses for API endpoints
- User-friendly error messages

## Usage Examples

### Starting the Web Server

```bash
# Default configuration
./nexus

# Custom web port
./nexus --web-port=9090

# Disable web server
./nexus --web-enabled=false

# Custom webroot directory
./nexus --web-root=/custom/webroot
```

### Accessing the Interface

```bash
# Main dashboard
curl http://localhost:8086/

# System status API
curl http://localhost:8086/api/status

# Minion information
curl http://localhost:8086/api/minions

# Health check
curl http://localhost:8086/api/health

# Download index
curl http://localhost:8086/download/
```

### Monitoring Integration

The API endpoints are designed for integration with monitoring systems:

```bash
# Health check for monitoring
curl -f http://localhost:8086/api/health

# Minion count monitoring
curl -s http://localhost:8086/api/minions | jq '.count'

# System status for alerting
curl -s http://localhost:8086/api/status | jq '.database.status'
```

## Testing

Comprehensive test coverage includes:

- HTTP handler testing with mock responses
- Security header validation
- API endpoint response verification
- Binary download functionality
- Template rendering validation
- Error condition handling

Run tests with:

```bash
go test ./internal/web -v
```

## Benefits

### Operational Benefits
- **Easy Health Monitoring** - Simple HTTP endpoints for system status
- **User-Friendly Interface** - No specialized gRPC tools required
- **Flexible Configuration** - Comprehensive configuration options
- **Monitoring Integration** - Standard HTTP endpoints for monitoring tools

### Development Benefits
- **Optional Feature** - Can be disabled without affecting gRPC functionality
- **File System Assets** - Easy customization without recompiling
- **Comprehensive Testing** - Full test coverage for reliability
- **Real-time Data** - Live integration with nexus server state

### Security Benefits
- **Standard HTTP Security** - Industry-standard security headers
- **Read-only Interface** - No administrative functions via web interface
- **Input Validation** - Proper validation for all user inputs
- **Request Logging** - Complete audit trail of web requests

The web server provides a modern, accessible interface to the Minexus system while maintaining the robust gRPC foundation for core functionality.