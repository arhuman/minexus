#!/bin/bash

# run_tests.sh - Enhanced test runner with coverage collection and robust Docker service management
# Supports both unit tests and integration tests based on SLOW_TESTS environment variable

set -e

# Configuration
COVERAGE_FILE="coverage.out"
COVERAGE_HTML="coverage.html"
DOCKER_COMPOSE_FILE="docker-compose.yml"
MAX_RETRIES=60
RETRY_INTERVAL=2
NEXUS_MINION_PORT=${NEXUS_MINION_PORT:-11972}
DB_PORT=5432

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if a port is open
check_port() {
    local host=$1
    local port=$2
    local timeout=${3:-2}
    
    # Use netcat with built-in timeout for macOS compatibility
    if nc -z -w "$timeout" "$host" "$port" 2>/dev/null; then
        return 0
    else
        return 1
    fi
}

# Wait for service to be ready
wait_for_service() {
    local service_name=$1
    local host=$2
    local port=$3
    local max_attempts=${4:-$MAX_RETRIES}
    
    log_info "Waiting for $service_name to be ready on $host:$port..."
    
    for ((i=1; i<=max_attempts; i++)); do
        if check_port "$host" "$port"; then
            log_info "$service_name is ready (attempt $i/$max_attempts)"
            return 0
        fi
        
        if [ $i -eq $max_attempts ]; then
            log_error "$service_name failed to start within $((max_attempts * RETRY_INTERVAL)) seconds"
            return 1
        fi
        
        log_info "Waiting for $service_name... (attempt $i/$max_attempts)"
        sleep $RETRY_INTERVAL
    done
}

# Check Docker Compose service status
check_service_status() {
    local service_name=$1
    
    if docker compose ps --format json | grep -q "\"Service\":\"$service_name\".*\"State\":\"running\""; then
        return 0
    else
        return 1
    fi
}

# Setup Docker services with proper dependency management
setup_docker_services() {
    log_info "Setting up Docker services for integration tests..."
    
    # Check if services are already running
    local services_to_start=()
    
    if ! check_service_status "nexus_db"; then
        services_to_start+=("nexus_db")
    fi
    
    if ! check_service_status "nexus_server"; then
        services_to_start+=("nexus")
    fi
    
    if ! check_service_status "minion_1"; then
        services_to_start+=("minion")
    fi
    
    if [ ${#services_to_start[@]} -gt 0 ]; then
        log_info "Starting services: ${services_to_start[*]}"
        
        # Start database first if needed
        if [[ " ${services_to_start[*]} " =~ " nexus_db " ]]; then
            log_info "Starting database service..."
            docker compose up -d nexus_db
            wait_for_service "PostgreSQL Database" "localhost" "$DB_PORT" 30
        fi
        
        # Start nexus server if needed
        if [[ " ${services_to_start[*]} " =~ " nexus " ]]; then
            log_info "Starting Nexus server..."
            docker compose up -d nexus
            wait_for_service "Nexus Server" "localhost" "$NEXUS_MINION_PORT" 45
        fi
        
        # Start minion if needed
        if [[ " ${services_to_start[*]} " =~ " minion " ]]; then
            log_info "Starting Minion service..."
            docker compose up -d minion
            sleep 5  # Give minion time to register
        fi
    else
        log_info "All required services are already running"
        
        # Still verify they're accessible
        wait_for_service "PostgreSQL Database" "localhost" "$DB_PORT" 10
        wait_for_service "Nexus Server" "localhost" "$NEXUS_MINION_PORT" 10
    fi
}

# Build console executable with proper error handling
build_console() {
    log_info "Building console executable..."
    
    if [ ! -f ./console ] || [ cmd/console/console.go -nt ./console ]; then
        if go build -o console ./cmd/console; then
            log_info "Console built successfully"
        else
            log_error "Failed to build console"
            return 1
        fi
    else
        log_info "Console executable is up to date"
    fi
}

# Check if integration tests should be included
if [ -n "$SLOW_TESTS" ]; then
    log_info "Running all tests including integration tests (SLOW_TESTS is set)..."
    
    # Verify Docker is running
    if ! docker info >/dev/null 2>&1; then
        log_error "Docker is not running or not accessible"
        exit 1
    fi
    
    # Setup services and build console
    setup_docker_services
    build_console
    
    # Final verification
    log_info "Running final verification..."
    wait_for_service "PostgreSQL Database" "localhost" "$DB_PORT" 5
    wait_for_service "Nexus Server" "localhost" "$NEXUS_MINION_PORT" 5
    
    log_info "Test environment setup completed successfully!"
else
    log_info "Running unit tests only (set SLOW_TESTS=1 to include integration tests)..."
fi

# Run tests with coverage
go test -coverprofile=$COVERAGE_FILE ./... -v

# Check if coverage file was generated
if [ -f "$COVERAGE_FILE" ]; then
    echo ""
    echo "Coverage results:"
    echo "================"
    
    # Show coverage summary
    go tool cover -func=$COVERAGE_FILE
    
    # Generate HTML coverage report
    go tool cover -html=$COVERAGE_FILE -o $COVERAGE_HTML
    
    echo ""
    echo "Coverage report generated: $COVERAGE_HTML"
    echo "Coverage data saved to: $COVERAGE_FILE"
    
    # Calculate total coverage percentage
    TOTAL_COVERAGE=$(go tool cover -func=$COVERAGE_FILE | grep "total:" | awk '{print $3}')
    if [ -n "$TOTAL_COVERAGE" ]; then
        echo "Total coverage: $TOTAL_COVERAGE"
    fi
    
    # Show test summary
    if [ -n "$SLOW_TESTS" ]; then
        echo ""
        echo "✓ Unit tests and integration tests completed"
        echo "  Run 'make test' for unit tests only"
        echo "  Run 'SLOW_TESTS=1 make test' for all tests"
    else
        echo ""
        echo "✓ Unit tests completed"
        echo "  Run 'SLOW_TESTS=1 make test' to include integration tests"
    fi
else
    echo "Warning: Coverage file not generated"
fi
