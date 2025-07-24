# Minexus - Distributed Command & Control System

```
⚠️ This code is not ready for production ⚠️
The API, features, configuration are subject to changes.
This software lacks some security features needed for production
(no minions input sanitization, resource limiting...)
⚠️ This code is not ready for production ⚠️
```
[![Go Report Card](https://goreportcard.com/badge/github.com/arhuman/minexus)](https://goreportcard.com/report/github.com/arhuman/minexus)
[![Build](https://github.com/arhuman/minexus/actions/workflows/CI.yml/badge.svg)](https://github.com/arhuman/minexus/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Minexus is a Remote Administration Tool (RAT) first used as a faster alternative to ansible.
It's made of a central Nexus server, (multiple) Minion clients(s), and a Console client for administration..
You can use it:
* for remote deployment/execution tool (like ansible)
* for monitoring purpose
* for security purpose
* ... (tell us!)

Exemple of currently implemented commands:
Tag Management:

* tag-set \<minion-id\> \<key\>=\<value\> \[...\]    - Set tags for a minion (replaces all)
* tag-update \<minion-id\> +\<key\>=\<value\> -\<key\> \[...\] - Update tags for a minion
* tag-list, lt                               - List all available tags

Command management:

*  command-send all \<cmd\>                     - Send command to all minions
*  command-send minion \<id\> \<cmd\>             - Send command to specific minion
*  command-send tag \<key\>=\<value\> \<cmd\>       - Send command to minions with tag
*  command-status all                         - Show status breakdown of all commands
*  command-status minion \<id\>                 - Show detailed status of commands for a minion
*  command-status stats                       - Show command execution statistics by minion
*  result-get \<cmd-id\>                        - Get results for a command ID

Where \<cmd\> can be:

* Any shell command ⚠️ This command is not filtered in any way, and may be deprecated in future release for security reason ⚠️
* A built-in file command (get, copy, move, info)
* A built-in system command (os, status)
* A built-in docker-compose command (ps, up, down)
* A built-in logging command (level, increase, decrease)
* ...

It's current features include:

- **gRPC Communication**: High-performance, cross-platform RPC
- **TLS Encryption**: Secure communication between all components
- **Web Interface**: User-friendly HTTP dashboard and REST API
- **Tag-based Targeting**: Flexible minion selection using tags
- **Real-time Command Streaming**: Live command delivery to minions
- **Command Result Tracking**: Complete audit trail of command execution
- **Auto-discovery**: Minions automatically register with Nexus
- **Zero-config Defaults**: Works immediately without configuration
- **Flexible Configuration**: Multiple configuration methods
- **Database Persistence**: Command history and minion registry

We focus on modularity and extensibility to make it easy to add new commands.
(more info in [adding_commands.md](documentation/adding_commands.md))

## Why Minexus

Although I was very satisfied with ansible for deployment, I found it not practical for remote admistration and monitoring:
- Ansible is to slow
- Ansible multiple outpout handling is not convenient
- Poor monitoring (No state management) requiring the use of other tools (telegraf/grafana...)

Plus I was planning to make a security agent for other needs.
So I decided to make this agent (minion) the server (nexus) and start by implementing the basic architecture for remote administration and then add incrementally new features, so that minexus could cover both my administration, monitoring and security needs.

## Quick Start

The system works out-of-the-box with sensible defaults - no configuration required!

The easiest way to launch one minion, a nexus server and it's associated database is through docker compose:
`docker compose up -d`

Then to attach a console:
`docker compose exec console /app/console`

## Project Structure

```
minexus/
├── .github/               # GitHub Actions workflows and issue templates
│   ├── ISSUE_TEMPLATE/    #   Issue templates
│   └── workflows/         #   CI/CD workflows
├── cmd/                   # Application entry points
│   ├── console/           #   Console client main
│   ├── minion/            #   Minion client main
│   └── nexus/             #   Nexus server main
├── config/                # Configuration files
│   └── docker/            #   Docker configuration
│       └── initdb/        #     Database initialization scripts
├── documentation/         # Project documentation
├── internal/              # Internal packages
│   ├── certs/             #   TLS certificate generation and management
│   ├── command/           #   Command implementations for minions
│   ├── config/            #   Configuration system
│   ├── logging/           #   Logging infrastructure
│   ├── minion/            #   Minion client implementation
│   ├── nexus/             #   Nexus server implementation
│   └── version/           #   Version handling
├── proto/                 # Protocol buffer definitions
├── protogen/              # Generated protobuf code
├── CODE_OF_CONDUCT.md     # Code of Conduct for contributors
├── Dockerfile.*           # Dockerfiles for different components
├── LICENSE                # Project license information
├── Makefile               # Build and development tasks
├── README.md              # Project overview and documentation
├── docker-compose.yml     # Docker Compose configuration
└── env.sample             # Sample environment file
```

## Configuration

### Configuration Priority

The configuration system follows this priority order (highest to lowest):

1. **Command Line Flags** (highest priority)
2. **Environment Variables** 
3. **`.env` File**
4. **Default Values** (lowest priority)

For detailed configuration options, see [documentation/configuration.md](documentation/configuration.md).

For detailed version handling information, see [documentation/version.md](documentation/version.md).

### TLS Configuration

Minexus supports TLS encryption for secure communication between all components. TLS can be enabled using configuration files, environment variables, or command-line flags.

#### TLS Certificates (Embedded at Build Time)

TLS encryption is mandatory for all Minexus components. Certificates are embedded directly into the binaries at build time for security and simplicity.

#### Generating TLS Certificates for Development

For development builds, certificates are embedded from `internal/certs/`. Use OpenSSL to generate them:

```bash
# Certificate generation is handled by the certificate generation script
# See documentation/certificate_generation.md for details

# Rebuild binaries to embed the new certificates
make build
```

**Important:** For production environments, replace the certificates in `internal/certs/` with certificates signed by a trusted Certificate Authority (CA) before building.

#### Security Notes

- **Certificate Management**: In production, use certificates from a trusted CA
- **Private Key Security**: Protect private key files with appropriate file permissions (`chmod 600`)
- **Certificate Rotation**: Plan for regular certificate renewal and rotation


## Architecture

```
┌─────────────┐    gRPC     ┌─────────────┐    PostgreSQL    ┌──────────────┐
│   Console   │◄───────────►│    Nexus    │◄─────────────────│   Database   │
│   Client    │   (mTLS)    │   Server    │                  │              │
└─────────────┘   :11973    │             │                  └──────────────┘
                             │  Triple-    │
┌─────────────┐    HTTP      │  Server     │
│ Web Browser │◄───────────►│ Architecture│
│  Dashboard  │   :8086     │             │
└─────────────┘             └─────────────┘
                                   ▲
                              gRPC │ (TLS)
                                   │ :11972
                     ┌─────────────┼─────────────┐
                     │             │             │
                     ▼             ▼             ▼
              ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
              │   Minion    │ │   Minion    │ │   Minion    │
              │  Client 1   │ │  Client 2   │ │  Client N   │
              └─────────────┘ └─────────────┘ └─────────────┘
```

### Triple-Server Architecture

The Nexus server runs three concurrent services:

- **Minion Server** (port 11972): TLS gRPC server for minion connections
- **Console Server** (port 11973): mTLS gRPC server for console connections
- **Web Server** (port 8086): HTTP server for web interface and REST API


## Documentation

More documentation is available in the [`documentation/`](documentation/) directory:

- **[adding_commands.md](documentation/adding_commands.md)** - Developer oriented guide to add commands to Minexus
- **[configuration.md](documentation/configuration.md)** - Complete configuration guide for all components
- **[Webserver.md](documentation/Webserver.md)** - Web interface and REST API documentation
- **[version.md](documentation/version.md)** - Version handling, building, and querying guide
- **[commands.md](documentation/commands.md)** - Complete guide to all console and minion commands
- **[testing.md](documentation/testing.md)** - Comprehensive testing guide and best practices

### Quick Documentation Links

- [Available Commands](documentation/commands.md#overview) - All commands that can be sent to minions
- [Console Help System](documentation/commands.md#console-help-system) - Interactive command help
- [File Operations](documentation/commands.md#file-commands) - File manipulation commands
- [System Commands](documentation/commands.md#system-commands) - System information and shell commands
- [Testing Guide](documentation/testing.md#overview) - Unit tests, integration tests, and best practices
- [Development Workflow](documentation/testing.md#development-workflow) - Fast unit tests vs comprehensive testing
- [CI/CD Integration](documentation/testing.md#cicd-integration) - Automated testing setup
- [Configuration Options](documentation/configuration.md#configuration-options) - All available settings
- [Version Information](documentation/version.md#querying-version-information) - How to check versions
- [Build with Custom Versions](documentation/version.md#setting-and-changing-versions) - Custom version builds
- [Troubleshooting](documentation/configuration.md#troubleshooting) - Common issues and solutions

## Running

### Prerequisites

- Docker and Docker Compose
- Go 1.23.1 or later
- PostgreSQL (optional - can use existing database or create/run a docker image)

For development you also need

- Protocol Buffers compiler (`protoc`)

### Running binaries

```bash
# Build all components (embeds TLS certificates)
go build -o nexus ./cmd/nexus
go build -o minion ./cmd/minion
go build -o console ./cmd/console

# Start Nexus server with dual-port architecture (TLS is mandatory, certificates embedded in binary)
# Port 11972 for minions (standard TLS), Port 11973 for console (mTLS)
nohup ./nexus > nexus.log &

# In another terminal, start a Minion client (TLS is mandatory)
nohup ./minion > minion.log &

# In another terminal, start the Console client (TLS is mandatory)
./console
```

**Note:** TLS encryption is mandatory and certificates are embedded at build time. No external certificate files are required at runtime.

## Running containers (Docker compose)

For local development, you can use Docker Compose to launch the complete triad (nexus/minion/console) with a PostgreSQL database:

```bash
# Start the full development stack (nexus server + minion + database)
docker compose up

# Start with console for interactive testing
docker compose --profile console up

# Start only specific services
docker compose up nexus          # Just nexus and database
docker compose up nexus minion   # Nexus, minion, and database

# Run in background
docker compose up -d

# View logs
docker compose logs -f nexus
docker compose logs -f minion

# Stop all services
docker compose down
```

### Service Overview

- **nexus_db**: PostgreSQL database with automatic schema initialization
- **nexus**: Nexus server with triple-server architecture:
  - **Port 11972** (`NEXUS_MINION_PORT`) - Minion connections with standard TLS
  - **Port 11973** (`NEXUS_CONSOLE_PORT`) - Console connections with mutual TLS (mTLS)
  - **Port 8086** (`NEXUS_WEB_PORT`) - Web interface and REST API over HTTP
- **minion**: Single minion client that connects to nexus using `NEXUS_SERVER` + `NEXUS_MINION_PORT`
- **console**: Interactive console client that connects using `NEXUS_SERVER` + `NEXUS_CONSOLE_PORT` (optional, use `--profile console`)

The docker-compose setup includes:
- Automatic service dependency management
- Health checks and restart policies
- Shared networking between services
- Volume persistence for database
- Environment variable configuration

### Console Access

The console service is configured with `stdin_open` and `tty` for interactive use:

```bash
# Start with console
docker compose --profile console up

# Or attach to running console
docker compose exec console /app/console
```

### Web Interface Access

The web interface provides a user-friendly dashboard and REST API for monitoring and managing your Minexus infrastructure:

```bash
# Access the web dashboard (when nexus is running)
open http://localhost:8086

# Or use the REST API directly
curl http://localhost:8086/api/status
curl http://localhost:8086/api/minions
curl http://localhost:8086/api/health

# Download pre-built binaries
curl -O http://localhost:8086/download/minion/linux-amd64
curl -O http://localhost:8086/download/console/windows-amd64.exe
```

**Web Interface Features:**
- **Dashboard**: Real-time system status, connected minions, and server health
- **REST API**: Programmatic access to system information
- **Binary Downloads**: Direct access to pre-built minion and console binaries
- **Monitoring**: Standard HTTP endpoints for integration with monitoring tools

For complete web interface documentation, see [documentation/Webserver.md](documentation/Webserver.md).

## Usage Examples

### Docker Compose Management

Minexus provides built-in docker-compose commands for managing containerized applications across your infrastructure:

```bash
# Check status of services across all web servers
command-send tag role=web "docker-compose:ps /opt/myapp"

# Deploy application to staging environment
command-send tag env=staging '{"command": "up", "path": "/opt/myapp", "build": true}'

# Restart specific service on production servers
command-send tag env=prod '{"command": "down", "path": "/opt/myapp", "service": "web"}'
command-send tag env=prod '{"command": "up", "path": "/opt/myapp", "service": "web"}'

# Stop application on all servers
command-send all "docker-compose:down /opt/myapp"
```

### System Administration

```bash
# Check system resources across infrastructure
command-send all "system:info"
command-send tag role=database "df -h"

# Update configuration files
command-send minion web-01 'file:get /etc/nginx/nginx.conf'
command-send minion web-01 '{"command": "copy", "source": "/tmp/nginx.conf", "destination": "/etc/nginx/nginx.conf"}'

# Service management
command-send tag env=prod "systemctl restart nginx"
command-send all "systemctl status docker"
```

### Monitoring and Troubleshooting

```bash
# Check application logs
command-send tag role=web "docker-compose:ps /opt/myapp"
command-send minion web-01 "docker logs myapp_web_1"

# Network diagnostics
command-send all "netstat -tulpn | grep :80"
command-send minion web-01 "ping -c 3 database-server"

# Process monitoring
command-send all "ps aux | grep nginx"
command-send tag role=database "top -b -n 1 | head -20"
```

## Development

### Build from Source
```bash
git clone <repository>
cd minexus

# Generate protobuf code (if needed)
make proto

# Build all components
make build

# Run unit tests only (fast)
make test

# Run all tests including integration tests (slow)
SLOW_TESTS=1 make test
```

### Testing

The project uses a conditional testing system that separates fast unit tests from slower integration tests:

#### Unit Tests (Default)
```bash
# Run unit tests only - fast, no external dependencies
make test

# Generate coverage report for unit tests
make cover
```

#### Integration Tests (Conditional)
```bash
# Run all tests including integration tests - requires Docker services
SLOW_TESTS=1 make test

# Generate coverage report including integration tests
SLOW_TESTS=1 make cover

# View detailed coverage in browser
SLOW_TESTS=1 make cover-html
```

#### Testing recommended workflow

**Development Workflow:**
- Use `make test` for frequent testing during development (fast feedback)
- Use `SLOW_TESTS=1 make test` before committing changes (comprehensive testing)
- Integration tests automatically start required Docker services

**CI/CD Workflow:**
```bash
# For CI pipelines - includes integration tests and generates coverage
make cover-ci

# For release builds - comprehensive testing with audit
make release
```

**Environment Variables:**
- `SLOW_TESTS=1` - Enables integration tests that require Docker services
- Integration tests automatically handle Docker Compose service lifecycle
- Services are started only if not already running

**Test Categories:**
- **Unit Tests**: Fast, isolated tests with no external dependencies
- **Integration Tests**: End-to-end tests requiring Nexus, Minion, and PostgreSQL services
- **Coverage**: Both unit and integration coverage available depending on `SLOW_TESTS` setting

## Contributing

All contributions are welcome!
See [CONTRIBUTING.md](CONTRIBUTING.md) for detailed information.

## License

This project is licensed under the terms of the [MIT License](LICENSE).

## Support

For issues and questions, please use the issue tracker.
