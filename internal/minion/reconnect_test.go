package minion

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestReconnectionManager(t *testing.T) {
	logger := zap.NewNop()
	initialDelay := 100 * time.Millisecond
	maxDelay := 5 * time.Second

	rm := NewReconnectionManager(initialDelay, maxDelay, logger)

	// Disable jitter for exact delay testing
	rm.SetJitterEnabled(false)

	// Test initial state
	if rm.GetCurrentDelay() != initialDelay {
		t.Errorf("Expected initial delay %v, got %v", initialDelay, rm.GetCurrentDelay())
	}

	if rm.GetAttemptCount() != 0 {
		t.Errorf("Expected initial attempt count 0, got %v", rm.GetAttemptCount())
	}

	// Test first delay (should be initial delay)
	delay1 := rm.GetNextDelay()
	if delay1 != initialDelay {
		t.Errorf("Expected first delay %v, got %v", initialDelay, delay1)
	}

	// Test exponential backoff progression
	delay2 := rm.GetNextDelay()
	expectedDelay2 := 2 * initialDelay
	if delay2 != expectedDelay2 {
		t.Errorf("Expected second delay %v, got %v", expectedDelay2, delay2)
	}

	delay3 := rm.GetNextDelay()
	expectedDelay3 := 4 * initialDelay
	if delay3 != expectedDelay3 {
		t.Errorf("Expected third delay %v, got %v", expectedDelay3, delay3)
	}

	// Test that delay caps at maximum
	for i := 0; i < 10; i++ {
		delay := rm.GetNextDelay()
		if delay > maxDelay {
			t.Errorf("Delay %v exceeded maximum %v", delay, maxDelay)
		}
	}

	// Test reset functionality
	rm.ResetDelay()
	if rm.GetCurrentDelay() != initialDelay {
		t.Errorf("Expected delay to reset to %v, got %v", initialDelay, rm.GetCurrentDelay())
	}

	if rm.GetAttemptCount() != 0 {
		t.Errorf("Expected attempt count to reset to 0, got %v", rm.GetAttemptCount())
	}
}

func TestReconnectionManagerJitter(t *testing.T) {
	logger := zap.NewNop()
	initialDelay := 1 * time.Second
	maxDelay := 10 * time.Second

	rm := NewReconnectionManager(initialDelay, maxDelay, logger)

	// Ensure jitter is enabled (it should be by default)
	rm.SetJitterEnabled(true)

	// Test that jitter produces different values
	delays := make([]time.Duration, 100) // Use more samples for better jitter detection
	for i := 0; i < 100; i++ {
		delays[i] = rm.GetNextDelay()
		rm.ResetDelay() // Reset for consistent base delay
	}

	// Check that we get some variation (jitter should make values different)
	allSame := true
	uniqueDelays := make(map[time.Duration]bool)

	for _, delay := range delays {
		uniqueDelays[delay] = true
		if delay != delays[0] {
			allSame = false
		}
	}

	if allSame {
		t.Error("Expected jitter to produce some variation in delays")
	}

	// Should have multiple unique values due to jitter
	if len(uniqueDelays) < 3 {
		t.Errorf("Expected at least 3 unique delay values due to jitter, got %d unique values", len(uniqueDelays))
	}

	// All delays should be within reasonable bounds
	for i, delay := range delays {
		if delay < 100*time.Millisecond {
			t.Errorf("Delay %d (%v) is too small (below minimum jitter)", i, delay)
		}
		if delay > initialDelay {
			t.Errorf("Delay %d (%v) exceeded base delay %v", i, delay, initialDelay)
		}
	}
}

func TestReconnectionManagerStats(t *testing.T) {
	logger := zap.NewNop()
	initialDelay := 500 * time.Millisecond
	maxDelay := 30 * time.Second

	rm := NewReconnectionManager(initialDelay, maxDelay, logger)

	// Get initial stats
	stats := rm.GetStats()
	if stats.AttemptCount != 0 {
		t.Errorf("Expected initial attempt count 0, got %v", stats.AttemptCount)
	}
	if stats.CurrentDelay != initialDelay {
		t.Errorf("Expected initial current delay %v, got %v", initialDelay, stats.CurrentDelay)
	}
	if stats.InitialDelay != initialDelay {
		t.Errorf("Expected initial delay %v, got %v", initialDelay, stats.InitialDelay)
	}
	if stats.MaxDelay != maxDelay {
		t.Errorf("Expected max delay %v, got %v", maxDelay, stats.MaxDelay)
	}
	if stats.IsAtMaxDelay {
		t.Error("Expected IsAtMaxDelay to be false initially")
	}
	if !stats.JitterEnabled {
		t.Error("Expected jitter to be enabled by default")
	}

	// Progress through several attempts
	for i := 0; i < 5; i++ {
		rm.GetNextDelay()
	}

	// Check stats after progression
	stats = rm.GetStats()
	if stats.AttemptCount != 5 {
		t.Errorf("Expected attempt count 5, got %v", stats.AttemptCount)
	}
	if stats.CurrentDelay == initialDelay {
		t.Error("Expected current delay to have increased from initial")
	}
}

func TestReconnectionManagerEdgeCases(t *testing.T) {
	logger := zap.NewNop()

	// Test with very small delays - jitter should enforce minimum
	rm := NewReconnectionManager(1*time.Nanosecond, 1*time.Microsecond, logger)
	rm.SetJitterEnabled(true) // Ensure jitter is enabled
	delay := rm.GetNextDelay()
	if delay < 100*time.Millisecond {
		t.Errorf("Expected minimum jitter delay of 100ms, got %v", delay)
	}

	// Test with initial delay equal to max delay
	rm2 := NewReconnectionManager(5*time.Second, 5*time.Second, logger)
	rm2.SetJitterEnabled(true)
	for i := 0; i < 3; i++ {
		delay := rm2.GetNextDelay()
		if delay > 5*time.Second {
			t.Errorf("Delay %v exceeded max delay %v", delay, 5*time.Second)
		}
	}

	// Test with zero delays - should still enforce minimum
	rm3 := NewReconnectionManager(0, 0, logger)
	rm3.SetJitterEnabled(true)
	delay = rm3.GetNextDelay()
	if delay < 100*time.Millisecond {
		t.Errorf("Expected minimum jitter delay of 100ms even with zero config, got %v", delay)
	}

	// Test that without jitter, small delays are preserved
	rm4 := NewReconnectionManager(1*time.Nanosecond, 1*time.Microsecond, logger)
	rm4.SetJitterEnabled(false)
	delay = rm4.GetNextDelay()
	if delay != 1*time.Nanosecond {
		t.Errorf("Expected delay to be preserved without jitter: expected %v, got %v", 1*time.Nanosecond, delay)
	}
}
