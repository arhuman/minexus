# Minion Commands Guide

This document provides comprehensive information about all commands that can be sent to minions through the Minexus console.

## Overview

Minions can execute different types of commands:
1. **System Commands** - Predefined system operations (system:info, system:os)
2. **File Commands** - JSON-formatted commands for file operations (file:get, file:copy, etc.)
3. **Logging Commands** - Dynamic logging level management (logging:level, logging:increase, logging:decrease)
4. **Shell Commands** - Direct shell command execution

## Command Syntax

### New Improved Syntax (Recommended)

The console now uses an improved, clearer syntax that eliminates common mistakes:

```bash
# Send to all minions
minexus> command-send all <command>

# Send to specific minion
minexus> command-send minion <id> <command>

# Send to minions with specific tag
minexus> command-send tag <key>=<value> <command>
```

### Command Type Examples

```bash
# System commands
minexus> command-send all system:info
minexus> command-send minion abc123 system:os

# File commands
minexus> command-send minion abc123 file:get "/etc/hosts"

# Logging commands
minexus> command-send all logging:level
minexus> command-send minion abc123 logging:increase

# Shell commands (explicit)
minexus> command-send all shell "ls -la"

# Shell commands (implicit - default)
minexus> command-send tag env=prod "df -h"
```

### Legacy Syntax (Deprecated)

⚠️ **The old syntax with flags is deprecated and will be removed in a future version:**

```bash
# OLD - Will show deprecation warning
minexus> command-send -m <id> <command>
minexus> command-send -t <key>=<value> <command>
minexus> command-send <command>  # to all minions
```

## Accessing Command Help

### Console Help System

The console provides comprehensive help for all available commands:

```
minexus> help                    # Show all available commands
minexus> help <command-name>     # Show detailed help for specific command
```

**Examples:**
```
minexus> help file:get          # Detailed help for file get command
minexus> help shell             # Help for shell commands
minexus> help system:info       # Help for system info command
```

## File Commands

File commands use JSON format and provide secure file operations on minions.

### file:get - Retrieve File Content

**Purpose:** Get file content or information from minion filesystem

**Usage:**
```json
{"command": "get", "source": "/path/to/file", "options": {"max_size": 1048576}}
```

**Parameters:**
- `command` (string, required): Must be "get"
- `source` (string, required): Path to file or directory
- `options.max_size` (int64, optional): Maximum file size to read (default: 100MB)

**Examples:**

1. **Get a text file:**
   ```
   minexus> command-send minion abc123 '{"command": "get", "source": "/etc/hosts"}'
   ```

2. **Get file with size limit:**
   ```
   minexus> command-send minion abc123 '{"command": "get", "source": "/var/log/app.log", "options": {"max_size": 1024}}'
   ```

3. **Get directory info:**
   ```
   minexus> command-send all '{"command": "get", "source": "/home/user/documents"}'
   ```

**Notes:**
- Binary files are returned as base64-encoded content
- Large files are automatically truncated with preview
- Directory requests return metadata only

### file:copy - Copy Files

**Purpose:** Copy files or directories on the minion

**Usage:**
```json
{
  "command": "copy",
  "source": "/src/path",
  "destination": "/dst/path",
  "recursive": true,
  "options": {
    "overwrite": true,
    "create_dirs": true,
    "preserve_perm": false
  }
}
```

**Parameters:**
- `command` (string, required): Must be "copy"
- `source` (string, required): Source path
- `destination` (string, required): Destination path
- `recursive` (bool, optional): Copy directories recursively (default: false)
- `options.overwrite` (bool, optional): Overwrite existing files (default: false)
- `options.create_dirs` (bool, optional): Create destination directories (default: false)
- `options.preserve_perm` (bool, optional): Preserve file permissions (default: false)

**Examples:**

1. **Copy single file:**
   ```
   minexus> command-send minion abc123 '{"command": "copy", "source": "/tmp/source.txt", "destination": "/backup/source.txt"}'
   ```

2. **Copy directory recursively:**
   ```
   minexus> command-send minion abc123 '{"command": "copy", "source": "/home/user/docs", "destination": "/backup/docs", "recursive": true, "options": {"create_dirs": true}}'
   ```

### file:move - Move/Rename Files

**Purpose:** Move or rename files and directories on the minion

**Usage:**
```json
{"command": "move", "source": "/old/path", "destination": "/new/path"}
```

**Parameters:**
- `command` (string, required): Must be "move"
- `source` (string, required): Source path
- `destination` (string, required): Destination path

**Examples:**

1. **Rename a file:**
   ```
   minexus> command-send minion abc123 '{"command": "move", "source": "/tmp/old_name.txt", "destination": "/tmp/new_name.txt"}'
   ```

2. **Move directory:**
   ```
   minexus> command-send minion abc123 '{"command": "move", "source": "/home/user/temp", "destination": "/archive/temp"}'
   ```

**Notes:**
- Attempts atomic rename first, falls back to copy+delete if needed
- Be careful with cross-filesystem moves

### file:info - Get File Information

**Purpose:** Get detailed information about files or directories

**Usage:**
```json
{"command": "info", "source": "/path/to/file", "recursive": true}
```

**Parameters:**
- `command` (string, required): Must be "info"
- `source` (string, required): Path to file or directory
- `recursive` (bool, optional): List directory contents (default: false)

**Examples:**

1. **Get file information:**
   ```
   minexus> command-send minion abc123 '{"command": "info", "source": "/etc/passwd"}'
   ```

2. **List directory contents:**
   ```
   minexus> command-send minion abc123 '{"command": "info", "source": "/home/user", "recursive": true}'
   ```

## System Commands

### system:info - System Information

**Purpose:** Get system information including memory, uptime, and load

**Usage:**
```
minexus> command-send all system:info
minexus> command-send minion abc123 system:info
```

**Output includes:**
- System uptime (approximated)
- Memory usage statistics
- Current system load

### system:os - Operating System Info

**Purpose:** Get operating system and architecture information

**Usage:**
```
minexus> command-send all system:os
minexus> command-send minion abc123 system:os
```

**Output includes:**
- Operating system name (linux, windows, darwin, etc.)
- System architecture (amd64, arm64, etc.)

## Logging Commands

Logging commands provide dynamic control over the logging verbosity of minions at runtime without requiring service restart.

### logging:level - Get Current Logging Level

**Purpose:** Retrieve the current logging level of the minion

**Usage:**
```
minexus> command-send all logging:level
minexus> command-send minion abc123 logging:level
```

**Output:** Current logging level (e.g., "Current logging level: info")

**Example:**
```
minexus> command-send minion abc123 logging:level
Command dispatched successfully. Command ID: log123
minexus> result-get log123
Result from minion abc123:
  Exit Code: 0
  STDOUT: Current logging level: info
```

### logging:increase - Increase Logging Verbosity

**Purpose:** Increase logging verbosity by lowering the log level (more messages will be logged)

**Usage:**
```
minexus> command-send all logging:increase
minexus> command-send minion abc123 logging:increase
```

**Level progression (increasing verbosity):**
- fatal → panic → dpanic → error → warn → info → debug

**Output:** Confirmation message with old and new levels

**Examples:**

1. **Increase from info to debug:**
   ```
   minexus> command-send minion abc123 logging:increase
   Result: Logging level increased from info to debug (more verbose)
   ```

2. **Enable debug logging on production minions:**
   ```
   minexus> command-send tag env=prod logging:increase
   ```

3. **Already at maximum verbosity:**
   ```
   Result: Already at maximum verbosity level: debug
   ```

### logging:decrease - Decrease Logging Verbosity

**Purpose:** Decrease logging verbosity by raising the log level (fewer messages will be logged)

**Usage:**
```
minexus> command-send all logging:decrease
minexus> command-send minion abc123 logging:decrease
```

**Level progression (decreasing verbosity):**
- debug → info → warn → error → dpanic → panic → fatal

**Output:** Confirmation message with old and new levels

**Examples:**

1. **Decrease from debug to info:**
   ```
   minexus> command-send minion abc123 logging:decrease
   Result: Logging level decreased from debug to info (less verbose)
   ```

2. **Reduce log noise in production:**
   ```
   minexus> command-send tag env=prod logging:decrease
   ```

3. **Already at minimum verbosity:**
   ```
   Result: Already at minimum verbosity level: fatal
   ```

### Logging Commands Use Cases

**Debugging scenarios:**
```bash
# Check current logging levels across all minions
minexus> command-send all logging:level

# Enable debug logging for troubleshooting
minexus> command-send minion problematic-minion logging:increase

# Reduce verbosity after debugging
minexus> command-send minion problematic-minion logging:decrease
```

**Production management:**
```bash
# Temporarily increase logging for investigation
minexus> command-send tag env=prod logging:increase

# Return to normal logging levels
minexus> command-send tag env=prod logging:decrease
```

**Notes:**
- Changes take effect immediately without requiring minion restart
- All operations are atomic and thread-safe
- Cannot increase beyond debug level or decrease beyond fatal level
- Useful for dynamic troubleshooting and performance tuning

## Shell Commands

### shell - Execute Shell Commands

**Purpose:** Execute arbitrary shell commands on the minion

**Usage:**
```
minexus> command-send all <shell command>
minexus> command-send minion <id> <shell command>
minexus> command-send tag <key>=<value> <shell command>
```

**Examples:**

1. **List files:**
   ```
   minexus> command-send all "ls -la /tmp"
   minexus> command-send minion abc123 "ls -la /tmp"
   ```

2. **Check disk usage:**
   ```
   minexus> command-send tag env=prod "df -h"
   ```

3. **Show running processes:**
   ```
   minexus> command-send all "ps aux"
   ```

4. **Check system uptime:**
   ```
   minexus> command-send minion abc123 uptime
   ```

5. **Network information:**
   ```
   minexus> command-send tag role=server "ip addr show"
   ```

6. **System logs:**
   ```
   minexus> command-send minion abc123 "tail -n 20 /var/log/syslog"
   ```

7. **Explicit shell command:**
   ```
   minexus> command-send all shell "echo 'Hello from minion'"
   ```

**Notes:**
- Commands are executed in the default shell
- Output includes stdout, stderr, and exit code
- Be careful with destructive commands
- Commands run with minion process privileges
- No interactive commands (use non-interactive flags when available)

## Command Targeting

Commands can be sent to specific minions or groups using the improved syntax:

### Send to All Minions
```
minexus> command-send all "ls -la"
```

### Send to Specific Minion
```
minexus> command-send minion <minion-id> "ls -la"
```

### Send to Minions with Specific Tag
```
minexus> command-send tag environment=production "df -h"
```

### Common Mistakes to Avoid

❌ **Wrong (will fail silently):**
```
minexus> command-send abc123 ls -la          # Missing 'minion' keyword
minexus> command-send -m abc123 ls -la       # Old deprecated syntax
```

✅ **Correct:**
```
minexus> command-send minion abc123 "ls -la"  # Clear and explicit
```

## Command Results

After sending a command, you'll receive a command ID:

```
minexus> command-send all "ls -la"
Command dispatched successfully. Command ID: abc123
Use 'result-get abc123' to check results
```

### Retrieving Results
```
minexus> result-get abc123
```

Results include:
- Minion ID that executed the command
- Exit code
- Standard output (stdout)
- Standard error (stderr)
- Execution timestamp

## Security Considerations

### File Commands
- File operations are subject to the minion's file system permissions
- Path traversal attacks are prevented (`..` in paths)
- Optional base path restriction can be configured
- File size limits prevent memory exhaustion

### Shell Commands
- Commands execute with minion process privileges
- No privilege escalation (sudo commands require minion to run as appropriate user)
- Output is captured but commands are not sandboxed
- Avoid running destructive commands without verification

### Best Practices
1. **Test commands** on non-production minions first
2. **Use file commands** instead of shell commands for file operations when possible
3. **Be specific** with targeting to avoid unintended execution
4. **Monitor results** for all executed commands
5. **Use tags** to organize and target minions appropriately

## Error Handling

### Common Error Cases

1. **File not found:**
   ```json
   {"success": false, "error": "source does not exist: /nonexistent/file"}
   ```

2. **Permission denied:**
   ```json
   {"success": false, "error": "failed to open file: permission denied"}
   ```

3. **Invalid JSON for structured commands:**
   ```json
   {"success": false, "error": "failed to parse file request: invalid character"}
   ```

4. **Shell command not found:**
   ```
   Exit Code: 127
   STDERR: command not found: nonexistentcommand
   ```

### Troubleshooting

1. **Check minion connectivity:**
   ```
   minexus> minion-list
   ```

2. **Verify command syntax:**
   ```
   minexus> help <command-name>
   ```

3. **Check command results:**
   ```
   minexus> result-get <command-id>
   ```

4. **Test with simple commands first:**
   ```
   minexus> command-send echo "test"
   ```

## Console Commands

The console provides several management commands for interacting with minions and the system:

### minion-list (lm)
**Purpose:** List all connected minions with their status and information

**Usage:**
```
minexus> minion-list
minexus> lm
```

**Output includes:**
- Minion ID
- Hostname and IP address
- Operating system
- Last seen timestamp
- Assigned tags

### tag-list (lt)
**Purpose:** List all available tags across all minions

**Usage:**
```
minexus> tag-list
minexus> lt
```

**Output:** All unique tag key=value pairs found across connected minions

### result-get (results)
**Purpose:** Retrieve execution results for a specific command ID

**Usage:**
```
minexus> result-get <command-id>
minexus> results <command-id>
```

**Parameters:**
- `command-id` (required): The command ID returned when a command is dispatched

**Output includes:**
- Minion ID that executed the command
- Exit code
- Standard output (stdout)
- Standard error (stderr)
- Execution timestamp

### tag-set
**Purpose:** Set tags for a minion (replaces all existing tags)

**Usage:**
```
minexus> tag-set <minion-id> <key>=<value> [<key>=<value>...]
```

**Parameters:**
- `minion-id` (required): ID of the target minion
- `key=value` (required): One or more tag assignments

**Examples:**
```
minexus> tag-set abc123 env=prod role=web
minexus> tag-set def456 environment=development
```

### tag-update
**Purpose:** Update specific tags for a minion (add/remove individual tags)

**Usage:**
```
minexus> tag-update <minion-id> +<key>=<value> -<key> [...]
```

**Parameters:**
- `minion-id` (required): ID of the target minion
- `+key=value`: Add or update a tag
- `-key`: Remove a tag

**Examples:**
```
minexus> tag-update abc123 +version=2.1 -debug
minexus> tag-update def456 +env=staging +role=api -temp
```

## Command Development

To add new commands to the system:

1. **Add command logic** to appropriate package in `internal/command/`
2. **Register command** in [`internal/command/registry.go`](../internal/command/registry.go)
3. **Update help documentation** in the registry
4. **Test command** functionality

### Command Registry Structure

Commands are registered with:
- **Name**: Unique command identifier
- **Category**: Grouping (file, system, etc.)
- **Description**: Brief description
- **Usage**: Example usage syntax
- **Parameters**: Detailed parameter information
- **Examples**: Working examples with expected output
- **Notes**: Important usage notes

This ensures comprehensive help is automatically available for all commands.