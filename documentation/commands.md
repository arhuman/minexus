# Minexus Console Commands Reference

This document provides a comprehensive reference for all commands available in the Minexus console client.

## Overview

The Minexus console provides two types of commands:

1. **Console Commands**: Executed locally in the console to manage the session and interact with the Nexus server
2. **Minion Commands**: Sent to minions for execution via the `command-send` command

All commands support tab completion and are saved to command history for easy reuse.

## Console Help System

The console includes an interactive help system accessible via the `help` command:

```bash
help          # Show all available commands
help command  # Show detailed help for specific command
h             # Alias for help
```

## Console Commands

These commands are executed directly in the console interface:

### Core Navigation & Help

| Command | Aliases | Description | Example |
|---------|---------|-------------|---------|
| `help` | `h` | Show help information for console commands | `help command-send` |
| `version` | `v` | Show console version information | `version` |
| `quit` | `exit` | Exit the console gracefully | `quit` |
| `clear` | - | Clear the terminal screen | `clear` |
| `history` | - | Show command history information | `history` |

### Minion Management

| Command | Aliases | Description | Syntax |
|---------|---------|-------------|---------|
| `minion-list` | `lm` | List all connected minions with details | `minion-list` |
| `tag-list` | `lt` | List all available tags across minions | `tag-list` |
| `tag-set` | - | Set/replace all tags for a minion | `tag-set <minion-id> <key>=<value> [...]` |
| `tag-update` | - | Add/remove specific tags for a minion | `tag-update <minion-id> +<key>=<value> -<key> [...]` |

#### Tag Management Examples

```bash
# Set multiple tags for a minion (replaces all existing tags)
tag-set web-01 env=prod role=webserver region=us-east

# Add tags while keeping existing ones
tag-update web-01 +monitoring=enabled +backup=daily

# Remove specific tags
tag-update web-01 -temp -maintenance

# Combine add and remove operations
tag-update web-01 +version=2.1 -version=2.0
```

### Command Execution & Management

| Command | Aliases | Description | Syntax |
|---------|---------|-------------|---------|
| `command-send` | `cmd` | Send commands to minions | `command-send <target> <command>` |
| `result-get` | `results` | Get results for a specific command ID | `result-get <command-id>` |
| `command-status` | - | Show command execution status | `command-status <type>` |

#### Command Send Targets

The `command-send` command supports three targeting methods:

**Target All Minions:**
```bash
command-send all <command>
# Example: command-send all "uname -a"
```

**Target Specific Minion:**
```bash
command-send minion <minion-id> <command>
# Example: command-send minion web-01 "systemctl status nginx"
```

**Target by Tags:**
```bash
command-send tag <key>=<value> <command>
# Example: command-send tag env=prod "df -h"
```

#### Command Status Options

**Show All Commands Status:**
```bash
command-status all
# Displays breakdown: PENDING(2), COMPLETED(15), FAILED(1)
```

**Show Minion-Specific Status:**
```bash
command-status minion web-01
# Shows detailed command history for specific minion
```

**Show Statistics:**
```bash
command-status stats
# Shows success rates and performance metrics per minion
```

## Minion Commands

These are commands sent to minions using `command-send`. Minions execute these commands and return results.

### System Commands

| Command | Description | Example |
|---------|-------------|---------|
| `system:info` | Get comprehensive system information | `command-send all system:info` |
| `system:os` | Get operating system and architecture | `command-send all system:os` |

**System Info Output includes:**
- OS name and version  
- Architecture (amd64, arm64, etc.)
- Memory information (total, available, used)
- Hostname
- Uptime

### File Commands

File operations support both simple syntax and JSON format for complex operations:

| Command | Description | Simple Syntax | JSON Format |
|---------|-------------|---------------|-------------|
| `file:get` | Retrieve file content | `file:get /path/to/file` | See examples below |
| `file:copy` | Copy files/directories | Not supported | Required |
| `file:move` | Move/rename files | Not supported | Required |
| `file:info` | Get file information | `file:info /path/to/file` | Supported |

#### File Command Examples

**Simple file retrieval:**
```bash
command-send minion web-01 "file:get /etc/hosts"
command-send minion web-01 "file:get /var/log/nginx/access.log"
```

**Complex file operations (JSON format):**
```bash
# Copy file with options
command-send minion web-01 '{"command": "copy", "source": "/tmp/config.conf", "destination": "/etc/app/config.conf", "options": {"overwrite": true}}'

# Move/rename file
command-send minion web-01 '{"command": "move", "source": "/tmp/old_name.txt", "destination": "/tmp/new_name.txt"}'

# Get detailed file information
command-send minion web-01 '{"command": "info", "source": "/var/log/app.log"}'
```

### Logging Commands

Control minion logging levels remotely:

| Command | Description | Example |
|---------|-------------|---------|
| `logging:level` | Get current logging level | `command-send all logging:level` |
| `logging:increase` | Increase verbosity (debug←info←warn←error) | `command-send all logging:increase` |
| `logging:decrease` | Decrease verbosity (debug→info→warn→error) | `command-send all logging:decrease` |

### Docker Compose Commands

Manage Docker Compose applications on minions:

| Command | Description | Example |
|---------|-------------|---------|
| `docker-compose:ps` | List services and their status | `command-send minion web-01 "docker-compose:ps /opt/myapp"` |
| `docker-compose:up` | Start services (detached mode) | `command-send minion web-01 "docker-compose:up /opt/myapp"` |
| `docker-compose:down` | Stop and remove services | `command-send minion web-01 "docker-compose:down /opt/myapp"` |

#### Docker Compose Command Features

- **Flexible syntax**: Supports both simple string and JSON formats
- **Service targeting**: Start/stop specific services or all services
- **Build support**: Force rebuild images during startup
- **Path validation**: Automatically locates docker-compose.yml/yaml files
- **Error handling**: Comprehensive error reporting with command output

#### Docker Compose Examples

**Simple syntax (recommended for basic operations):**
```bash
# List all services in /opt/myapp
command-send minion web-01 "docker-compose:ps /opt/myapp"

# Start all services
command-send minion web-01 "docker-compose:up /opt/myapp"

# Stop all services
command-send minion web-01 "docker-compose:down /opt/myapp"
```

**JSON syntax (for advanced operations):**
```bash
# Start specific service with rebuild
command-send minion web-01 '{"command": "up", "path": "/opt/myapp", "service": "web", "build": true}'

# Stop specific service only
command-send minion web-01 '{"command": "down", "path": "/opt/myapp", "service": "database"}'

# List services (JSON format)
command-send minion web-01 '{"command": "ps", "path": "/opt/myapp"}'
```

**Deployment scenarios:**
```bash
# Deploy application with rebuild
command-send tag env=staging '{"command": "up", "path": "/opt/myapp", "build": true}'

# Rolling restart of web service
command-send tag role=web '{"command": "down", "path": "/opt/myapp", "service": "web"}'
command-send tag role=web '{"command": "up", "path": "/opt/myapp", "service": "web"}'

# Check status across all servers
command-send all "docker-compose:ps /opt/myapp"
```

#### Docker Compose Command Parameters

| Parameter | Type | Required | Description | Default |
|-----------|------|----------|-------------|---------|
| `path` | string | Yes | Directory containing docker-compose.yml | - |
| `service` | string | No | Specific service name to target | All services |
| `build` | boolean | No | Force rebuild images (up command only) | false |

#### Docker Compose Notes

- Requires `docker-compose` to be installed on the target minion
- Path must contain `docker-compose.yml` or `docker-compose.yaml`
- The `up` command runs in detached mode (`-d`) by default
- The `down` command removes containers and networks when stopping all services
- Service-specific `down` operations use `stop` + `rm` instead of `down`

### Shell Commands

Execute arbitrary shell commands on minions:

| Command | Description | Example |
|---------|-------------|---------|
| `shell <cmd>` | Execute shell command with enhanced logging | `command-send minion web-01 "shell ps aux"` |
| `system <cmd>` | Alias for shell command | `command-send minion web-01 "system df -h"` |
| `<any command>` | Default: execute as shell command | `command-send minion web-01 "whoami"` |

#### Shell Command Features

- **Automatic shell detection**: Uses `/bin/sh` on Unix, `cmd` on Windows
- **Timeout handling**: Default 30-second timeout
- **Full output capture**: Both stdout and stderr captured
- **Exit code reporting**: Success/failure status tracked
- **Execution metadata**: Duration, shell used, timeout status

#### Shell Command Examples

```bash
# System information
command-send all "uname -a"
command-send tag env=prod "uptime"

# Process management
command-send minion web-01 "ps aux | grep nginx"
command-send minion web-01 "systemctl status docker"

# File system operations
command-send all "df -h"
command-send tag role=database "du -sh /var/lib/mysql"

# Network diagnostics  
command-send minion web-01 "netstat -tulpn"
command-send minion web-01 "ping -c 3 google.com"
```

## Interactive Features

### Tab Completion

The console supports intelligent tab completion for:

- **All console commands**: `help`, `version`, `minion-list`, etc.
- **Command aliases**: `h`, `v`, `lm`, `lt`, etc.  
- **Target types**: `all`, `minion`, `tag`
- **Minion IDs**: Dynamically loaded from connected minions
- **Tag keys and values**: Based on current tag database

### Command History

- **Persistent storage**: Saved to `~/.minexus_history`
- **Navigation**: Use ↑/↓ arrow keys to browse history
- **Search**: Ctrl+R for reverse incremental search
- **Auto-save**: Commands automatically added after execution

### Input Handling

- **Signal management**: Ctrl+Z blocked to prevent suspension
- **Standard shortcuts**: Ctrl+C (interrupt), Ctrl+D (EOF) work normally
- **Line editing**: Full readline support with editing capabilities

## Command Status Tracking

### Status States

Commands progress through these states:

| State | Description |
|-------|-------------|
| `PENDING` | Command queued but not yet delivered to minion |
| `RECEIVED` | Command received by minion, queued for execution |
| `EXECUTING` | Command currently running on minion |
| `COMPLETED` | Command finished successfully (exit code 0) |
| `FAILED` | Command finished with error (exit code ≠ 0) |

### Result Retrieval

```bash
# Send a command and note the returned command ID
command-send minion web-01 "ps aux"
# Output: Command sent successfully. Command ID: abc123ef-4567-89ab-cdef-0123456789ab

# Retrieve results using the command ID
result-get abc123ef-4567-89ab-cdef-0123456789ab
```

### Status Monitoring

```bash
# Get overview of all commands
command-status all
# Example output:
# Command Status Summary:
# PENDING: 2
# EXECUTING: 1  
# COMPLETED: 47
# FAILED: 3

# Get detailed status for specific minion
command-status minion web-01
# Shows complete command history with timestamps

# View success statistics
command-status stats
# Shows success rates and average execution times per minion
```

## Error Handling & Validation

### Input Validation

The console performs validation to prevent common errors:

- **Empty commands**: Rejected with helpful error message
- **Invalid minion IDs**: Checked against connected minions
- **Malformed tag syntax**: Must be `key=value` format
- **Path traversal**: File commands reject `../` sequences
- **Hex string detection**: Helpful errors for UUID-like strings

### Timeout Management

- **Default timeouts**: 30 seconds for shell commands  
- **Graceful termination**: Timed-out processes properly killed
- **Timeout reporting**: Clear indication when commands timeout

### Error Messages

Common error scenarios and their messages:

```bash
# Invalid target type
command-send invalid_target "ls"
# Error: Invalid target type. Use: all, minion <id>, or tag <key>=<value>

# Nonexistent minion
command-send minion nonexistent "ls"  
# Error: Minion 'nonexistent' not found

# Malformed tag
command-send tag invalid_format "ls"
# Error: Tag must be in format key=value
```

## Security Considerations

### Authentication & Encryption

- **Mutual TLS (mTLS)**: Console requires client certificate authentication
- **Certificate validation**: Both client and server certificates must be valid
- **Embedded certificates**: TLS certificates built into console binary

### Command Execution

- **No input sanitization**: Commands executed exactly as typed (development tool)
- **Full shell access**: Complete access to minion's shell environment  
- **Path traversal protection**: File operations include basic path validation

### Best Practices

1. **Use specific targeting**: Prefer `minion <id>` over `all` for sensitive operations
2. **Verify before execution**: Use `minion-list` to confirm target minions
3. **Monitor command status**: Check results with `command-status` and `result-get`
4. **Tag management**: Use meaningful tags for efficient minion organization

## Advanced Usage Patterns

### Batch Operations

```bash
# System maintenance across environment
command-send tag env=staging "apt update && apt upgrade -y"
command-send tag env=staging "systemctl restart nginx"
command-send tag env=staging system:info

# Monitoring and diagnostics
command-send all "df -h"
command-send tag role=database "mysqladmin processlist"
command-send tag role=webserver "tail -50 /var/log/nginx/error.log"
```

### Progressive Deployment

```bash
# Deploy to staging first
command-send tag env=staging "systemctl stop myapp"
command-send tag env=staging "file:copy /tmp/myapp-v2.0 /usr/local/bin/myapp"
command-send tag env=staging "systemctl start myapp"

# Verify staging deployment
command-send tag env=staging "systemctl status myapp"
command-send tag env=staging "curl -f http://localhost:8080/health"

# Deploy to production after verification
command-send tag env=prod "systemctl stop myapp"
# ... repeat process
```

### Information Gathering

```bash
# Environment survey
command-send all system:info
command-send all "ps aux | grep -E '(nginx|apache|docker)'"
command-send all "netstat -tulpn | grep LISTEN"

# Security audit
command-send all "find /tmp -type f -perm -4000 2>/dev/null"
command-send all "awk -F: '$3==0 {print $1}' /etc/passwd"
command-send all "systemctl list-units --failed"
```

This comprehensive command reference enables effective administration of distributed systems through the Minexus RAT platform.