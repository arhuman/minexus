package minion

import (
	"math/rand"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/arhuman/minexus/internal/logging"
)

// ReconnectionManager handles exponential backoff reconnection strategy
type ReconnectionManager struct {
	mu                sync.Mutex
	initialDelay      time.Duration
	maxDelay          time.Duration
	currentDelay      time.Duration
	attemptCount      int
	logger            *zap.Logger
	jitterEnabled     bool
	backoffMultiplier float64
}

// NewReconnectionManager creates a new reconnection manager with exponential backoff
func NewReconnectionManager(initialDelay, maxDelay time.Duration, logger *zap.Logger) *ReconnectionManager {
	logger, start := logging.FuncLogger(logger, "NewReconnectionManager")
	defer logging.FuncExit(logger, start)

	return &ReconnectionManager{
		initialDelay:      initialDelay,
		maxDelay:          maxDelay,
		currentDelay:      initialDelay,
		attemptCount:      0,
		logger:            logger,
		jitterEnabled:     true,
		backoffMultiplier: 2.0, // Double the delay each time
	}
}

// GetNextDelay calculates and returns the next reconnection delay
// It implements exponential backoff with optional jitter
func (rm *ReconnectionManager) GetNextDelay() time.Duration {
	logger, start := logging.FuncLogger(rm.logger, "ReconnectionManager.GetNextDelay")
	defer logging.FuncExit(logger, start)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// First attempt should use initial delay
	if rm.attemptCount == 0 {
		rm.attemptCount++
		finalDelay := rm.currentDelay
		if rm.jitterEnabled {
			finalDelay = rm.addJitter(rm.currentDelay)
		}
		logger.Debug("First reconnection attempt",
			zap.Duration("base_delay", rm.currentDelay),
			zap.Duration("final_delay", finalDelay),
			zap.Int("attempt", rm.attemptCount),
			zap.Bool("jitter_enabled", rm.jitterEnabled))
		return finalDelay
	}

	// Calculate exponential backoff
	newDelay := time.Duration(float64(rm.currentDelay) * rm.backoffMultiplier)

	// Cap at maximum delay
	if newDelay > rm.maxDelay {
		newDelay = rm.maxDelay
	}

	rm.currentDelay = newDelay
	rm.attemptCount++

	// Add jitter to prevent thundering herd problem
	finalDelay := rm.currentDelay
	if rm.jitterEnabled {
		finalDelay = rm.addJitter(rm.currentDelay)
	}

	logger.Debug("Calculated next reconnection delay",
		zap.Duration("base_delay", rm.currentDelay),
		zap.Duration("final_delay", finalDelay),
		zap.Int("attempt", rm.attemptCount),
		zap.Bool("jitter_enabled", rm.jitterEnabled),
		zap.Bool("at_max_delay", rm.currentDelay >= rm.maxDelay))

	return finalDelay
}

// ResetDelay resets the reconnection delay back to initial value
// This should be called upon successful reconnection
func (rm *ReconnectionManager) ResetDelay() {
	logger, start := logging.FuncLogger(rm.logger, "ReconnectionManager.ResetDelay")
	defer logging.FuncExit(logger, start)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	logger.Debug("Resetting reconnection delay to initial value",
		zap.Duration("previous_delay", rm.currentDelay),
		zap.Duration("initial_delay", rm.initialDelay),
		zap.Int("previous_attempts", rm.attemptCount))

	rm.currentDelay = rm.initialDelay
	rm.attemptCount = 0
}

// GetCurrentDelay returns the current delay without incrementing
func (rm *ReconnectionManager) GetCurrentDelay() time.Duration {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.currentDelay
}

// GetAttemptCount returns the current number of reconnection attempts
func (rm *ReconnectionManager) GetAttemptCount() int {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.attemptCount
}

// SetJitterEnabled enables or disables jitter
func (rm *ReconnectionManager) SetJitterEnabled(enabled bool) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.jitterEnabled = enabled
}

// SetBackoffMultiplier sets the multiplier for exponential backoff
func (rm *ReconnectionManager) SetBackoffMultiplier(multiplier float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if multiplier > 1.0 {
		rm.backoffMultiplier = multiplier
	}
}

// addJitter adds random jitter to the delay to prevent thundering herd problems
// Uses full jitter approach: delay = random(0, delay)
func (rm *ReconnectionManager) addJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		// For zero or negative delays, apply minimum jitter
		minDelay := 100 * time.Millisecond
		return minDelay
	}

	// Use full jitter: pick a random value between 0 and delay
	jitterDelay := time.Duration(rand.Int63n(int64(delay)))

	// Ensure minimum delay of at least 100ms to avoid too frequent retries
	// Apply minimum regardless of original delay size when jitter is enabled
	minDelay := 100 * time.Millisecond
	if jitterDelay < minDelay {
		jitterDelay = minDelay
	}

	return jitterDelay
}

// IsAtMaxDelay returns true if the current delay has reached the maximum
func (rm *ReconnectionManager) IsAtMaxDelay() bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.currentDelay >= rm.maxDelay
}

// GetStats returns current reconnection statistics
func (rm *ReconnectionManager) GetStats() ReconnectionStats {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return ReconnectionStats{
		AttemptCount:      rm.attemptCount,
		CurrentDelay:      rm.currentDelay,
		InitialDelay:      rm.initialDelay,
		MaxDelay:          rm.maxDelay,
		IsAtMaxDelay:      rm.currentDelay >= rm.maxDelay,
		BackoffMultiplier: rm.backoffMultiplier,
		JitterEnabled:     rm.jitterEnabled,
	}
}

// ReconnectionStats holds statistics about reconnection attempts
type ReconnectionStats struct {
	AttemptCount      int           `json:"attempt_count"`
	CurrentDelay      time.Duration `json:"current_delay"`
	InitialDelay      time.Duration `json:"initial_delay"`
	MaxDelay          time.Duration `json:"max_delay"`
	IsAtMaxDelay      bool          `json:"is_at_max_delay"`
	BackoffMultiplier float64       `json:"backoff_multiplier"`
	JitterEnabled     bool          `json:"jitter_enabled"`
}
