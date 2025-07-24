# Testing Guide

This document explains the testing system for the Minexus project, including unit tests, integration tests, and best practices for development and CI/CD workflows.

## Overview

The Minexus project uses a conditional testing system that separates fast unit tests from slower integration tests. This allows developers to run quick tests during development while ensuring comprehensive testing before releases.

**All test commands automatically run in the test environment** (`MINEXUS_ENV=test`) and use the test configuration file (`.env.test`) for consistent test behavior.

## Test Categories

### Unit Tests
- **Fast execution** (typically < 5 seconds)
- **No external dependencies** (no Docker, database, or network services)
- **Run by default** with `make test`
- **Focus on individual components** and business logic

### Integration Tests
- **Slower execution** (typically 30-60 seconds)
- **Require Docker services** (Nexus server, Minion clients, PostgreSQL database)
- **Run conditionally** with `SLOW_TESTS=1 make test`
- **Test end-to-end workflows** and service interactions

## Quick Start

```bash
# Run unit tests only (fast, for development) - automatically uses MINEXUS_ENV=test
make test

# Run all tests including integration tests (comprehensive) - automatically uses MINEXUS_ENV=test
SLOW_TESTS=1 make test

# Generate coverage report - automatically uses MINEXUS_ENV=test
make cover

# Generate coverage report including integration tests - automatically uses MINEXUS_ENV=test
SLOW_TESTS=1 make cover
```

**Environment Note:** All test commands automatically set `MINEXUS_ENV=test` and load the test configuration from `.env.test`. No manual environment configuration is required for testing.

## Testing Commands

### Basic Testing

| Command | Description | Duration | Dependencies | Environment |
|---------|-------------|----------|--------------|-------------|
| `make test` | Unit tests only | ~5s | None | `MINEXUS_ENV=test` |
| `SLOW_TESTS=1 make test` | All tests | ~60s | Docker | `MINEXUS_ENV=test` |
| `make cover` | Unit test coverage | ~5s | None | `MINEXUS_ENV=test` |
| `SLOW_TESTS=1 make cover` | Full coverage | ~60s | Docker | `MINEXUS_ENV=test` |

### Coverage Analysis

```bash
# Generate HTML coverage report and open in browser (uses MINEXUS_ENV=test)
make cover-html

# Generate HTML coverage report including integration tests (uses MINEXUS_ENV=test)
SLOW_TESTS=1 make cover-html

# CI/CD coverage output (percentage only) (uses MINEXUS_ENV=test)
make cover-ci
```

### Release and Audit

```bash
# Comprehensive release testing (includes integration tests) (uses MINEXUS_ENV=test)
make release

# Code quality checks (without tests) (uses MINEXUS_ENV=test)
make audit
```

## Environment Variables

### MINEXUS_ENV (Automatic)

All test commands automatically set `MINEXUS_ENV=test` to ensure consistent test behavior:

- **Automatic Setting**: Test targets set `MINEXUS_ENV=test` without manual intervention
- **Configuration File**: Uses `.env.test` for test-specific configuration
- **Isolation**: Ensures tests don't interfere with development or production environments

### SLOW_TESTS

Controls whether integration tests are executed:

- **Not set** (default): Only unit tests run
- **Set to any value**: Integration tests are included

```bash
# These are equivalent ways to enable integration tests (all use MINEXUS_ENV=test automatically)
SLOW_TESTS=1 make test
SLOW_TESTS=true make test
SLOW_TESTS=yes make test
export SLOW_TESTS=1 && make test
```

## Development Workflow

### Daily Development
```bash
# Fast feedback loop during development (automatically uses MINEXUS_ENV=test)
make test
```

### Before Committing
```bash
# Comprehensive testing before push (automatically uses MINEXUS_ENV=test)
SLOW_TESTS=1 make test
```

### Code Coverage Analysis
```bash
# Check unit test coverage (automatically uses MINEXUS_ENV=test)
make cover

# Check comprehensive coverage (automatically uses MINEXUS_ENV=test)
SLOW_TESTS=1 make cover-html
```

### Environment Considerations
- **Test Isolation**: All tests run in test environment to avoid affecting dev/prod configurations
- **Configuration**: Ensure `.env.test` file exists with appropriate test settings
- **Database**: Test environment uses separate test database (typically `minexus_test`)

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Test
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v3
        with:
          go-version: 1.23.1
      
      # Unit tests (fast) - automatically uses MINEXUS_ENV=test
      - name: Run unit tests
        run: make test
      
      # Integration tests (comprehensive) - automatically uses MINEXUS_ENV=test
      - name: Run integration tests
        run: SLOW_TESTS=1 make test
        
      # Coverage report - automatically uses MINEXUS_ENV=test
      - name: Generate coverage
        run: SLOW_TESTS=1 make cover-ci
```

**CI Environment Notes:**
- All test commands automatically use `MINEXUS_ENV=test`
- Ensure `.env.test` file is available or provide test configuration via environment variables
- Test database and services are isolated from production

### Docker-based CI

```yaml
# For CI environments with Docker support
- name: Run comprehensive tests
  run: |
    # Integration tests automatically start Docker services and use MINEXUS_ENV=test
    SLOW_TESTS=1 make test
```

## Integration Test Architecture

### Automatic Service Management

Integration tests automatically manage Docker Compose services:

1. **Service Detection**: Checks if required services are running
2. **Automatic Startup**: Starts Nexus, Minion, and PostgreSQL if needed
3. **Health Checks**: Waits for services to be ready before testing
4. **Cleanup**: Services remain running for subsequent test runs

### Required Services

- **nexus_db**: PostgreSQL database with schema initialization
- **nexus_server**: Nexus gRPC server (port 11972 for minions)
- **minion_1**: Test minion client connected to Nexus

### Test Categories

Integration tests cover:

- **Console Commands**: Help, version, minion listing
- **Shell Commands**: Remote command execution via minions
- **File Commands**: File operations on remote systems
- **System Commands**: System information gathering
- **Error Handling**: Invalid commands and edge cases
- **Database Integrity**: Data consistency and relationships

## Troubleshooting

### Common Issues

#### Docker Services Not Starting
```bash
# Check service status
docker compose ps

# View service logs
docker compose logs nexus
docker compose logs minion

# Restart services
docker compose down && docker compose up -d
```

#### Port Conflicts
```bash
# Check if port 11972 is in use
lsof -i :11972

# Or use netcat
nc -z localhost 11972
```

#### Database Connection Issues
```bash
# Check database connectivity
docker compose exec nexus_db pg_isready -U postgres

# Connect to database directly
docker compose exec nexus_db psql -U postgres -d minexus
```

### Performance Tips

#### Development Speed
```bash
# Keep Docker services running between test runs
docker compose up -d

# Use unit tests for rapid iteration
make test
```

#### CI/CD Optimization
```bash
# Use Docker layer caching
# Run unit tests first (fast feedback)
# Run integration tests in parallel when possible
```

## File Organization

### Test Files

```
minexus/
├── integration_test.go          # Integration tests (conditional execution)
├── run_tests.sh                 # Test runner script
├── cmd/
│   ├── console/console_test.go  # Console unit tests
│   ├── minion/minion_test.go    # Minion unit tests
│   └── nexus/nexus_test.go      # Nexus unit tests
└── internal/
    ├── minion/minion_test.go    # Internal minion tests
    └── nexus/nexus_test.go      # Internal nexus tests
```

### Coverage Files

```
coverage.out                     # Coverage data
coverage.html                    # HTML coverage report
```

## Best Practices

### Test Development

1. **Write unit tests first** - Fast feedback during development
2. **Add integration tests for workflows** - End-to-end validation
3. **Use descriptive test names** - Clear test purpose
4. **Test error conditions** - Validate error handling
5. **Keep tests isolated** - No dependencies between tests

### Environment Management

1. **Use test environment** - All tests automatically run with `MINEXUS_ENV=test`
2. **Test configuration** - Ensure `.env.test` exists with proper test settings
3. **Environment isolation** - Tests don't interfere with dev/prod environments
4. **Document prerequisites** - Clear setup instructions
5. **Automate service management** - Reduce manual steps
6. **Provide cleanup procedures** - Easy environment reset

### CI/CD Integration

1. **Separate fast and slow tests** - Optimize feedback loops
2. **Use appropriate timeouts** - Handle slow operations
3. **Cache dependencies** - Reduce CI execution time
4. **Generate coverage reports** - Track test effectiveness

## Advanced Usage

### Custom Test Scenarios

```bash
# Test specific packages only
go test ./internal/nexus/... -v

# Test with race detection
go test -race ./... -v

# Test with verbose output and timeout
SLOW_TESTS=1 go test -v -timeout 300s ./...

# Run specific integration test
SLOW_TESTS=1 go test -run TestIntegrationSuite/ConsoleCommands -v
```

### Debug Mode

```bash
# Enable debug logging during tests
DEBUG=1 SLOW_TESTS=1 make test

# Run tests with additional logging
go test -v -args -debug ./...
```

### Manual Service Control

```bash
# Start services manually
docker compose up -d nexus minion

# Run tests against existing services
SLOW_TESTS=1 make test

# Stop services
docker compose down