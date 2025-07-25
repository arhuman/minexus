// Package util provides common utility functions for the Minexus system.
package util

import (
	"fmt"
	"strings"
)

// IsSpace reports whether the character is a Unicode white space character.
// Simplified version of unicode.IsSpace for shell-style command parsing.
func IsSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r'
}

// ParseCommandLine parses a command line string respecting shell-style quoting.
// This handles single quotes, double quotes, and escaped characters properly.
func ParseCommandLine(line string) ([]string, error) {
	var args []string
	var current strings.Builder
	var inSingleQuote, inDoubleQuote bool
	var escaped bool

	for _, r := range line {
		if escaped {
			// Previous character was a backslash, add this character literally
			current.WriteRune(r)
			escaped = false
			continue
		}

		switch r {
		case '\\':
			// Escape next character (only if not in single quotes)
			if !inSingleQuote {
				escaped = true
				continue
			}
			current.WriteRune(r)

		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			} else {
				current.WriteRune(r)
			}

		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			} else {
				current.WriteRune(r)
			}

		default:
			if IsSpace(r) && !inSingleQuote && !inDoubleQuote {
				// End of current argument
				if current.Len() > 0 {
					args = append(args, current.String())
					current.Reset()
				}
			} else {
				current.WriteRune(r)
			}
		}
	}

	// Check for unclosed quotes
	if inSingleQuote {
		return nil, fmt.Errorf("unclosed single quote")
	}
	if inDoubleQuote {
		return nil, fmt.Errorf("unclosed double quote")
	}

	// Add final argument if any
	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args, nil
}
