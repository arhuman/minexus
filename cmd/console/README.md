# Minexus Console

The Minexus Console is an interactive REPL (Read-Eval-Print Loop) application that allows you to communicate with the Nexus server and manage minions.

## Features

- Interactive command-line interface
- List and manage connected minions
- Send commands to minions (all, specific, or by tags)
- View command execution results
- Manage minion tags
- Real-time communication with the Nexus server

## Building

Build the console application using the Makefile:

```bash
make console
```

Or build manually:

```bash
go build -o console ./cmd/console/
```

## Configuration

The console can be configured through environment variables, `.env` file, or command-line flags:

### Environment Variables

- `NEXUS_SERVER` - Nexus server address (default: "localhost:11972")
- `CONNECT_TIMEOUT` - Connection timeout in seconds (default: 3)
- `DEBUG` - Enable debug logging (default: false)

### Command-line Flags

```bash
./console -server localhost:11972 -timeout 10 -debug
```

- `-server, --server` - Nexus server address
- `-timeout, --timeout` - Connection timeout in seconds
- `-debug, --debug` - Enable debug mode

## Usage

Start the console:

```bash
./console
```

## Available Commands

### Basic Commands

- `help`, `h` - Show help message
- `clear` - Clear screen
- `quit`, `exit` - Exit the console

### Minion Management

- `minion-list`, `lm` - List all connected minions
- `tag-list`, `lt` - List all available tags

### Command Execution

Send commands to minions:

```bash
# Send to all minions
command-send all ls -la

# Send to specific minion
command-send minion <minion-id> ls -la

# Send to minions with specific tag
command-send tag environment=production uptime
```

Get command results:

```bash
result-get <command-id>
```

### Tag Management

Set tags for a minion (replaces all existing tags):

```bash
tag-set <minion-id> environment=production role=webserver
```

Update tags for a minion (add/remove specific tags):

```bash
# Add tags
tag-update <minion-id> +environment=staging +role=database

# Remove tags
tag-update <minion-id> -old_tag

# Mix add and remove
tag-update <minion-id> +new_env=test -old_env
```

## Examples

### Basic Workflow

1. Start the console:
   ```bash
   ./console
   ```

2. List connected minions:
   ```bash
   minexus> minion-list
   ```

3. Send a command to all minions:
   ```bash
   minexus> command-send whoami
   ```

4. Get command results:
   ```bash
   minexus> result-get 123e4567-e89b-12d3-a456-426614174000
   ```

### Tag-based Targeting

1. Set tags on minions:
   ```bash
   minexus> tag-set abc123 environment=production role=webserver
   minexus> tag-set def456 environment=staging role=database
   ```

2. Send commands to specific environments:
   ```bash
   minexus> command-send tag environment=production systemctl status nginx
   minexus> command-send tag role=database ps aux | grep postgres
   ```

### Advanced Tag Management

```bash
# List all available tags
minexus> tag-list

# Update tags incrementally
minexus> tag-update abc123 +region=us-east-1 +datacenter=dc1 -old_region

# Set multiple tags at once
minexus> tag-set def456 environment=production role=api region=eu-west-1
```

## Command Output Format

### Minion List
```
Connected minions (2):
ID                                   | Hostname          | IP             | OS       | Tags
------------------------------------ | ----------------- | -------------- | -------- | ----
abc123-def4-5678-9012-345678901234  | web-server-01     | 192.168.1.100  | linux    | env=prod, role=web
def456-abc7-8901-2345-678901234567  | db-server-01      | 192.168.1.101  | linux    | env=prod, role=db
```

### Command Results
```
Command results (2):
Minion ID                            | Exit Code | Output
------------------------------------ | --------- | ------
abc123-def4-5678-9012-345678901234  | 0         | root [15:04:05]
def456-abc7-8901-2345-678901234567  | 0         | postgres [15:04:06]
```

## Error Handling

- Connection errors: The console will display error messages if it cannot connect to the Nexus server
- Command errors: Failed commands will show appropriate error messages
- Invalid syntax: The console provides usage hints for incorrect command syntax

## Integration with Nexus

The console communicates with the Nexus server using the gRPC `ConsoleService` interface, providing:

- Real-time minion status
- Command dispatch and result retrieval
- Tag management operations
- Secure communication over gRPC

## Troubleshooting

### Connection Issues

1. Verify Nexus server is running:
   ```bash
   ./nexus
   ```

2. Check server address and port:
   ```bash
   ./console -server localhost:11972 -debug
   ```

3. Test connectivity:
   ```bash
   grpcurl -plaintext localhost:11972 list
   ```

### Command Issues

- Ensure minions are connected and registered
- Check command syntax and arguments
- Use `minion-list` to verify available targets
- Check command results with `result-get <command-id>`
