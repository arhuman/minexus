
# Default environment for builds
MINEXUS_ENV ?= prod

VERSION=$(shell git describe --tags --always --long --dirty)
COMMIT=$(shell git rev-parse --short HEAD)
BUILD_DATE=$(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Detect host platform
ifeq ($(OS),Windows_NT)
	HOST_OS = windows
else
	HOST_OS = $(shell uname -s | tr '[:upper:]' '[:lower:]')
	ifeq ($(HOST_OS),darwin)
		HOST_OS = darwin
	else ifeq ($(HOST_OS),linux)
		HOST_OS = linux
	else
		HOST_OS = unknown
	endif
endif

# Detect host architecture
ifeq ($(HOST_OS),windows)
	HOST_ARCH = $(if $(findstring AMD64,$(PROCESSOR_ARCHITECTURE)),amd64,$(if $(findstring x86,$(PROCESSOR_ARCHITECTURE)),386,unknown))
else
	HOST_ARCH = $(shell uname -m | sed 's/x86_64/amd64/;s/i[3-6]86/386/;s/aarch64/arm64/;s/armv7l/arm/')
endif

# Build flags for version injection
LDFLAGS=-ldflags "-X github.com/arhuman/minexus/internal/version.Version=$(VERSION) -X github.com/arhuman/minexus/internal/version.GitCommit=$(COMMIT) -X github.com/arhuman/minexus/internal/version.BuildDate=$(BUILD_DATE) -X github.com/arhuman/minexus/internal/version.BuildEnv=$(MINEXUS_ENV)"

PROTO_DIR=proto
OUT_DIR_GO=protogen
PROTOC_GEN_GO=$(shell which protoc-gen-go)
PROTOC_GEN_GO_GRPC=$(shell which protoc-gen-go-grpc)
PROTOC_GEN_JS=$(shell which protoc-gen-grpc-web)

# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## tidy: format code and tidy modfile
.PHONY: tidy
tidy:
	go fmt ./...
	go mod tidy -v

## audit: run quality control checks
.PHONY: audit
audit:
	go mod verify
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest -checks=all,-ST1000,-U1000 ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	MINEXUS_ENV=test go test -race -buildvcs -vet=off ./...

## doc: make documentation
.PHONY: doc
doc:
	swag init --parseDependency --parseInternal --parseDepth 2 -g cmd/nexus/nexus.go

## build: build the binary for current platform (production environment)
.PHONY: build
build: certs-prod
	@echo "Building for detected platform: $(HOST_OS)/$(HOST_ARCH) (production)"
	cp internal/certs/files/prod/*.crt internal/certs/files/
	cp internal/certs/files/prod/*.key internal/certs/files/
	MINEXUS_ENV=prod GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o nexus ./cmd/nexus/
	MINEXUS_ENV=prod GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o minion ./cmd/minion/
	MINEXUS_ENV=prod GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o console ./cmd/console/
	$(MAKE) certs-clean
	@echo "Build complete"

## build_darwin: build binaries for macOS (amd64)
.PHONY: build_darwin
build_darwin:
	@echo "Building for macOS (amd64) (production)..."
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=darwin go build $(LDFLAGS) -o nexus-darwin ./cmd/nexus/
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=darwin go build $(LDFLAGS) -o minion-darwin ./cmd/minion/
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=darwin go build $(LDFLAGS) -o console-darwin ./cmd/console/
	@echo "macOS build complete"

## build_linux: build binaries for Linux (amd64)
.PHONY: build_linux
build_linux:
	@echo "Building for Linux (amd64) (production)..."
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=linux go build $(LDFLAGS) -o nexus-linux ./cmd/nexus/
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=linux go build $(LDFLAGS) -o minion-linux ./cmd/minion/
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=linux go build $(LDFLAGS) -o console-linux ./cmd/console/
	@echo "Linux build complete"

## build_windows: build binaries for Windows (amd64)
.PHONY: build_windows
build_windows:
	@echo "Building for Windows (amd64) (production)..."
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=windows go build $(LDFLAGS) -o nexus-windows.exe ./cmd/nexus/
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=windows go build $(LDFLAGS) -o minion-windows.exe ./cmd/minion/
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=windows go build $(LDFLAGS) -o console-windows.exe ./cmd/console/
	@echo "Windows build complete"

## build_all_platforms: build for all platforms
.PHONY: build_all_platforms
build_all_platforms: build_darwin build_linux build_windows
	@echo "All platform builds complete"

## build_all: build for all platforms (alias for build_all_platforms)
.PHONY: build_all
build_all: build_all_platforms
#  GOARCH=amd64 GOOS=darwin go build -o ${BINARY_NAME}-darwin main.go
#  GOARCH=amd64 GOOS=windows go build -o ${BINARY_NAME}-windows main.go

## build-binaries: build binaries for web server downloads (all platforms and architectures)
.PHONY: build-binaries
build-binaries: certs-prod
	@echo "Building binaries for web server downloads..."
	@mkdir -p binaries/minion binaries/console
	
	# Linux AMD64
	@echo "Building Linux AMD64..."
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=linux go build $(LDFLAGS) -o binaries/minion/linux-amd64 ./cmd/minion/
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=linux go build $(LDFLAGS) -o binaries/console/linux-amd64 ./cmd/console/
	
	# Linux ARM64
	@echo "Building Linux ARM64..."
	MINEXUS_ENV=prod GOARCH=arm64 GOOS=linux go build $(LDFLAGS) -o binaries/minion/linux-arm64 ./cmd/minion/
	MINEXUS_ENV=prod GOARCH=arm64 GOOS=linux go build $(LDFLAGS) -o binaries/console/linux-arm64 ./cmd/console/
	
	# Windows AMD64
	@echo "Building Windows AMD64..."
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=windows go build $(LDFLAGS) -o binaries/minion/windows-amd64.exe ./cmd/minion/
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=windows go build $(LDFLAGS) -o binaries/console/windows-amd64.exe ./cmd/console/
	
	# Windows ARM64
	@echo "Building Windows ARM64..."
	MINEXUS_ENV=prod GOARCH=arm64 GOOS=windows go build $(LDFLAGS) -o binaries/minion/windows-arm64.exe ./cmd/minion/
	MINEXUS_ENV=prod GOARCH=arm64 GOOS=windows go build $(LDFLAGS) -o binaries/console/windows-arm64.exe ./cmd/console/
	
	# macOS AMD64
	@echo "Building macOS AMD64..."
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=darwin go build $(LDFLAGS) -o binaries/minion/darwin-amd64 ./cmd/minion/
	MINEXUS_ENV=prod GOARCH=amd64 GOOS=darwin go build $(LDFLAGS) -o binaries/console/darwin-amd64 ./cmd/console/
	
	# macOS ARM64
	@echo "Building macOS ARM64..."
	MINEXUS_ENV=prod GOARCH=arm64 GOOS=darwin go build $(LDFLAGS) -o binaries/minion/darwin-arm64 ./cmd/minion/
	MINEXUS_ENV=prod GOARCH=arm64 GOOS=darwin go build $(LDFLAGS) -o binaries/console/darwin-arm64 ./cmd/console/
	
	@echo "All platform binaries built successfully in binaries/ directory"

## clean: clean go artefacts (binary included)
clean:
	go clean
	rm -f ${WINDOWS} ${LINUX} ${DARWIN}
	rm -f coverage.out coverage.html
	rm -f minion nexus console
	rm -f minion-* nexus-* console-*
	rm -f *.exe
	rm -rf binaries/
	$(MAKE) certs-clean

## certs-clean: remove copied certificates from root certs directory
.PHONY: certs-clean
certs-clean:
	@echo "Cleaning up certificate files from root certs directory..."
	@rm -f internal/certs/files/*.crt internal/certs/files/*.key internal/certs/files/*.conf internal/certs/files/*.csr internal/certs/files/*.srl

## compose_build: docker-compose build
compose_build:
	docker compose build

## compose_run: stop/build and relaunch docker-compose
compose_run: compose_stop compose_build
	docker compose up -d

## compose_stop: docker-compose down
compose_stop:
	docker compose down


## local: launch docker-compose env
local: compose_run

# ==================================================================================== #
# DOCKER & RUN COMMANDS (Environment-Specific)
# ==================================================================================== #

## build-prod: Build Docker images for the production environment
.PHONY: build-prod
build-prod: certs-prod
	@echo "Building Docker images for PROD environment..."
	MINEXUS_ENV=prod docker compose build

## build-test: Build Docker images for the test environment
.PHONY: build-test
build-test:
	@echo "Building Docker images for TEST environment..."
	MINEXUS_ENV=test docker compose build

## build-prod-local: Build binaries for production environment locally
.PHONY: build-prod-local
build-prod-local: certs-prod
	@echo "Building binaries for PROD environment..."
	MINEXUS_ENV=prod GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o nexus-prod ./cmd/nexus/
	MINEXUS_ENV=prod GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o minion-prod ./cmd/minion/
	MINEXUS_ENV=prod GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o console-prod ./cmd/console/

## build-test-local: Build binaries for test environment locally
.PHONY: build-test-local
build-test-local:
	@echo "Building binaries for TEST environment..."
	MINEXUS_ENV=test GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o nexus-test ./cmd/nexus/
	MINEXUS_ENV=test GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o minion-test ./cmd/minion/
	MINEXUS_ENV=test GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o console-test ./cmd/console/

## run-prod: Run the application in production mode (builds first)
.PHONY: run-prod
run-prod: stop-prod build-prod
	@echo "Starting application in PROD mode..."
	MINEXUS_ENV=prod docker compose up -d

## run-test: Run the application in test mode (builds first)
.PHONY: run-test
run-test: stop-test build-test
	@echo "Starting application in TEST mode..."
	MINEXUS_ENV=test docker compose up -d

## stop-prod: Stop production environment services
.PHONY: stop-prod
stop-prod:
	@echo "Stopping PROD environment..."
	MINEXUS_ENV=prod docker compose down --remove-orphans

## stop-test: Stop test environment services
.PHONY: stop-test
stop-test:
	@echo "Stopping TEST environment..."
	MINEXUS_ENV=test docker compose down --remove-orphans

## logs-prod: Follow logs for the production environment
.PHONY: logs-prod
logs-prod:
	@echo "Following logs for PROD environment..."
	MINEXUS_ENV=prod docker compose logs -f

## logs-test: Follow logs for the test environment
.PHONY: logs-test
logs-test:
	@echo "Following logs for TEST environment..."
	MINEXUS_ENV=test docker compose logs -f

## release: test build and audit current code (includes integration tests)
release:
	SLOW_TESTS=1 $(MAKE) test
	$(MAKE) build
	$(MAKE) audit

## build: buil the binary
run: build
	./${BINARY_NAME}

## certs-prod: generate production certificates if prod directory is empty
.PHONY: certs-prod
certs-prod:
	@if [ ! -f internal/certs/files/prod/ca.crt ]; then \
		echo "Production certificates not found. Generating..."; \
		chmod +x internal/certs/files/mkcerts.sh; \
		export MINEXUS_ENV=prod; \
		set -a; . ./.env.prod; set +a; \
		internal/certs/files/mkcerts.sh $$NEXUS_SERVER "/CN=Minexus CA/O=Minexus" internal/certs/files/prod; \
	else \
		echo "Production certificates already exist."; \
	fi
## test: run tests with coverage (set SLOW_TESTS=1 to include integration tests)
test:
	@echo "Copying test certificates..."
	@cp -R internal/certs/files/test/* internal/certs/files/
	go run honnef.co/go/tools/cmd/staticcheck@latest -checks=all,-ST1000,-U1000 ./...
	@echo "Loading test environment variables..."
	@export MINEXUS_ENV=test && ./run_tests.sh
	@echo "Cleaning up test certificates..."
	@$(MAKE) certs-clean

## cover: run tests with coverage and display detailed results (set SLOW_TESTS=1 to include integration tests)
.PHONY: cover
cover:
	@if [ -n "$$SLOW_TESTS" ]; then \
		echo "Running tests with detailed coverage analysis (including integration tests)..."; \
	else \
		echo "Running tests with detailed coverage analysis (unit tests only)..."; \
	fi
	@MINEXUS_ENV=test SLOW_TESTS=$$SLOW_TESTS ./run_tests.sh
	@if [ -f coverage.out ]; then \
		echo ""; \
		echo "=== DETAILED COVERAGE BY PACKAGE ==="; \
		go tool cover -func=coverage.out | grep -v "total:"; \
		echo ""; \
		TOTAL=$$(go tool cover -func=coverage.out | grep "total:" | awk '{print $$3}'); \
		echo "Total Coverage: $$TOTAL"; \
		if [ -n "$$SLOW_TESTS" ]; then \
			echo "Coverage includes integration tests"; \
		else \
			echo "Coverage for unit tests only (set SLOW_TESTS=1 for complete coverage)"; \
		fi; \
	else \
		echo "Coverage file not generated"; \
	fi

## cover-html: generate and open HTML coverage report
.PHONY: cover-html
cover-html: cover
	@if command -v open >/dev/null 2>&1; then \
		open coverage.html; \
	elif command -v xdg-open >/dev/null 2>&1; then \
		xdg-open coverage.html; \
	else \
		echo "HTML coverage report available at: coverage.html"; \
	fi

## cover-clean: remove coverage files
.PHONY: cover-clean
cover-clean:
	rm -f coverage.out coverage.html

## cover-ci: run coverage for CI/CD (outputs coverage percentage, includes integration tests)
.PHONY: cover-ci
cover-ci:
	@MINEXUS_ENV=test SLOW_TESTS=1 go test -coverprofile=coverage.out ./... -v > /dev/null 2>&1
	@if [ -f coverage.out ]; then \
		TOTAL=$$(go tool cover -func=coverage.out | grep "total:" | awk '{print $$3}'); \
		echo "$$TOTAL"; \
	else \
		echo "0.0%"; \
	fi

## test-integration: run integration tests with Docker services
.PHONY: test-integration
test-integration:
	@echo "Running integration tests with Docker services..."
	@cp -R internal/certs/files/test/* internal/certs/files/
	MINEXUS_ENV=test SLOW_TESTS=1 go test -v ./... -run TestIntegration
	@echo "Cleaning up test certificates..."
	@$(MAKE) certs-clean

## grpc: generate gRPC code
.PHONY: grpc
grpc:
	@echo "Generating Go server code..."

	protoc --proto_path=$(PROTO_DIR) \
		--go_out=$(OUT_DIR_GO) \
		--go-grpc_out=$(OUT_DIR_GO) \
		--go_opt=paths=source_relative \
		--go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/minexus.proto


#	@echo "Generating JavaScript client code..."
#	protoc --proto_path=$(PROTO_DIR) \
#		--js_out=import_style=commonjs:$(OUT_DIR_JS) \
#		--grpc-web_out=import_style=commonjs,mode=grpcwebtext:$(OUT_DIR_JS) \
#		$(PROTO_DIR)/*.proto

	@echo "gRPC code generation complete."

## nexus: build nexus server (production environment)
.PHONY: nexus
nexus:
	MINEXUS_ENV=prod go build $(LDFLAGS) -o nexus ./cmd/nexus/

## minion: build minion client (production environment)
.PHONY: minion
minion:
	MINEXUS_ENV=prod go build $(LDFLAGS) -o minion ./cmd/minion/

## console: build console REPL (production environment)
.PHONY: console
console:
	MINEXUS_ENV=prod go build $(LDFLAGS) -o console ./cmd/console/

## build-all: build all binaries
.PHONY: build-all
build-all: nexus minion console

## help: display this usage
.PHONY: help
help:
	@echo 'Usage:'
	@echo ${MAKEFILE_LIST}
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'
	@echo ''
	@echo 'Testing Commands (run in test environment):'
	@echo '  make test                - Run unit tests with coverage (MINEXUS_ENV=test)'
	@echo '  SLOW_TESTS=1 make test   - Run all tests including integration tests (MINEXUS_ENV=test)'
	@echo '  make cover               - Run tests with detailed coverage analysis (MINEXUS_ENV=test)'
	@echo '  SLOW_TESTS=1 make cover  - Run coverage analysis including integration tests (MINEXUS_ENV=test)'
	@echo ''
	@echo 'Environment-Specific Docker Commands:'
	@echo '  make run-prod            - Build and run production environment'
	@echo '  make run-test            - Build and run test environment'
	@echo '  make build-prod          - Build Docker images for production'
	@echo '  make build-test          - Build Docker images for test'
	@echo '  make stop-prod           - Stop production environment'
	@echo '  make stop-test           - Stop test environment'
	@echo '  make logs-prod           - Follow logs for production environment'
	@echo '  make logs-test           - Follow logs for test environment'
	@echo ''
	@echo 'Certificate Management:'
	@echo '  make certs-prod          - Generate production certificates if missing'
	@echo ''
	@echo 'Binary Distribution:'
	@echo '  make build-binaries      - Build binaries for all platforms for web server downloads'
	@echo ''
	@echo 'Environment Variables:'
	@echo '  SLOW_TESTS=1             - Include integration tests (requires Docker services)'

.PHONY: confirm
confirm:
	@echo -n 'Are you sure? [y/N] ' && read ans && [ $${ans:-N} = y ]

