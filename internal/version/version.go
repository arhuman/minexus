package version

import (
	"fmt"
	"os"
	"runtime"
)

var (
	// Version is the application version - set by build flags
	Version = "test"
	// GitCommit is the git commit hash - set by build flags
	GitCommit = "unknown"
	// BuildDate is the build date - set by build flags
	BuildDate = "unknown"
	// BuildEnv is the environment this binary was built for - set by build flags
	BuildEnv = "unknown"
)

// Info returns detailed version information
func Info() string {
	return fmt.Sprintf("Version: %s, Commit: %s, Built: %s, BuildEnv: %s, Go: %s",
		Version, GitCommit, BuildDate, BuildEnv, runtime.Version())
}

// Short returns a short version string
func Short() string {
	return Version
}

// Component returns version info for a specific component
func Component(componentName string) string {
	return fmt.Sprintf("%s %s (commit: %s, built: %s, env: %s)",
		componentName, Version, GitCommit, BuildDate, BuildEnv)
}

// Environment returns the build environment
func Environment() string {
	if BuildEnv == "unknown" {
		return "test" // Default fallback
	}
	return BuildEnv
}

// EnvironmentInfo returns detailed environment information including runtime detection
func EnvironmentInfo() string {
	buildEnv := Environment()
	runtimeEnv := "unknown"

	// Try to detect runtime environment from MINEXUS_ENV
	if envVar := os.Getenv("MINEXUS_ENV"); envVar != "" {
		runtimeEnv = envVar
	}

	if buildEnv == runtimeEnv {
		return fmt.Sprintf("Environment: %s (build matches runtime)", buildEnv)
	}

	return fmt.Sprintf("Environment: build=%s, runtime=%s", buildEnv, runtimeEnv)
}
