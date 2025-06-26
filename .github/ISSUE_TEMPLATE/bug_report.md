---
name: Bug report
about: Create a bug report to help us improve Minexus
title: ''
labels: ''
assignees: ''

---

## Bug Description

**Clear and concise description of what the bug is.**

## Environment

**Please complete the following information:**

- **OS**: [e.g., Ubuntu 22.04, Windows 11, macOS 13.1]
- **Architecture**: [e.g., amd64, arm64]
- **Minexus Version**: [run `./nexus --version` or `./minion --version`]
- **Go Version**: [run `go version`]
- **Docker Version**: [if using Docker, run `docker --version`]
- **Database**: [PostgreSQL version if applicable]

## Component Affected

**Check all that apply:**
- [ ] Nexus Server
- [ ] Minion Client  
- [ ] Console Client
- [ ] Command System
- [ ] Database/Storage
- [ ] TLS/Security
- [ ] Configuration
- [ ] Documentation
- [ ] Build System

## Steps to Reproduce

**Provide detailed steps to reproduce the behavior:**

1. Go to '...'
2. Run command '...'
3. See error

```bash
# Include exact commands used
./nexus --config=config.yaml
./console
command-send all system:info
```

## Expected Behavior

**Clear and concise description of what you expected to happen.**

## Actual Behavior

**Clear and concise description of what actually happened.**

## Error Output

**Include relevant logs, error messages, or stack traces:**

```
Paste error output here
```

**Console/Terminal Output:**
```
Paste console output here
```

**Log Files:**
```
Paste relevant log entries here
```

## Configuration

**Include relevant configuration (remove sensitive information):**

```yaml
# config.yaml or .env contents
NEXUS_SERVER=localhost
NEXUS_MINION_PORT=11972
# ... other relevant config
```

## Testing Information

**Have you tested this with:**
- [ ] Unit tests: `make test`
- [ ] Integration tests: `SLOW_TESTS=1 make test`
- [ ] Manual testing
- [ ] Different environments

**Test results:**
```
# Include test output if relevant
```

## Reproducibility

**How often does this bug occur?**
- [ ] Always (100%)
- [ ] Frequently (75%+)
- [ ] Sometimes (25-75%)
- [ ] Rarely (<25%)
- [ ] Only once

## Additional Context

**Add any other context about the problem here:**

- Does this affect multiple minions or just one?
- Does this happen with specific commands only?
- Are there any patterns you've noticed?
- Have you tried any workarounds?

## Screenshots/Files

**If applicable, add screenshots or attach relevant files:**

- Configuration files (sanitized)
- Log files
- Screenshots of error messages
- Network traces (if network-related)

## Possible Solution

**If you have ideas for fixing the bug, please describe:**
