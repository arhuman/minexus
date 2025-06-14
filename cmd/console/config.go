package main

import (
	"minexus/internal/config"
)

// LoadConsoleConfig loads console configuration using the unified config system
func LoadConsoleConfig() (*config.ConsoleConfig, error) {
	return config.LoadConsoleConfig()
}
