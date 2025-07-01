//go:build !windows
// +build !windows

package command

import (
	"os"
	"strconv"
	"syscall"
)

// addUnixOwnerInfo adds Unix-specific owner information
func addUnixOwnerInfo(stat os.FileInfo, info *FileInfo) {
	if sys, ok := stat.Sys().(*syscall.Stat_t); ok {
		info.Owner = strconv.Itoa(int(sys.Uid))
		info.Group = strconv.Itoa(int(sys.Gid))
	}
}
