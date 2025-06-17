package version

import (
	"fmt"
	"runtime"
)

var (
	// Version is the application version - set by build flags
	Version = "dev"
	// GitCommit is the git commit hash - set by build flags
	GitCommit = "unknown"
	// BuildDate is the build date - set by build flags
	BuildDate = "unknown"
)

// Info returns detailed version information
func Info() string {
	return fmt.Sprintf("Version: %s, Commit: %s, Built: %s, Go: %s",
		Version, GitCommit, BuildDate, runtime.Version())
}

// Short returns a short version string
func Short() string {
	return Version
}

// Component returns version info for a specific component
func Component(componentName string) string {
	return fmt.Sprintf("%s %s (commit: %s, built: %s)",
		componentName, Version, GitCommit, BuildDate)
}
