# Version Handling in Minexus

This document explains how versioning works in the Minexus system, including how versions are determined, how to query version information, and how to set custom versions during builds.

## Overview

Minexus uses a build-time version injection system that embeds version information directly into the compiled binaries. The version information includes:

- **Version**: Git tag/commit version (e.g., `v1.2.3`, `dev`)
- **Git Commit**: Short commit hash (e.g., `abc1234`)
- **Build Date**: When the binary was compiled (e.g., `2024-01-01_12:00:00`)
- **Go Version**: Go runtime version used for compilation

## Version Package

The version handling is centralized in [`internal/version/version.go`](../internal/version/version.go):

```go
var (
    // Version is the application version - set by build flags
    Version = "dev"
    // GitCommit is the git commit hash - set by build flags
    GitCommit = "unknown"
    // BuildDate is the build date - set by build flags
    BuildDate = "unknown"
)
```

### Available Functions

- **[`version.Info()`](../internal/version/version.go:18)**: Returns detailed version information including Go runtime version
- **[`version.Short()`](../internal/version/version.go:24)**: Returns just the version string
- **[`version.Component(componentName)`](../internal/version/version.go:29)**: Returns formatted version info for a specific component

## Querying Version Information

### Command Line Version Check

All components support version flags:

```bash
# Display version information for any component
./nexus --version     # or -v
./minion --version    # or -v
./console --version   # or -v
```

**Example output:**
```
Nexus Version: v1.2.3, Commit: abc1234, Built: 2024-01-01_12:00:00, Go: go1.23.1
```

### Console Interactive Version Command

Within the console REPL interface:

```
minexus> version     # or v
Console Version: v1.2.3, Commit: abc1234, Built: 2024-01-01_12:00:00, Go: go1.23.1
```

### Startup Version Display

Each component automatically displays its version when starting:

```
[NEXUS] Starting Nexus v1.2.3 (commit: abc1234, built: 2024-01-01_12:00:00)
[MINION] Starting Minion v1.2.3 (commit: abc1234, built: 2024-01-01_12:00:00)
[CONSOLE] Starting Console v1.2.3 (commit: abc1234, built: 2024-01-01_12:00:00)
```

## Build-Time Version Injection

### How It Works

Version information is injected at build time using Go's `-ldflags` with `-X` to set package variables:

```bash
LDFLAGS=-ldflags "-X minexus/internal/version.Version=$(VERSION) \
                  -X minexus/internal/version.GitCommit=$(COMMIT) \
                  -X minexus/internal/version.BuildDate=$(BUILD_DATE)"
```

### Version Detection Logic

The [`Makefile`](../Makefile:2-4) automatically determines version information:

```makefile
VERSION=$(shell git describe --tags --always --long --dirty)
COMMIT=$(shell git rev-parse --short HEAD)
BUILD_DATE=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
```

### Version Sources

1. **Git Tags**: If you have git tags, version will be based on the latest tag
2. **Git Commits**: If no tags exist, uses the commit hash
3. **Dirty State**: Appends `-dirty` if there are uncommitted changes
4. **No Git**: Falls back to `"dev"` if not in a git repository

## Setting and Changing Versions

### 1. Using Git Tags (Recommended)

Create a git tag to set a specific version:

```bash
# Create and push a version tag
git tag v1.2.3
git push origin v1.2.3

# Build with the tagged version
make build
```

**Result**: All binaries will show version `v1.2.3`

### 2. Manual Version Override

Override the version during build:

```bash
# Set custom version
make build VERSION=custom-version

# Or set all version components
go build -ldflags "-X minexus/internal/version.Version=v2.0.0 \
                   -X minexus/internal/version.GitCommit=manual \
                   -X minexus/internal/version.BuildDate=$(date -u '+%Y-%m-%d_%H:%M:%S')" \
         -o nexus ./cmd/nexus/
```

### 3. Environment Variable Override

Set version through environment variables before building:

```bash
export CUSTOM_VERSION=v1.5.0
make build VERSION=$CUSTOM_VERSION
```

### 4. CI/CD Pipeline Versioning

For automated builds, you can inject version information:

```bash
# In CI/CD pipeline
VERSION=$(git describe --tags --always --long --dirty)
COMMIT=$(git rev-parse --short HEAD)
BUILD_DATE=$(date -u '+%Y-%m-%d_%H:%M:%S')

go build -ldflags "-X minexus/internal/version.Version=$VERSION \
                   -X minexus/internal/version.GitCommit=$COMMIT \
                   -X minexus/internal/version.BuildDate=$BUILD_DATE" \
         -o nexus ./cmd/nexus/
```

## Version Scenarios

### Development Builds

When building without git tags or in a dirty repository:

```bash
git describe --tags --always --long --dirty
# Output: abc1234-dirty  (if no tags exist and repo is dirty)
```

**Binary version**: `abc1234-dirty`

### Release Builds

When building from a tagged commit:

```bash
git tag v1.0.0
git describe --tags --always --long --dirty
# Output: v1.0.0
```

**Binary version**: `v1.0.0`

### Pre-release Builds

When building from commits after a tag:

```bash
git describe --tags --always --long --dirty
# Output: v1.0.0-5-gabc1234  (5 commits after v1.0.0 tag)
```

**Binary version**: `v1.0.0-5-gabc1234`

## Build Targets and Version Injection

### Using Makefile Targets

All build targets automatically inject version information:

```bash
# Build all components with version info
make build-all

# Build individual components
make nexus    # Builds nexus with version info
make minion   # Builds minion with version info
make console  # Builds console with version info
```

### Manual Builds

For manual builds, use the defined LDFLAGS:

```bash
# Use the Makefile's LDFLAGS
make nexus

# Or manually with version injection
go build -ldflags "-X minexus/internal/version.Version=v1.0.0 \
                   -X minexus/internal/version.GitCommit=abc1234 \
                   -X minexus/internal/version.BuildDate=2024-01-01_12:00:00" \
         -o nexus ./cmd/nexus/
```

### Builds Without Version Injection

If you build without version flags, defaults are used:

```bash
go build -o nexus ./cmd/nexus/
./nexus --version
# Output: Nexus Version: dev, Commit: unknown, Built: unknown, Go: go1.23.1
```

## Best Practices

### 1. Use Semantic Versioning

Follow semantic versioning for git tags:

```bash
git tag v1.0.0    # Major release
git tag v1.1.0    # Minor release  
git tag v1.1.1    # Patch release
```

### 2. Tag Release Commits

Always tag release commits:

```bash
# Make release commit
git commit -m "Release v1.2.3"

# Tag the release
git tag v1.2.3

# Push commit and tag
git push origin main v1.2.3
```

### 3. Clean Releases

Ensure clean working directory for releases:

```bash
# Check for uncommitted changes
git status

# Clean build for release
make clean
make build
```

### 4. Verify Version Information

Always verify version information after building:

```bash
./nexus --version
./minion --version
./console --version
```

## Integration with CI/CD

### GitHub Actions Example

```yaml
name: Build and Release
on:
  push:
    tags: ['v*']

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
        with:
          fetch-depth: 0  # Fetch all history for git describe
      
      - name: Build with version
        run: |
          VERSION=$(git describe --tags --always --long --dirty)
          echo "Building version: $VERSION"
          make build
          
      - name: Verify version
        run: |
          ./nexus --version
          ./minion --version
          ./console --version
```

### Docker Builds

```dockerfile
# Build stage
FROM golang:1.23.1-alpine AS builder

WORKDIR /app
COPY . .

# Build with version injection
RUN VERSION=$(git describe --tags --always --long --dirty) && \
    COMMIT=$(git rev-parse --short HEAD) && \
    BUILD_DATE=$(date -u '+%Y-%m-%d_%H:%M:%S') && \
    go build -ldflags "-X minexus/internal/version.Version=$VERSION \
                       -X minexus/internal/version.GitCommit=$COMMIT \
                       -X minexus/internal/version.BuildDate=$BUILD_DATE" \
             -o nexus ./cmd/nexus/
```

## Troubleshooting

### Version Shows as "dev"

**Cause**: No git repository or tags found
**Solution**: 
```bash
git init
git add .
git commit -m "Initial commit"
git tag v1.0.0
make build
```

### Version Shows as "unknown"

**Cause**: Built without version injection
**Solution**: Use make targets or manual LDFLAGS:
```bash
make build  # Uses automatic version injection
```

### Dirty Version Suffix

**Cause**: Uncommitted changes in repository
**Solution**: Commit or stash changes:
```bash
git status
git add .
git commit -m "Clean up for release"
make build
```

### Cannot Find Git

**Cause**: Git not available during build
**Solution**: Ensure git is installed or set version manually:
```bash
# Manual version setting
make build VERSION=v1.0.0
```

## API Integration

You can also access version information programmatically:

```go
import "minexus/internal/version"

// Get full version info
info := version.Info()
fmt.Println(info)

// Get just version
ver := version.Short()
fmt.Println(ver)

// Get component-specific info
component := version.Component("MyComponent")
fmt.Println(component)
```

This enables version checking in application logic, logging, or monitoring systems.