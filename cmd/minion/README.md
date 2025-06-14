# Minion Tests

This directory contains tests for the Minion command in [`minion.go`](minion.go).

## Test Categories

### Fast Tests (Default)
By default, `go test` runs only fast unit tests that complete in under 1 second. These tests cover:

- Configuration loading and validation
- Logger setup 
- Version flag handling
- Mock-based unit tests for core functionality
- Environment variable parsing
- Basic component validation

### Slow Tests (Integration Tests)
Integration tests that test the complete minion lifecycle are gated behind the `SLOW_TESTS` environment variable. These tests:

- Create real minion instances with full goroutine lifecycle
- Test command execution end-to-end
- Test registration, heartbeat, and reconnection logic
- Simulate network delays and error conditions
- Take 1-5+ seconds each to complete

## Running Tests

### Run Fast Tests Only (Default)
```bash
go test -v
```

### Run All Tests (Including Slow Integration Tests)
```bash
SLOW_TESTS=true go test -v
```

### Run with Coverage
```bash
# Fast tests only
go test -cover -v

# All tests with coverage
SLOW_TESTS=true go test -cover -v
```

### Run Specific Test
```bash
# Fast test
go test -run TestConfigurationLoading -v

# Slow test (requires SLOW_TESTS=true)
SLOW_TESTS=true go test -run TestMinionCommandExecution -v
```

## Performance Considerations

**Fast Tests (default):**
- Total runtime: ~2-5 seconds
- Individual tests: < 1 second each
- Suitable for continuous integration and development

**Slow Tests (with SLOW_TESTS=true):**
- Total runtime: ~60+ seconds  
- Individual tests: 1-15 seconds each
- Suitable for comprehensive testing and release validation

## Test Coverage

The test suite provides comprehensive coverage of:

✅ **Core Functionality**
- Version flag handling (`--version`, `-v`)
- Configuration loading and validation  
- Logger setup (debug and production modes)
- Minion creation and lifecycle management

✅ **Registration & Communication**
- Successful registration with server
- Registration failure scenarios
- Registration unsuccessful responses
- Periodic heartbeat registration
- gRPC stream reconnection logic

✅ **Command Execution**
- System commands (`echo`, shell commands)
- Built-in system commands (`system:info`, `system:os`)
- Logging commands (`logging:level`, `logging:increase`, `logging:decrease`)
- File commands (`file:get`)
- Invalid/empty command handling

✅ **Error Handling & Edge Cases**
- GetCommands API errors
- SendCommandResult API errors
- Stream receive errors and reconnection
- Context cancellation
- Signal handling concepts

✅ **Configuration & Environment**
- Environment variable parsing
- gRPC connection parameter validation
- Configuration validation and defaults

## CI/CD Integration

For continuous integration, use fast tests by default:
```yaml
# .github/workflows/test.yml
- name: Run fast tests
  run: go test -v ./cmd/minion

- name: Run integration tests (nightly)
  run: SLOW_TESTS=true go test -v ./cmd/minion
  # Only run on nightly builds or releases
```

## Development Workflow

**During development:**
```bash
# Quick feedback loop
go test -v
```

**Before committing:**
```bash
# Full test suite
SLOW_TESTS=true go test -v
```

**Performance debugging:**
```bash
# Profile slow tests
SLOW_TESTS=true go test -v -cpuprofile=cpu.prof -memprofile=mem.prof