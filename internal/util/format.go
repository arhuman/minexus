// Package util provides common utility functions for the Minexus system.
package util

import (
	"fmt"
	"strings"
	"time"
)

// FormatTags formats tags map for display
func FormatTags(tags map[string]string) string {
	if len(tags) == 0 {
		return "-"
	}

	var parts []string
	for k, v := range tags {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}

	result := strings.Join(parts, ", ")
	if len(result) > 30 {
		return result[:27] + "..."
	}
	return result
}

// FormatLastSeen formats Unix timestamp for display
func FormatLastSeen(timestamp int64) string {
	if timestamp == 0 {
		return "Never"
	}

	lastSeen := time.Unix(timestamp, 0)
	now := time.Now()

	duration := now.Sub(lastSeen)

	if duration < time.Minute {
		return "Just now"
	} else if duration < time.Hour {
		minutes := int(duration.Minutes())
		return fmt.Sprintf("%dm ago", minutes)
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		return fmt.Sprintf("%dh ago", hours)
	} else {
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}
