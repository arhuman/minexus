
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
LDFLAGS=-ldflags "-X minexus/internal/version.Version=$(VERSION) -X minexus/internal/version.GitCommit=$(COMMIT) -X minexus/internal/version.BuildDate=$(BUILD_DATE)"

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
	go test -race -buildvcs -vet=off ./...

## doc: make documentation
.PHONY: doc
doc:
	swag init --parseDependency --parseInternal --parseDepth 2 -g cmd/nexus/nexus.go

## build: build the binary for current platform
.PHONY: build
build:
	@echo "Building for detected platform: $(HOST_OS)/$(HOST_ARCH)"
	GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o nexus ./cmd/nexus/
	GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o minion ./cmd/minion/
	GOARCH=$(HOST_ARCH) GOOS=$(HOST_OS) go build $(LDFLAGS) -o console ./cmd/console/
	@echo "Build complete"

## build_darwin: build binaries for macOS (amd64)
.PHONY: build_darwin
build_darwin:
	@echo "Building for macOS (amd64)..."
	GOARCH=amd64 GOOS=darwin go build $(LDFLAGS) -o nexus-darwin ./cmd/nexus/
	GOARCH=amd64 GOOS=darwin go build $(LDFLAGS) -o minion-darwin ./cmd/minion/
	GOARCH=amd64 GOOS=darwin go build $(LDFLAGS) -o console-darwin ./cmd/console/
	@echo "macOS build complete"

## build_linux: build binaries for Linux (amd64)
.PHONY: build_linux
build_linux:
	@echo "Building for Linux (amd64)..."
	GOARCH=amd64 GOOS=linux go build $(LDFLAGS) -o nexus-linux ./cmd/nexus/
	GOARCH=amd64 GOOS=linux go build $(LDFLAGS) -o minion-linux ./cmd/minion/
	GOARCH=amd64 GOOS=linux go build $(LDFLAGS) -o console-linux ./cmd/console/
	@echo "Linux build complete"

## build_windows: build binaries for Windows (amd64)
.PHONY: build_windows
build_windows:
	@echo "Building for Windows (amd64)..."
	GOARCH=amd64 GOOS=windows go build $(LDFLAGS) -o nexus-windows.exe ./cmd/nexus/
	GOARCH=amd64 GOOS=windows go build $(LDFLAGS) -o minion-windows.exe ./cmd/minion/
	GOARCH=amd64 GOOS=windows go build $(LDFLAGS) -o console-windows.exe ./cmd/console/
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

## clean: clean go artefacts (binary included)
clean:
	go clean
	rm -f ${WINDOWS} ${LINUX} ${DARWIN}
	rm -f coverage.out coverage.html
	rm -f minion nexus console
	rm -f minion-* nexus-* console-*
	rm -f *.exe

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

## release: test build and audit current code (includes integration tests)
release:
	SLOW_TESTS=1 $(MAKE) test
	$(MAKE) build
	$(MAKE) audit

## build: buil the binary
run: build
	./${BINARY_NAME}

## test: run tests with coverage (set SLOW_TESTS=1 to include integration tests)
test:
	@echo "Copying test certificates..."
	@cp -R internal/certs/files/test/* internal/certs/files/
	go run honnef.co/go/tools/cmd/staticcheck@latest -checks=all,-ST1000,-U1000 ./...
	./run_tests.sh

## cover: run tests with coverage and display detailed results (set SLOW_TESTS=1 to include integration tests)
.PHONY: cover
cover:
	@if [ -n "$$SLOW_TESTS" ]; then \
		echo "Running tests with detailed coverage analysis (including integration tests)..."; \
	else \
		echo "Running tests with detailed coverage analysis (unit tests only)..."; \
	fi
	@SLOW_TESTS=$$SLOW_TESTS ./run_tests.sh
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
	@SLOW_TESTS=1 go test -coverprofile=coverage.out ./... -v > /dev/null 2>&1
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
	SLOW_TESTS=1 go test -v ./... -run TestIntegration

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

## nexus: build nexus server
.PHONY: nexus
nexus:
	go build $(LDFLAGS) -o nexus ./cmd/nexus/

## minion: build minion client
.PHONY: minion
minion:
	go build $(LDFLAGS) -o minion ./cmd/minion/

## console: build console REPL
.PHONY: console
console:
	go build $(LDFLAGS) -o console ./cmd/console/

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
	@echo 'Testing Commands:'
	@echo '  make test                - Run unit tests with coverage'
	@echo '  SLOW_TESTS=1 make test   - Run all tests including integration tests'
	@echo '  make cover               - Run tests with detailed coverage analysis'
	@echo '  SLOW_TESTS=1 make cover  - Run coverage analysis including integration tests'
	@echo ''
	@echo 'Environment Variables:'
	@echo '  SLOW_TESTS=1             - Include integration tests (requires Docker services)'

.PHONY: confirm
confirm:
	@echo -n 'Are you sure? [y/N] ' && read ans && [ $${ans:-N} = y ]

