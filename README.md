# Minexus - Distributed Command & Control System

```
⚠️ This code is not ready for production ⚠️
The API, features, configuration are subject to changes.
This software lack the basic security features needed for production
(No TLS encryption/authentication, no minions input sanitization...)
⚠️i This code is not ready for production ⚠️
```

Minexus is a Remote Administration Tool (RAT)
It's made of a central Nexus server, (multiple) Minion clients(s), and a Console client for administration..
You can use it:
- for remote deployment/execution tool (like ansible)
- for monitoring purpose
- for security purpose
- ... (tell us!)

It's current features include:

- **gRPC Communication**: High-performance, cross-platform RPC
- **Tag-based Targeting**: Flexible minion selection using tags
- **Real-time Command Streaming**: Live command delivery to minions
- **Command Result Tracking**: Complete audit trail of command execution
- **Auto-discovery**: Minions automatically register with Nexus
- **Zero-config Defaults**: Works immediately without configuration
- **Flexible Configuration**: Multiple configuration methods
- **Database Persistence**: Command history and minion registry

We focus on modularity and extensibility to make it easy to add new commands.
(more info in [ADDING_COMMANDS.md](documentation/ADDING_COMMANDS.md))

## Quick Start

The system works out-of-the-box with sensible defaults - no configuration required!

The easiest way to launch one minion, a nexus server and it's associated database is through docker compose:
`docker compose up -d`

Then to attach a console:
`docker compose exec console /app/console`

## Project Structure

```
minexus/
├── cmd/                   # Application entry points
│   ├── nexus/             #   Nexus server main
│   ├── minion/            #   Minion client main
│   └── console/           #   Console client main
├── internal/              # Internal packages
│   ├── command/           #   Command implementations for minions
│   ├── config/             #   Configuration system
│   ├── logging/           #   Logging infrastructure
│   ├── minion/            #   Minion client implementation
│   ├── nexus/             #   Nexus server implementation
│   └── version/           #   Version handling
├── proto/                 # Protocol buffer definitions
├── protogen/              # Generated protobuf code
├── config/                 # Configuration files
│   └── docker/            #   Docker configuration
│       └── initdb/        #     Database initialization scripts
├── documentation/         # Project documentation
├── Makefile                # Build and development tasks
├── docker-compose.yml     # Docker Compose configuration
├── Dockerfile.*            # Container build files
└── go.mod                 # Go module definition
```

## Configuration

### Configuration Priority

The configuration system follows this priority order (highest to lowest):

1. **Command Line Flags** (highest priority)
2. **Environment Variables** 
3. **`.env` File**
4. **Default Values** (lowest priority)

For detailed configuration options, see [documentation/CONFIGURATION.md](documentation/CONFIGURATION.md).

For detailed version handling information, see [documentation/VERSION.md](documentation/VERSION.md).


## Architecture

```
┌─────────────┐    gRPC     ┌─────────────┐    PostgreSQL    ┌──────────────┐
│   Console   │◄──────────►│    Nexus    │◄─────────────────│   Database   │
│   Client    │             │   Server    │                  │              │
└─────────────┘             └─────────────┘                  └──────────────┘
                                   ▲
                              gRPC │
                                   │
                     ┌─────────────┼─────────────┐
                     │             │             │
                     ▼             ▼             ▼
              ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
              │   Minion    │ │   Minion    │ │   Minion    │
              │  Client 1   │ │  Client 2   │ │  Client N   │
              └─────────────┘ └─────────────┘ └─────────────┘
```


## Documentation

More documentation is available in the [`documentation/`](documentation/) directory:

- **[ADDING_COMMANDS.md](documentation/ADDING_COMMANDS.md)** - Developer oriented guide to add commands to Minexus
- **[CONFIGURATION.md](documentation/CONFIGURATION.md)** - Complete configuration guide for all components
- **[VERSION.md](documentation/VERSION.md)** - Version handling, building, and querying guide
- **[COMMANDS.md](documentation/COMMANDS.md)** - Complete guide to all minion commands
- **[TESTING.md](documentation/TESTING.md)** - Comprehensive testing guide and best practices
- **[SIMPLIFICATION.md](documentation/SIMPLIFICATION.md)** - Architecture improvements and simplification details
- **[IMPLEMENTATION_PLAN.md](documentation/IMPLEMENTATION_PLAN.md)** - Detailed implementation status and refactoring progress

### Quick Documentation Links

- [Available Commands](documentation/COMMANDS.md#overview) - All commands that can be sent to minions
- [Console Help System](documentation/COMMANDS.md#console-help-system) - Interactive command help
- [File Operations](documentation/COMMANDS.md#file-commands) - File manipulation commands
- [System Commands](documentation/COMMANDS.md#system-commands) - System information and shell commands
- [Testing Guide](documentation/TESTING.md#overview) - Unit tests, integration tests, and best practices
- [Development Workflow](documentation/TESTING.md#development-workflow) - Fast unit tests vs comprehensive testing
- [CI/CD Integration](documentation/TESTING.md#cicd-integration) - Automated testing setup
- [Configuration Options](documentation/CONFIGURATION.md#configuration-options) - All available settings
- [Version Information](documentation/VERSION.md#querying-version-information) - How to check versions
- [Build with Custom Versions](documentation/VERSION.md#setting-and-changing-versions) - Custom version builds
- [Troubleshooting](documentation/CONFIGURATION.md#troubleshooting) - Common issues and solutions

## Running

### Prerequisites

- Docker and Docker Compose
- Go 1.23.1 or later
- PostgreSQL (optional - can use existing database or create/run a docker image)

For development you also need

- Protocol Buffers compiler (`protoc`)

### Running binaries

```bash
# Build all components
go build -o nexus ./cmd/nexus
go build -o minion ./cmd/minion
go build -o console ./cmd/console

# Start Nexus server (uses defaults)
nohup ./nexus > nexus.log &

# In another terminal, start a Minion client (uses defaults)
nohup ./minion > minion.log &

# In another terminal, start the Console client (uses defaults)
./console
```

That's it! The system will use sensible defaults and work immediately.

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
- **nexus**: Nexus server (gRPC port 11972) with health checks
- **minion**: Single minion client that connects to nexus automatically
- **console**: Interactive console client (optional, use `--profile console`)

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

1. Fork the develop branch
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

This project is licensed under the terms of the [MIT License](LICENSE).

## Support

For issues and questions, please use the issue tracker.
