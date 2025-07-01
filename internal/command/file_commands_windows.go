//go:build windows
// +build windows

package command

import (
	"os"
)

// addUnixOwnerInfo is a no-op on Windows
func addUnixOwnerInfo(stat os.FileInfo, info *FileInfo) {
	// Windows doesn't have Unix-style ownership
	// So we leave the Owner and Group fields empty
}
