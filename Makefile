# Default environment for builds
MINEXUS_ENV ?= prod

VERSION=$(shell git describe --tags --always --long --dirty)
COMMIT=$(shell git rev-parse --short HEAD)
BUILD_DATE=$(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Include host detection
include makefiles/host_detection.mk

PROTO_DIR=proto
OUT_DIR_GO=protogen
PROTOC_GEN_GO=$(shell which protoc-gen-go)
PROTOC_GEN_GO_GRPC=$(shell which protoc-gen-go-grpc)
PROTOC_GEN_JS=$(shell which protoc-gen-grpc-web)

# Build flags for version injection
LDFLAGS=-ldflags "-X github.com/arhuman/minexus/internal/version.Version=$(VERSION) -X github.com/arhuman/minexus/internal/version.GitCommit=$(COMMIT) -X github.com/arhuman/minexus/internal/version.BuildDate=$(BUILD_DATE) -X github.com/arhuman/minexus/internal/version.BuildEnv=$(MINEXUS_ENV)"

# Build functions to reduce duplication
define build_binary
	MINEXUS_ENV=$(1) GOARCH=$(2) GOOS=$(3) go build $(LDFLAGS) -o $(4) ./cmd/$(5)/
endef

define build_all_components
	$(call build_binary,$(1),$(2),$(3),nexus$(4),nexus)
	$(call build_binary,$(1),$(2),$(3),minion$(4),minion)
	$(call build_binary,$(1),$(2),$(3),console$(4),console)
endef

define build_platform
	@echo "Building $(1) $(2)..."
	$(call build_all_components,prod,$(2),$(1),$(3))
	$(if $(4),$(call build_binary,prod,$(2),$(1),binaries/minion/$(1)-$(2)$(5),minion))
	$(if $(4),$(call build_binary,prod,$(2),$(1),binaries/console/$(1)-$(2)$(5),console))
endef

# Phony targets
.PHONY: audit build build_all build_all_platforms build-binaries build-prod-local build-test
.PHONY: certs-clean certs-prod clean compose-build compose-run compose-stop console cover cover-ci cover-clean cover-html
.PHONY: doc grpc help local logs-docker minion nexus release test test-integration tidy confirm

# ==================================================================================== #
# Makefile Targets (alphabetical order, build first as default)
# ==================================================================================== #

# First target is default target (called also with single `make`)
## build: build binaries for current platform (production environment) with optional suffixes
build: certs-prod
	@echo "Building for detected platform: $(HOST_OS)/$(HOST_ARCH) (production)"
	cp internal/certs/files/prod/*.crt internal/certs/files/
	cp internal/certs/files/prod/*.key internal/certs/files/
	$(call build_all_components,prod,$(HOST_ARCH),$(HOST_OS),)
	$(MAKE) certs-clean
	@echo "Build complete"

## audit: run quality control checks
audit:
	go mod verify
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest -checks=all,-ST1000,-U1000 ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	MINEXUS_ENV=test go test -race -buildvcs -vet=off ./...

## build_all: alias for build_all_platforms
build_all: build_all_platforms

## build_all_platforms: build for all platforms and architectures (includes web distribution binaries)
build_all_platforms: certs-prod
	@echo "Building for all platforms and architectures..."
	@mkdir -p binaries/minion binaries/console
	
	# Linux builds
	$(call build_platform,linux,amd64,-linux,yes,)
	$(call build_platform,linux,arm64,,yes,)
	
	# Windows builds  
	$(call build_platform,windows,amd64,-windows.exe,yes,.exe)
	$(call build_platform,windows,arm64,,yes,.exe)
	
	# macOS builds
	$(call build_platform,darwin,amd64,-darwin,yes,)
	$(call build_platform,darwin,arm64,,yes,)
	
	$(MAKE) certs-clean
	@echo "All platform builds complete (traditional binaries + web distribution binaries in binaries/ directory)"

## build-binaries: alias for build_all_platforms (web distribution binaries)
build-binaries: build_all_platforms

## compose-build: Build Docker images for specified environment (default: test)
compose-build:
	@ENV=$${MINEXUS_ENV:-test}; \
	echo "Building Docker images for $$ENV environment..."; \
	[ "$$ENV" = "prod" ] && $(MAKE) certs-prod || true; \
	MINEXUS_ENV=$$ENV docker compose build

## build-prod-local: alias for build (production binaries with -prod suffix included)
build-prod-local: build

## build-test: Build binaries for test environment
build-test:
	@echo "Building binaries for TEST environment..."
	$(call build_all_components,test,$(HOST_ARCH),$(HOST_OS),-test)

## certs-clean: remove copied certificates from root certs directory
certs-clean:
	@rm -f internal/certs/files/*.{crt,key,conf,csr,srl}

## certs-prod: generate production certificates if needed
certs-prod:
	@[ -f internal/certs/files/prod/ca.crt ] || { \
		echo "Generating production certificates..."; \
		chmod +x internal/certs/files/mkcerts.sh; \
		set -a; . ./.env.prod; set +a; \
		MINEXUS_ENV=prod internal/certs/files/mkcerts.sh $$NEXUS_SERVER "/CN=Minexus CA/O=Minexus" internal/certs/files/prod; \
	}

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


## console: build console REPL (production environment)
console:
	$(call build_binary,prod,$(HOST_ARCH),$(HOST_OS),console,console)

## cover: run tests with coverage and display detailed results (set SLOW_TESTS=1 to include integration tests)
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

## cover-ci: run coverage for CI/CD (outputs coverage percentage, includes integration tests)
cover-ci:
	@MINEXUS_ENV=test SLOW_TESTS=1 go test -coverprofile=coverage.out ./... -v > /dev/null 2>&1
	@if [ -f coverage.out ]; then \
		TOTAL=$$(go tool cover -func=coverage.out | grep "total:" | awk '{print $$3}'); \
		echo "$$TOTAL"; \
	else \
		echo "0.0%"; \
	fi

## cover-clean: remove coverage files
cover-clean:
	rm -f coverage.out coverage.html

## cover-html: generate and open HTML coverage report
cover-html: cover
	@if command -v open >/dev/null 2>&1; then \
		open coverage.html; \
	elif command -v xdg-open >/dev/null 2>&1; then \
		xdg-open coverage.html; \
	else \
		echo "HTML coverage report available at: coverage.html"; \
	fi

## doc: make documentation
doc:
	swag init --parseDependency --parseInternal --parseDepth 2 -g cmd/nexus/nexus.go

## grpc: generate gRPC code
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

## help: display this usage
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
	@echo 'Docker Commands (use MINEXUS_ENV=prod for production):'
	@echo '  make compose-run         - Build and run in specified environment'
	@echo '  make compose-build       - Build Docker images for environment'
	@echo '  make compose-stop        - Stop services for environment'
	@echo '  make local               - Quick start in test environment'
	@echo '  make logs-docker         - Follow logs for environment'
	@echo ''
	@echo 'Certificate Management:'
	@echo '  make certs-prod          - Generate production certificates if missing'
	@echo ''
	@echo 'Binary Distribution:'
	@echo '  make build_all_platforms - Build for all platforms and architectures'
	@echo '  make build-binaries      - Alias for build_all_platforms'
	@echo ''
	@echo 'Environment Variables:'
	@echo '  SLOW_TESTS=1             - Include integration tests (requires Docker services)'

## local: Run application in test environment (alias for compose-run)
local: compose-run

## logs-docker: Follow logs for specified environment (default: test)
logs-docker:
	@ENV=$${MINEXUS_ENV:-test}; \
	echo "Following logs for $$ENV environment..."; \
	MINEXUS_ENV=$$ENV docker compose logs -f

## minion: build minion client (production environment)
minion:
	$(call build_binary,prod,$(HOST_ARCH),$(HOST_OS),minion,minion)

## nexus: build nexus server (production environment)
nexus:
	$(call build_binary,prod,$(HOST_ARCH),$(HOST_OS),nexus,nexus)

## release: test build and audit current code (includes integration tests)
release:
	SLOW_TESTS=1 $(MAKE) test
	$(MAKE) build
	$(MAKE) audit

## compose-run: Run application in specified environment (default: test)
compose-run:
	@ENV=$${MINEXUS_ENV:-test}; \
	echo "Starting application in $$ENV mode..."; \
	$(MAKE) compose-stop MINEXUS_ENV=$$ENV; \
	$(MAKE) compose-build MINEXUS_ENV=$$ENV; \
	MINEXUS_ENV=$$ENV docker compose up -d

## compose-stop: Stop services for specified environment (default: test)
compose-stop:
	@ENV=$${MINEXUS_ENV:-test}; \
	echo "Stopping $$ENV environment..."; \
	MINEXUS_ENV=$$ENV docker compose down --remove-orphans

## test: run tests with coverage (set SLOW_TESTS=1 to include integration tests)
test:
	@echo "Copying test certificates..."
	@cp -R internal/certs/files/test/* internal/certs/files/
	go run honnef.co/go/tools/cmd/staticcheck@latest -checks=all,-ST1000,-U1000 ./...
	@echo "Loading test environment variables..."
	@export MINEXUS_ENV=test && ./run_tests.sh
	@echo "Cleaning up test certificates..."
	@$(MAKE) certs-clean

## test-integration: run integration tests with Docker services
test-integration:
	@echo "Running integration tests with Docker services..."
	@cp -R internal/certs/files/test/* internal/certs/files/
	MINEXUS_ENV=test SLOW_TESTS=1 go test -v ./... -run TestIntegration
	@echo "Cleaning up test certificates..."
	@$(MAKE) certs-clean

## tidy: format code and tidy modfile
tidy:
	go fmt ./...
	go mod tidy -v

confirm:
	@echo -n 'Are you sure? [y/N] ' && read ans && [ $${ans:-N} = y ]