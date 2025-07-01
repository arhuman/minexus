# Testing Guide

This document explains the testing system for the Minexus project, including unit tests, integration tests, and best practices for development and CI/CD workflows.

## Overview

The Minexus project uses a conditional testing system that separates fast unit tests from slower integration tests. This allows developers to run quick tests during development while ensuring comprehensive testing before releases.

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
# Run unit tests only (fast, for development)
make test

# Run all tests including integration tests (comprehensive)
SLOW_TESTS=1 make test

# Generate coverage report
make cover

# Generate coverage report including integration tests
SLOW_TESTS=1 make cover
```

## Testing Commands

### Basic Testing

| Command | Description | Duration | Dependencies |
|---------|-------------|----------|--------------|
| `make test` | Unit tests only | ~5s | None |
| `SLOW_TESTS=1 make test` | All tests | ~60s | Docker |
| `make cover` | Unit test coverage | ~5s | None |
| `SLOW_TESTS=1 make cover` | Full coverage | ~60s | Docker |

### Coverage Analysis

```bash
# Generate HTML coverage report and open in browser
make cover-html

# Generate HTML coverage report including integration tests
SLOW_TESTS=1 make cover-html

# CI/CD coverage output (percentage only)
make cover-ci
```

### Release and Audit

```bash
# Comprehensive release testing (includes integration tests)
make release

# Code quality checks (without tests)
make audit
```

## Environment Variables

### SLOW_TESTS

Controls whether integration tests are executed:

- **Not set** (default): Only unit tests run
- **Set to any value**: Integration tests are included

```bash
# These are equivalent ways to enable integration tests
SLOW_TESTS=1 make test
SLOW_TESTS=true make test
SLOW_TESTS=yes make test
export SLOW_TESTS=1 && make test
```

## Development Workflow

### Daily Development
```bash
# Fast feedback loop during development
make test
```

### Before Committing
```bash
# Comprehensive testing before push
SLOW_TESTS=1 make test
```

### Code Coverage Analysis
```bash
# Check unit test coverage
make cover

# Check comprehensive coverage
SLOW_TESTS=1 make cover-html
```

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
      
      # Unit tests (fast)
      - name: Run unit tests
        run: make test
      
      # Integration tests (comprehensive)
      - name: Run integration tests
        run: SLOW_TESTS=1 make test
        
      # Coverage report
      - name: Generate coverage
        run: SLOW_TESTS=1 make cover-ci
```

### Docker-based CI

```yaml
# For CI environments with Docker support
- name: Run comprehensive tests
  run: |
    # Integration tests automatically start Docker services
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

1. **Use environment variables** - Control test behavior
2. **Document prerequisites** - Clear setup instructions
3. **Automate service management** - Reduce manual steps
4. **Provide cleanup procedures** - Easy environment reset

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