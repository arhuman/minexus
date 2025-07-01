package command

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/arhuman/minexus/internal/logging"
	pb "github.com/arhuman/minexus/protogen"
)

// File operation constants
const (
	MaxPreviewSize = 1024 * 1024       // 1MB
	MaxFileSize    = 100 * 1024 * 1024 // 100MB
)

// FileCommandType represents the type of file operation to perform
type FileCommandType string

const (
	CmdGet  FileCommandType = "get"
	CmdCopy FileCommandType = "copy"
	CmdMove FileCommandType = "move"
	CmdInfo FileCommandType = "info"
)

// FileRequest represents a file operation request
type FileRequest struct {
	Command     FileCommandType `json:"command"`
	Source      string          `json:"source"`
	Destination string          `json:"destination,omitempty"` // Used for copy/move operations
	Recursive   bool            `json:"recursive,omitempty"`   // For directory operations
	Options     FileOptions     `json:"options,omitempty"`
}

// FileOptions contains additional options for file operations
type FileOptions struct {
	CreateDirs   bool  `json:"create_dirs,omitempty"`   // Create destination directories if they don't exist
	Overwrite    bool  `json:"overwrite,omitempty"`     // Overwrite existing files
	PreservePerm bool  `json:"preserve_perm,omitempty"` // Preserve file permissions
	MaxSize      int64 `json:"max_size,omitempty"`      // Maximum file size to process (bytes)
}

// FileInfo represents information about a file or directory
type FileInfo struct {
	Path        string      `json:"path"`
	Name        string      `json:"name"`
	Size        int64       `json:"size"`
	Mode        os.FileMode `json:"mode"`
	ModTime     time.Time   `json:"mod_time"`
	IsDir       bool        `json:"is_dir"`
	Permissions string      `json:"permissions"`
	Owner       string      `json:"owner,omitempty"`
	Group       string      `json:"group,omitempty"`
	ContentType string      `json:"content_type,omitempty"`
	Checksum    string      `json:"checksum,omitempty"`
}

// GetResponse represents the response for a get command
type GetResponse struct {
	FileInfo    FileInfo `json:"file_info"`
	Content     []byte   `json:"content,omitempty"`      // File content (for text files or small files)
	ContentB64  string   `json:"content_b64,omitempty"`  // Base64 encoded content (for binary files)
	Truncated   bool     `json:"truncated,omitempty"`    // Whether content was truncated due to size
	PreviewOnly bool     `json:"preview_only,omitempty"` // Whether only a preview was returned
}

// CopyMoveResponse represents the response for copy/move commands
type CopyMoveResponse struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	FilesCount  int    `json:"files_count"`
	BytesCopied int64  `json:"bytes_copied"`
	Duration    string `json:"duration"`
}

// InfoResponse represents the response for an info command
type InfoResponse struct {
	FileInfo FileInfo   `json:"file_info"`
	Children []FileInfo `json:"children,omitempty"` // For directories
}

// Helper functions for file operations

// parseFileRequest parses a JSON string or simple string command into a FileRequest
func parseFileRequest(payload string) (*FileRequest, error) {
	// Try to parse as simple string command first (e.g., "file:get /path")
	if simpleReq, err := parseSimpleFileCommand(payload); err == nil {
		return simpleReq, nil
	}

	// Fall back to JSON parsing
	var req FileRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return nil, fmt.Errorf("failed to parse file request: %w", err)
	}

	// Validate required fields
	if req.Command == "" {
		return nil, fmt.Errorf("command is required")
	}
	if req.Source == "" {
		return nil, fmt.Errorf("source path is required")
	}

	// Validate command-specific requirements
	switch req.Command {
	case CmdCopy, CmdMove:
		if req.Destination == "" {
			return nil, fmt.Errorf("destination is required for %s command", req.Command)
		}
	case CmdGet, CmdInfo:
		// No additional validation needed
	default:
		return nil, fmt.Errorf("unsupported command: %s", req.Command)
	}

	return &req, nil
}

// parseSimpleFileCommand parses simple string file commands like "file:get /path"
func parseSimpleFileCommand(payload string) (*FileRequest, error) {
	// Must start with "file:"
	if !strings.HasPrefix(payload, "file:") {
		return nil, fmt.Errorf("not a simple file command")
	}

	// Remove "file:" prefix
	cmdStr := payload[5:]

	// Split into parts
	parts := strings.Fields(cmdStr)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid file command format, expected: file:<command> <source> [destination]")
	}

	command := parts[0]
	source := parts[1]
	var destination string
	if len(parts) > 2 {
		destination = parts[2]
	}

	// Create request based on command type
	var fileCmd FileCommandType
	switch command {
	case "get":
		fileCmd = CmdGet
	case "copy":
		fileCmd = CmdCopy
		if destination == "" {
			return nil, fmt.Errorf("copy command requires destination")
		}
	case "move":
		fileCmd = CmdMove
		if destination == "" {
			return nil, fmt.Errorf("move command requires destination")
		}
	case "info":
		fileCmd = CmdInfo
	default:
		return nil, fmt.Errorf("unsupported file command: %s", command)
	}

	return &FileRequest{
		Command:     fileCmd,
		Source:      source,
		Destination: destination,
		Options:     FileOptions{},
	}, nil
}

// validatePath validates file paths to prevent security issues
func validatePath(path string) error {
	// Prevent path traversal attacks
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("path traversal not allowed")
	}

	return nil
}

// getFileInfo retrieves detailed information about a file or directory
func getFileInfo(path string) (*FileInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	info := &FileInfo{
		Path:        path,
		Name:        stat.Name(),
		Size:        stat.Size(),
		Mode:        stat.Mode(),
		ModTime:     stat.ModTime(),
		IsDir:       stat.IsDir(),
		Permissions: stat.Mode().String(),
	}

	// Add content type for files
	if !stat.IsDir() {
		info.ContentType = mime.TypeByExtension(filepath.Ext(path))

		// Calculate checksum for small files
		if stat.Size() <= MaxPreviewSize {
			if checksum, err := calculateChecksum(path); err == nil {
				info.Checksum = checksum
			} else {
				return nil, fmt.Errorf("failed to calculate checksum: %w", err)
			}
		} else {
			// For large files, we don't calculate checksum to avoid performance issues
			info.Checksum = "N/A (file too large)"
		}
	}

	// Add owner/group info on Unix systems
	if runtime.GOOS != "windows" {
		addUnixOwnerInfo(stat, info)
	}

	return info, nil
}

// listDirectory lists all files in a directory
func listDirectory(path string) ([]FileInfo, error) {

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var children []FileInfo
	for _, entry := range entries {
		entryPath := filepath.Join(path, entry.Name())

		if info, err := getFileInfo(entryPath); err == nil {
			children = append(children, *info)
		}
	}

	return children, nil
}

// calculateChecksum calculates MD5 checksum of a file
func calculateChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// isBinaryContent determines if content is binary
func isBinaryContent(content []byte) bool {
	// Simple heuristic: if content contains null bytes, consider it binary
	for _, b := range content {
		if b == 0 {
			return true
		}
	}
	return false
}

// copyFile copies a single file
func copyFile(src, dst string, options FileOptions) (int64, error) {

	// Check if destination exists
	if _, err := os.Stat(dst); err == nil && !options.Overwrite {
		return 0, fmt.Errorf("destination file exists and overwrite is false")
	}

	// Create destination directory if needed
	if options.CreateDirs {
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return 0, fmt.Errorf("failed to create destination directory: %w", err)
		}
	}

	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Create destination file
	dstFile, err := os.Create(dst)
	if err != nil {
		return 0, fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	// Copy content
	bytesCopied, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return bytesCopied, fmt.Errorf("failed to copy file content: %w", err)
	}

	// Preserve permissions if requested
	if options.PreservePerm {
		if srcInfo, err := os.Stat(src); err == nil {
			if err := os.Chmod(dst, srcInfo.Mode()); err != nil {
				return 0, fmt.Errorf("failed to preserve file permissions: %w", err)
			}
		}
	}

	return bytesCopied, nil
}

// copyDirectory copies a directory recursively
func copyDirectory(src, dst string, options FileOptions) (int, int64, error) {

	var filesCount int
	var totalBytes int64

	// Create destination directory
	if err := os.MkdirAll(dst, 0755); err != nil {
		return 0, 0, fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Walk source directory
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Copy file
		bytesCopied, err := copyFile(path, dstPath, options)
		if err != nil {
			return err
		}

		filesCount++
		totalBytes += bytesCopied
		return nil
	})

	if err != nil {
		return filesCount, totalBytes, err
	}

	return filesCount, totalBytes, nil
}

// FileGetCommand retrieves file content or information
type FileGetCommand struct {
	*BaseCommand
}

// NewFileGetCommand creates a new file get command
func NewFileGetCommand() *FileGetCommand {
	base := NewBaseCommand(
		"file:get",
		"file",
		"Retrieve file content or information from minion",
		`{"command": "get", "source": "/path/to/file", "options": {"max_size": 1048576}}`,
	).WithExamples(
		Example{
			Description: "Get a text file",
			Command:     `command-send minion abc123 '{"command": "get", "source": "/etc/hosts"}'`,
			Expected:    "Returns file content and metadata",
		},
		Example{
			Description: "Get file info only (large file)",
			Command:     `command-send minion abc123 '{"command": "get", "source": "/var/log/large.log", "options": {"max_size": 1024}}'`,
			Expected:    "Returns truncated content and full metadata",
		},
	).WithParameters(
		Param{Name: "command", Type: "string", Required: true, Description: "Must be 'get'"},
		Param{Name: "source", Type: "string", Required: true, Description: "Path to file or directory"},
		Param{Name: "options.max_size", Type: "int64", Required: false, Description: "Maximum file size to read", Default: "104857600"},
	).WithNotes(
		"Binary files are returned as base64-encoded content",
		"Large files are automatically truncated with preview",
		"Directory requests return metadata only",
	)

	return &FileGetCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *FileGetCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	funcName := "FileGetCommand.Execute"
	logger, start := logging.FuncLogger(ctx.Logger, funcName)
	defer logging.FuncExit(logger, start)

	// Parse the request
	request, err := parseFileRequest(payload)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to parse request: %w", err)), nil
	}

	// Validate command type
	if request.Command != CmdGet {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid command type: %s", request.Command)), nil
	}

	// Validate path
	if err := validatePath(request.Source); err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid source path: %w", err)), nil
	}

	sourcePath := filepath.Clean(request.Source)

	// Get file info
	fileInfo, err := getFileInfo(sourcePath)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to get file info: %w", err)), nil
	}

	response := &GetResponse{
		FileInfo: *fileInfo,
	}

	// If it's a directory, don't try to read content
	if fileInfo.IsDir {
		jsonOutput, err := json.Marshal(response)
		if err != nil {
			return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to serialize response: %w", err)), nil
		}
		return c.BaseCommand.CreateSuccessResult(ctx, string(jsonOutput)), nil
	}

	// Check file size limits
	maxSize := int64(MaxFileSize)
	if request.Options.MaxSize > 0 && request.Options.MaxSize < maxSize {
		maxSize = request.Options.MaxSize
	}

	if fileInfo.Size > maxSize {
		response.PreviewOnly = true
		response.Truncated = true
		maxSize = MaxPreviewSize
	}

	// Read file content
	file, err := os.Open(sourcePath)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to open file: %w", err)), nil
	}
	defer file.Close()

	// Read content up to max size
	content := make([]byte, maxSize)
	n, err := io.ReadFull(file, content)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to read file: %w", err)), nil
	}
	content = content[:n]

	// Determine if content is binary or text
	if isBinaryContent(content) {
		response.ContentB64 = base64.StdEncoding.EncodeToString(content)
	} else {
		response.Content = content
	}

	// Serialize response to JSON
	jsonOutput, err := json.Marshal(response)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to serialize response: %w", err)), nil
	}

	return c.BaseCommand.CreateSuccessResult(ctx, string(jsonOutput)), nil
}

// FileCopyCommand copies files or directories
type FileCopyCommand struct {
	*BaseCommand
}

// NewFileCopyCommand creates a new file copy command
func NewFileCopyCommand() *FileCopyCommand {
	base := NewBaseCommand(
		"file:copy",
		"file",
		"Copy files or directories on the minion",
		`{"command": "copy", "source": "/src/path", "destination": "/dst/path", "recursive": true, "options": {"overwrite": true}}`,
	).WithExamples(
		Example{
			Description: "Copy a single file",
			Command:     `command-send minion abc123 '{"command": "copy", "source": "/tmp/source.txt", "destination": "/tmp/backup.txt"}'`,
			Expected:    "File copied successfully",
		},
		Example{
			Description: "Copy directory recursively",
			Command:     `command-send minion abc123 '{"command": "copy", "source": "/home/user/docs", "destination": "/backup/docs", "recursive": true, "options": {"create_dirs": true}}'`,
			Expected:    "Directory and all contents copied",
		},
	).WithParameters(
		Param{Name: "command", Type: "string", Required: true, Description: "Must be 'copy'"},
		Param{Name: "source", Type: "string", Required: true, Description: "Source path"},
		Param{Name: "destination", Type: "string", Required: true, Description: "Destination path"},
		Param{Name: "recursive", Type: "bool", Required: false, Description: "Copy directories recursively", Default: "false"},
		Param{Name: "options.overwrite", Type: "bool", Required: false, Description: "Overwrite existing files", Default: "false"},
		Param{Name: "options.create_dirs", Type: "bool", Required: false, Description: "Create destination directories", Default: "false"},
		Param{Name: "options.preserve_perm", Type: "bool", Required: false, Description: "Preserve file permissions", Default: "false"},
	)

	return &FileCopyCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *FileCopyCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	funcName := "FileCopyCommand.Execute"
	logger, start := logging.FuncLogger(ctx.Logger, funcName)
	defer logging.FuncExit(logger, start)

	request, err := parseFileRequest(payload)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to parse request: %w", err)), nil
	}

	if request.Command != CmdCopy {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid command type: %s", request.Command)), nil
	}

	// Validate paths
	if err := validatePath(request.Source); err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid source path: %w", err)), nil
	}
	if err := validatePath(request.Destination); err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid destination path: %w", err)), nil
	}

	sourcePath := filepath.Clean(request.Source)
	destPath := filepath.Clean(request.Destination)
	startTime := time.Now()

	var filesCount int
	var bytesCopied int64

	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("source does not exist: %w", err)), nil
	}

	if sourceInfo.IsDir() {
		if !request.Recursive {
			return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("source is a directory, use recursive flag")), nil
		}
		filesCount, bytesCopied, err = copyDirectory(sourcePath, destPath, request.Options)
	} else {
		bytesCopied, err = copyFile(sourcePath, destPath, request.Options)
		if err == nil {
			filesCount = 1
		}
	}

	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("copy operation failed: %w", err)), nil
	}

	response := &CopyMoveResponse{
		Source:      request.Source,
		Destination: request.Destination,
		FilesCount:  filesCount,
		BytesCopied: bytesCopied,
		Duration:    time.Since(startTime).String(),
	}

	jsonOutput, err := json.Marshal(response)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to serialize response: %w", err)), nil
	}

	return c.BaseCommand.CreateSuccessResult(ctx, string(jsonOutput)), nil
}

// FileMoveCommand moves/renames files or directories
type FileMoveCommand struct {
	*BaseCommand
}

// NewFileMoveCommand creates a new file move command
func NewFileMoveCommand() *FileMoveCommand {
	base := NewBaseCommand(
		"file:move",
		"file",
		"Move/rename files or directories on the minion",
		`{"command": "move", "source": "/old/path", "destination": "/new/path"}`,
	).WithExamples(
		Example{
			Description: "Rename a file",
			Command:     `command-send minion abc123 '{"command": "move", "source": "/tmp/old_name.txt", "destination": "/tmp/new_name.txt"}'`,
			Expected:    "File renamed successfully",
		},
		Example{
			Description: "Move directory to different location",
			Command:     `command-send minion abc123 '{"command": "move", "source": "/home/user/temp", "destination": "/archive/temp"}'`,
			Expected:    "Directory moved successfully",
		},
	).WithParameters(
		Param{Name: "command", Type: "string", Required: true, Description: "Must be 'move'"},
		Param{Name: "source", Type: "string", Required: true, Description: "Source path"},
		Param{Name: "destination", Type: "string", Required: true, Description: "Destination path"},
	)

	return &FileMoveCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *FileMoveCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {

	request, err := parseFileRequest(payload)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to parse request: %w", err)), nil
	}

	if request.Command != CmdMove {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid command type: %s", request.Command)), nil
	}

	// Validate paths
	if err := validatePath(request.Source); err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid source path: %w", err)), nil
	}
	if err := validatePath(request.Destination); err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid destination path: %w", err)), nil
	}

	sourcePath := filepath.Clean(request.Source)
	destPath := filepath.Clean(request.Destination)
	startTime := time.Now()

	// Try atomic rename first
	err = os.Rename(sourcePath, destPath)
	if err == nil {
		// Success with atomic rename
		sourceInfo, _ := os.Stat(destPath)
		var size int64
		if sourceInfo != nil {
			size = sourceInfo.Size()
		}

		response := &CopyMoveResponse{
			Source:      request.Source,
			Destination: request.Destination,
			FilesCount:  1,
			BytesCopied: size,
			Duration:    time.Since(startTime).String(),
		}

		jsonOutput, err := json.Marshal(response)
		if err != nil {
			return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to serialize response: %w", err)), nil
		}

		return c.BaseCommand.CreateSuccessResult(ctx, string(jsonOutput)), nil
	}

	// If rename failed, try copy and delete
	// Check if source exists first
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("source does not exist: %w", err)), nil
	}

	var filesCount int
	var bytesCopied int64

	if sourceInfo.IsDir() {
		filesCount, bytesCopied, err = copyDirectory(sourcePath, destPath, request.Options)
	} else {
		bytesCopied, err = copyFile(sourcePath, destPath, request.Options)
		if err == nil {
			filesCount = 1
		}
	}

	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("move operation failed during copy: %w", err)), nil
	}

	// Delete source after successful copy
	err = os.RemoveAll(sourcePath)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to delete source after copy: %w", err)), nil
	}

	response := &CopyMoveResponse{
		Source:      request.Source,
		Destination: request.Destination,
		FilesCount:  filesCount,
		BytesCopied: bytesCopied,
		Duration:    time.Since(startTime).String(),
	}

	jsonOutput, err := json.Marshal(response)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to serialize response: %w", err)), nil
	}

	return c.BaseCommand.CreateSuccessResult(ctx, string(jsonOutput)), nil
}

// FileInfoCommand gets detailed information about files or directories
type FileInfoCommand struct {
	*BaseCommand
}

// NewFileInfoCommand creates a new file info command
func NewFileInfoCommand() *FileInfoCommand {
	base := NewBaseCommand(
		"file:info",
		"file",
		"Get detailed information about files or directories",
		`{"command": "info", "source": "/path/to/file", "recursive": true}`,
	).WithExamples(
		Example{
			Description: "Get file information",
			Command:     `command-send minion abc123 '{"command": "info", "source": "/etc/passwd"}'`,
			Expected:    "Returns file metadata including size, permissions, timestamps",
		},
		Example{
			Description: "List directory contents",
			Command:     `command-send minion abc123 '{"command": "info", "source": "/home/user", "recursive": true}'`,
			Expected:    "Returns directory info and children list",
		},
	).WithParameters(
		Param{Name: "command", Type: "string", Required: true, Description: "Must be 'info'"},
		Param{Name: "source", Type: "string", Required: true, Description: "Path to file or directory"},
		Param{Name: "recursive", Type: "bool", Required: false, Description: "List directory contents", Default: "false"},
	)

	return &FileInfoCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface
func (c *FileInfoCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {

	request, err := parseFileRequest(payload)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to parse request: %w", err)), nil
	}

	if request.Command != CmdInfo {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid command type: %s", request.Command)), nil
	}

	// Validate path
	if err := validatePath(request.Source); err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("invalid source path: %w", err)), nil
	}

	sourcePath := filepath.Clean(request.Source)

	fileInfo, err := getFileInfo(sourcePath)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to get file info: %w", err)), nil
	}

	response := &InfoResponse{
		FileInfo: *fileInfo,
	}

	// If it's a directory and recursive is requested, list children
	if fileInfo.IsDir && request.Recursive {
		children, err := listDirectory(sourcePath)
		if err != nil {
			return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to list directory: %w", err)), nil
		}
		response.Children = children
	}

	jsonOutput, err := json.Marshal(response)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to serialize response: %w", err)), nil
	}

	return c.BaseCommand.CreateSuccessResult(ctx, string(jsonOutput)), nil
}

// UnifiedFileCommand provides a unified file command that routes to specific operations
type UnifiedFileCommand struct {
	*BaseCommand
}

// NewFileCommand creates a new unified file command
func NewFileCommand() *UnifiedFileCommand {
	base := NewBaseCommand(
		"file",
		"file",
		"Unified file operations command that routes to specific file operations",
		`{"command": "get|copy|move|info", "source": "/path", "destination": "/path", "options": {...}}`,
	).WithExamples(
		Example{
			Description: "Simple file get using string format",
			Command:     "command-send minion abc123 'file:get /etc/hosts'",
			Expected:    "Returns file content in JSON format",
		},
		Example{
			Description: "JSON format file copy",
			Command:     `command-send minion abc123 '{"command": "copy", "source": "/tmp/file.txt", "destination": "/backup/file.txt"}'`,
			Expected:    "Returns copy operation result",
		},
	).WithParameters(
		Param{Name: "command", Type: "string", Required: true, Description: "Operation type: get, copy, move, or info"},
		Param{Name: "source", Type: "string", Required: true, Description: "Source file or directory path"},
		Param{Name: "destination", Type: "string", Required: false, Description: "Destination path (required for copy/move)"},
		Param{Name: "recursive", Type: "bool", Required: false, Description: "Apply operation recursively", Default: "false"},
		Param{Name: "options", Type: "object", Required: false, Description: "Additional options for the operation"},
	).WithNotes(
		"Supports both simple string format (file:get /path) and JSON format",
		"JSON format allows for more complex options and parameters",
		"All file operations include comprehensive error handling and validation",
	)

	return &UnifiedFileCommand{
		BaseCommand: base,
	}
}

// Execute implements ExecutableCommand interface and routes to appropriate file operation
func (c *UnifiedFileCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
	request, err := parseFileRequest(payload)
	if err != nil {
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("failed to parse request: %w", err)), nil
	}

	// Route to the appropriate operation based on command type
	var result *pb.CommandResult
	switch request.Command {
	case CmdGet:
		getCmd := NewFileGetCommand()
		result, err = getCmd.Execute(ctx, payload)
	case CmdCopy:
		copyCmd := NewFileCopyCommand()
		result, err = copyCmd.Execute(ctx, payload)
	case CmdMove:
		moveCmd := NewFileMoveCommand()
		result, err = moveCmd.Execute(ctx, payload)
	case CmdInfo:
		infoCmd := NewFileInfoCommand()
		result, err = infoCmd.Execute(ctx, payload)
	default:
		return c.BaseCommand.CreateErrorResult(ctx, fmt.Errorf("unsupported command: %s", request.Command)), nil
	}

	return result, err
}
