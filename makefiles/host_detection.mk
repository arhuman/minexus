# Host detection functions and variables
# This file detects the current host platform and architecture

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