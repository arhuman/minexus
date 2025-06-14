// Package logging provides standardized logging utilities for the Minexus system.
package logging

import (
	"time"

	"go.uber.org/zap"
)

// FuncLogger returns a logger with the function name as a field and the current time
// to measure elapsed time for the function execution
func FuncLogger(logger *zap.Logger, funcName string) (*zap.Logger, time.Time) {
	logger = logger.With(zap.String("location", funcName))
	logger.Debug(funcName+" started", zap.Time("start_time", time.Now()))
	return logger, time.Now()
}

// FuncExit logs the exit point of a function with elapsed time at debug level
func FuncExit(logger *zap.Logger, start time.Time) {
	logger.With(zap.Duration("elapsed", time.Since(start))).Debug("function completed")
}
