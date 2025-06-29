# Adding Commands Using the Simplified Command System

## Overview

This guide explains how to add new commands using Minexus's command system. The system provides a clean, self-contained architecture that eliminates boilerplate code and makes commands easy to write, test, and maintain.

## System Architecture

The command system consists of:

- **`ExecutableCommand` interface**: Single interface requiring `Execute()` and `Metadata()` methods
- **`BaseCommand`**: Embedded struct providing common functionality and metadata handling
- **`Registry`**: Simple map-based command storage and execution system
- **Individual command structs**: Each command embeds `*BaseCommand` and implements business logic

## Step-by-Step Guide

### Step 1: Create Your Command File

Create a new file for your command category. For this example, we'll create fun commands:

```bash
touch internal/command/fun_commands.go
```

### Step 2: Define Your Commands

Each command follows this pattern:

```go
package command

import (
    "fmt"
    pb "minexus/protogen"
)

// JokeCommand tells a random joke
type JokeCommand struct {
    *BaseCommand
}

// NewJokeCommand creates a new joke command
func NewJokeCommand() *JokeCommand {
    base := NewBaseCommand(
        "fun:joke",           // Command name (must be unique)
        "fun",               // Category for grouping
        "Tell a random programming joke", // Description
        "fun:joke",          // Usage pattern
    ).WithExamples(
        Example{
            Description: "Get a random programming joke",
            Command:     "command-send all fun:joke",
            Expected:    "Returns a funny programming joke",
        },
    ).WithNotes(
        "Jokes are randomly selected from a curated collection",
        "All jokes are family-friendly and programming-related",
    )

    return &JokeCommand{
        BaseCommand: base,
    }
}

// Execute implements ExecutableCommand interface
func (c *JokeCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
    jokes := []string{
        "Why do programmers prefer dark mode? Because light attracts bugs!",
        "How many programmers does it take to change a light bulb? None, that's a hardware problem.",
        "Why do Java developers wear glasses? Because they can't C#!",
        "What's a programmer's favorite hangout place? Foo Bar!",
        "Why did the programmer quit his job? He didn't get arrays!",
    }

    // Simple random selection (in production, use crypto/rand for true randomness)
    selectedJoke := jokes[len(jokes)%5] // This is just for demo
    
    return c.BaseCommand.CreateSuccessResult(ctx, selectedJoke), nil
}
```

### Step 3: Add Commands with Parameters

Here's an example with parameters:

```go
// FortuneCommand generates a custom fortune message
type FortuneCommand struct {
    *BaseCommand
}

// NewFortuneCommand creates a new fortune command
func NewFortuneCommand() *FortuneCommand {
    base := NewBaseCommand(
        "fun:fortune",
        "fun",
        "Generate a personalized fortune message",
        "fun:fortune [name]",
    ).WithParameters(
        Param{
            Name:        "name",
            Type:        "string",
            Required:    false,
            Description: "Name to personalize the fortune",
            Default:     "Brave Developer",
        },
    ).WithExamples(
        Example{
            Description: "Get a fortune with default name",
            Command:     "command-send all fun:fortune",
            Expected:    "Returns a fortune for 'Brave Developer'",
        },
        Example{
            Description: "Get a personalized fortune",
            Command:     "command-send all fun:fortune Alice",
            Expected:    "Returns a fortune personalized for Alice",
        },
    ).WithNotes(
        "If no name is provided, defaults to 'Brave Developer'",
        "Fortune messages are inspirational and development-focused",
        "Name parameter is optional and can be any string",
    )

    return &FortuneCommand{
        BaseCommand: base,
    }
}

// Execute implements ExecutableCommand interface
func (c *FortuneCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
    // Parse the command payload to extract parameters
    parts := strings.Fields(payload)
    name := "Brave Developer" // default
    
    if len(parts) > 1 {
        name = parts[1]
    }

    fortunes := []string{
        "Today will bring great debugging victories, %s!",
        "A breakthrough solution will come to you during coffee break, %s.",
        "Your code will compile on the first try today, %s!",
        "You will discover an elegant algorithm, %s.",
        "A helpful teammate will appear when you need them most, %s.",
    }

    selectedFortune := fmt.Sprintf(fortunes[0], name) // Demo selection
    
    return c.BaseCommand.CreateSuccessResult(ctx, selectedFortune), nil
}
```

### Step 4: Add Commands with Required Parameters

Here's an example with a required parameter:

```go
// JokeAboutNameCommand tells a personalized joke about a name
type JokeAboutNameCommand struct {
    *BaseCommand
}

// NewJokeAboutNameCommand creates a new joke about name command
func NewJokeAboutNameCommand() *JokeAboutNameCommand {
    base := NewBaseCommand(
        "fun:jokeaboutname",
        "fun",
        "Tell a personalized joke about someone's name",
        "fun:jokeaboutname <name>",
    ).WithParameters(
        Param{
            Name:        "name",
            Type:        "string",
            Required:    true,
            Description: "The name to create a joke about",
        },
    ).WithExamples(
        Example{
            Description: "Tell a joke about Alice",
            Command:     "command-send all fun:jokeaboutname Alice",
            Expected:    "Returns a personalized joke about Alice",
        },
        Example{
            Description: "Tell a joke about Bob",
            Command:     "command-send all fun:jokeaboutname Bob",
            Expected:    "Returns a personalized joke about Bob",
        },
    ).WithNotes(
        "Name parameter is required",
        "Jokes are generated based on common name patterns and programming humor",
    )

    return &JokeAboutNameCommand{
        BaseCommand: base,
    }
}

// Execute implements ExecutableCommand interface
func (c *JokeAboutNameCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
    parts := strings.Fields(payload)
    
    if len(parts) < 2 {
        return c.BaseCommand.CreateErrorResult(ctx,
            fmt.Errorf("name parameter is required. Usage: fun:jokeaboutname <name>")), nil
    }

    name := parts[1]
    
    // Generate name-based jokes
    jokes := map[string]string{
        "alice":   "Why did Alice break up with her debugger? Because it kept saying 'Alice has encountered an unexpected error!'",
        "bob":     "Bob's code is so clean, even his bugs are well-documented!",
        "charlie": "Charlie tried to name a variable 'Charlie' but the compiler said 'That's not a valid variable, that's a person!'",
        "default": "%s's code is like a joke - sometimes you get it, sometimes you don't, but it's always interesting!",
    }

    nameLower := strings.ToLower(name)
    joke, exists := jokes[nameLower]
    if !exists {
        joke = fmt.Sprintf(jokes["default"], name)
    } else if nameLower != "default" {
        // For specific name jokes, we don't need formatting
        joke = jokes[nameLower]
    }

    return c.BaseCommand.CreateSuccessResult(ctx, joke), nil
}
```

### Step 5: Add Error Handling

Example with comprehensive error handling:

```go
// RockPaperScissorsCommand plays rock paper scissors
type RockPaperScissorsCommand struct {
    *BaseCommand
}

// NewRockPaperScissorsCommand creates a new rock paper scissors command
func NewRockPaperScissorsCommand() *RockPaperScissorsCommand {
    base := NewBaseCommand(
        "fun:rps",
        "fun",
        "Play rock paper scissors against the computer",
        "fun:rps <rock|paper|scissors>",
    ).WithParameters(
        Param{
            Name:        "choice",
            Type:        "string",
            Required:    true,
            Description: "Your choice: rock, paper, or scissors",
        },
    ).WithExamples(
        Example{
            Description: "Play rock",
            Command:     "command-send all fun:rps rock",
            Expected:    "Computer plays and shows result",
        },
        Example{
            Description: "Play paper",
            Command:     "command-send all fun:rps paper",
            Expected:    "Computer plays and shows result",
        },
    ).WithNotes(
        "Computer choice is randomly generated",
        "Valid choices are: rock, paper, scissors (case-insensitive)",
    )

    return &RockPaperScissorsCommand{
        BaseCommand: base,
    }
}

// Execute implements ExecutableCommand interface
func (c *RockPaperScissorsCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
    parts := strings.Fields(payload)
    
    if len(parts) < 2 {
        return c.BaseCommand.CreateErrorResult(ctx, 
            fmt.Errorf("missing choice. Usage: fun:rps <rock|paper|scissors>")), nil
    }

    playerChoice := strings.ToLower(parts[1])
    validChoices := map[string]bool{"rock": true, "paper": true, "scissors": true}
    
    if !validChoices[playerChoice] {
        return c.BaseCommand.CreateErrorResult(ctx, 
            fmt.Errorf("invalid choice '%s'. Valid choices: rock, paper, scissors", parts[1])), nil
    }

    computerChoices := []string{"rock", "paper", "scissors"}
    computerChoice := computerChoices[0] // Demo - use proper random in production

    result := determineWinner(playerChoice, computerChoice)
    output := fmt.Sprintf("You played: %s\nComputer played: %s\nResult: %s", 
        playerChoice, computerChoice, result)

    return c.BaseCommand.CreateSuccessResult(ctx, output), nil
}

// Helper function
func determineWinner(player, computer string) string {
    if player == computer {
        return "It's a tie!"
    }
    
    winConditions := map[string]string{
        "rock":     "scissors",
        "paper":    "rock",
        "scissors": "paper",
    }
    
    if winConditions[player] == computer {
        return "You win!"
    }
    return "Computer wins!"
}
```

### Step 6: Register Your Commands

Add your commands to [`internal/command/setup.go`](internal/command/setup.go):

```go
func SetupCommands() *Registry {
    registry := NewRegistry()

    // ... existing registrations ...

    // Register fun commands
    registry.Register(NewJokeCommand())
    registry.Register(NewFortuneCommand())
    registry.Register(NewJokeAboutNameCommand())
    registry.Register(NewRockPaperScissorsCommand())

    return registry
}
```

## Complete Example File

Here's the complete `internal/command/fun_commands.go` file:

```go
package command

import (
    "fmt"
    "strings"
    pb "minexus/protogen"
)

// JokeCommand tells a random joke
type JokeCommand struct {
    *BaseCommand
}

// NewJokeCommand creates a new joke command
func NewJokeCommand() *JokeCommand {
    base := NewBaseCommand(
        "fun:joke",
        "fun",
        "Tell a random programming joke",
        "fun:joke",
    ).WithExamples(
        Example{
            Description: "Get a random programming joke",
            Command:     "command-send all fun:joke",
            Expected:    "Returns a funny programming joke",
        },
    ).WithNotes(
        "Jokes are randomly selected from a curated collection",
        "All jokes are family-friendly and programming-related",
    )

    return &JokeCommand{BaseCommand: base}
}

// Execute implements ExecutableCommand interface
func (c *JokeCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
    jokes := []string{
        "Why do programmers prefer dark mode? Because light attracts bugs!",
        "How many programmers does it take to change a light bulb? None, that's a hardware problem.",
        "Why do Java developers wear glasses? Because they can't C#!",
        "What's a programmer's favorite hangout place? Foo Bar!",
        "Why did the programmer quit his job? He didn't get arrays!",
    }

    selectedJoke := jokes[0] // Demo - implement proper random selection
    return c.BaseCommand.CreateSuccessResult(ctx, selectedJoke), nil
}

// FortuneCommand generates a custom fortune message
type FortuneCommand struct {
    *BaseCommand
}

// NewFortuneCommand creates a new fortune command
func NewFortuneCommand() *FortuneCommand {
    base := NewBaseCommand(
        "fun:fortune",
        "fun",
        "Generate a personalized fortune message",
        "fun:fortune [name]",
    ).WithParameters(
        Param{
            Name:        "name",
            Type:        "string",
            Required:    false,
            Description: "Name to personalize the fortune",
            Default:     "Brave Developer",
        },
    ).WithExamples(
        Example{
            Description: "Get a fortune with default name",
            Command:     "command-send all fun:fortune",
            Expected:    "Returns a fortune for 'Brave Developer'",
        },
        Example{
            Description: "Get a personalized fortune",
            Command:     "command-send all fun:fortune Alice",
            Expected:    "Returns a fortune personalized for Alice",
        },
    ).WithNotes(
        "If no name is provided, defaults to 'Brave Developer'",
        "Fortune messages are inspirational and development-focused",
        "Name parameter is optional and can be any string",
    )

    return &FortuneCommand{BaseCommand: base}
}

// Execute implements ExecutableCommand interface
func (c *FortuneCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
    parts := strings.Fields(payload)
    name := "Brave Developer"
    
    if len(parts) > 1 {
        name = parts[1]
    }

    fortune := fmt.Sprintf("Today will bring great debugging victories, %s!", name)
    return c.BaseCommand.CreateSuccessResult(ctx, fortune), nil
}

// JokeAboutNameCommand tells a personalized joke about a name
type JokeAboutNameCommand struct {
    *BaseCommand
}

// NewJokeAboutNameCommand creates a new joke about name command
func NewJokeAboutNameCommand() *JokeAboutNameCommand {
    base := NewBaseCommand(
        "fun:jokeaboutname",
        "fun",
        "Tell a personalized joke about someone's name",
        "fun:jokeaboutname <name>",
    ).WithParameters(
        Param{
            Name:        "name",
            Type:        "string",
            Required:    true,
            Description: "The name to create a joke about",
        },
    ).WithExamples(
        Example{
            Description: "Tell a joke about Alice",
            Command:     "command-send all fun:jokeaboutname Alice",
            Expected:    "Returns a personalized joke about Alice",
        },
        Example{
            Description: "Tell a joke about Bob",
            Command:     "command-send all fun:jokeaboutname Bob",
            Expected:    "Returns a personalized joke about Bob",
        },
    ).WithNotes(
        "Name parameter is required",
        "Jokes are generated based on common name patterns and programming humor",
    )

    return &JokeAboutNameCommand{BaseCommand: base}
}

// Execute implements ExecutableCommand interface
func (c *JokeAboutNameCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
    parts := strings.Fields(payload)
    
    if len(parts) < 2 {
        return c.BaseCommand.CreateErrorResult(ctx,
            fmt.Errorf("name parameter is required. Usage: fun:jokeaboutname <name>")), nil
    }

    name := parts[1]
    
    // Generate name-based jokes
    jokes := map[string]string{
        "alice":   "Why did Alice break up with her debugger? Because it kept saying 'Alice has encountered an unexpected error!'",
        "bob":     "Bob's code is so clean, even his bugs are well-documented!",
        "charlie": "Charlie tried to name a variable 'Charlie' but the compiler said 'That's not a valid variable, that's a person!'",
        "default": "%s's code is like a joke - sometimes you get it, sometimes you don't, but it's always interesting!",
    }

    nameLower := strings.ToLower(name)
    joke, exists := jokes[nameLower]
    if !exists {
        joke = fmt.Sprintf(jokes["default"], name)
    }

    return c.BaseCommand.CreateSuccessResult(ctx, joke), nil
}

// RockPaperScissorsCommand plays rock paper scissors
type RockPaperScissorsCommand struct {
    *BaseCommand
}

// NewRockPaperScissorsCommand creates a new rock paper scissors command
func NewRockPaperScissorsCommand() *RockPaperScissorsCommand {
    base := NewBaseCommand(
        "fun:rps",
        "fun",
        "Play rock paper scissors against the computer",
        "fun:rps <rock|paper|scissors>",
    ).WithParameters(
        Param{
            Name:        "choice",
            Type:        "string",
            Required:    true,
            Description: "Your choice: rock, paper, or scissors",
        },
    ).WithExamples(
        Example{
            Description: "Play rock",
            Command:     "command-send all fun:rps rock",
            Expected:    "Computer plays and shows result",
        },
    ).WithNotes(
        "Computer choice is randomly generated",
        "Valid choices are: rock, paper, scissors (case-insensitive)",
    )

    return &RockPaperScissorsCommand{BaseCommand: base}
}

// Execute implements ExecutableCommand interface
func (c *RockPaperScissorsCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
    parts := strings.Fields(payload)
    
    if len(parts) < 2 {
        return c.BaseCommand.CreateErrorResult(ctx,
            fmt.Errorf("missing choice. Usage: fun:rps <rock|paper|scissors>")), nil
    }

    playerChoice := strings.ToLower(parts[1])
    validChoices := map[string]bool{"rock": true, "paper": true, "scissors": true}
    
    if !validChoices[playerChoice] {
        return c.BaseCommand.CreateErrorResult(ctx,
            fmt.Errorf("invalid choice '%s'. Valid choices: rock, paper, scissors", parts[1])), nil
    }

    computerChoice := "rock" // Demo - implement proper random selection
    result := determineWinner(playerChoice, computerChoice)
    output := fmt.Sprintf("You played: %s\nComputer played: %s\nResult: %s",
        playerChoice, computerChoice, result)

    return c.BaseCommand.CreateSuccessResult(ctx, output), nil
}

// determineWinner determines the winner of rock paper scissors
func determineWinner(player, computer string) string {
    if player == computer {
        return "It's a tie!"
    }
    
    winConditions := map[string]string{
        "rock":     "scissors",
        "paper":    "rock",
        "scissors": "paper",
    }
    
    if winConditions[player] == computer {
        return "You win!"
    }
    return "Computer wins!"
}
```

## Testing Your Commands

Create unit tests for your commands:

```go
// internal/command/fun_commands_test.go
package command

import (
    "context"
    "testing"
    "go.uber.org/zap"
    "github.com/stretchr/testify/assert"
)

func TestJokeCommand(t *testing.T) {
    cmd := NewJokeCommand()
    
    // Test metadata
    metadata := cmd.Metadata()
    assert.Equal(t, "fun:joke", metadata.Name)
    assert.Equal(t, "fun", metadata.Category)
    assert.NotEmpty(t, metadata.Description)

    // Test execution
    logger := zap.NewNop()
    atomicLevel := zap.NewAtomicLevel()
    ctx := NewExecutionContext(context.Background(), logger, &atomicLevel, "test-minion", "test-cmd")
    
    result, err := cmd.Execute(ctx, "fun:joke")
    
    assert.NoError(t, err)
    assert.Equal(t, int32(0), result.ExitCode)
    assert.NotEmpty(t, result.Stdout)
    assert.Empty(t, result.Stderr)
}

func TestFortuneCommand(t *testing.T) {
    cmd := NewFortuneCommand()
    logger := zap.NewNop()
    atomicLevel := zap.NewAtomicLevel()
    ctx := NewExecutionContext(context.Background(), logger, &atomicLevel, "test-minion", "test-cmd")

    // Test with default name
    result, err := cmd.Execute(ctx, "fun:fortune")
    assert.NoError(t, err)
    assert.Contains(t, result.Stdout, "Brave Developer")

    // Test with custom name
    result, err = cmd.Execute(ctx, "fun:fortune Alice")
    assert.NoError(t, err)
    assert.Contains(t, result.Stdout, "Alice")
}

func TestJokeAboutNameCommand(t *testing.T) {
    cmd := NewJokeAboutNameCommand()
    logger := zap.NewNop()
    atomicLevel := zap.NewAtomicLevel()
    ctx := NewExecutionContext(context.Background(), logger, &atomicLevel, "test-minion", "test-cmd")

    // Test with known name
    result, err := cmd.Execute(ctx, "fun:jokeaboutname Alice")
    assert.NoError(t, err)
    assert.Equal(t, int32(0), result.ExitCode)
    assert.Contains(t, result.Stdout, "Alice")

    // Test with unknown name (should use default template)
    result, err = cmd.Execute(ctx, "fun:jokeaboutname Unknown")
    assert.NoError(t, err)
    assert.Equal(t, int32(0), result.ExitCode)
    assert.Contains(t, result.Stdout, "Unknown")

    // Test missing name parameter
    result, err = cmd.Execute(ctx, "fun:jokeaboutname")
    assert.NoError(t, err)
    assert.Equal(t, int32(1), result.ExitCode)
    assert.Contains(t, result.Stderr, "name parameter is required")
}

func TestRockPaperScissorsCommand(t *testing.T) {
    cmd := NewRockPaperScissorsCommand()
    logger := zap.NewNop()
    atomicLevel := zap.NewAtomicLevel()
    ctx := NewExecutionContext(context.Background(), logger, &atomicLevel, "test-minion", "test-cmd")

    // Test valid input
    result, err := cmd.Execute(ctx, "fun:rps rock")
    assert.NoError(t, err)
    assert.Equal(t, int32(0), result.ExitCode)
    assert.Contains(t, result.Stdout, "You played: rock")

    // Test invalid input
    result, err = cmd.Execute(ctx, "fun:rps invalid")
    assert.NoError(t, err) // Command returns error in result, not as Go error
    assert.Equal(t, int32(1), result.ExitCode)
    assert.Contains(t, result.Stderr, "invalid choice")

    // Test missing input
    result, err = cmd.Execute(ctx, "fun:rps")
    assert.NoError(t, err)
    assert.Equal(t, int32(1), result.ExitCode)
    assert.Contains(t, result.Stderr, "missing choice")
}
```

## Best Practices

### 1. Command Naming
- Use `category:action` format (e.g., `fun:joke`, `system:info`)
- Keep names short but descriptive
- Use lowercase with hyphens for multi-word actions (e.g., `fun:magic-8ball`)

### 2. Error Handling
- Use [`BaseCommand.CreateErrorResult()`](internal/command/base.go:74) for user errors
- Use [`BaseCommand.CreateErrorResultWithCode()`](internal/command/base.go:85) for custom exit codes
- Always return `nil` as the Go error from `Execute()` - put errors in the result

### 3. Parameter Parsing
- Always validate input parameters
- Provide clear error messages for invalid input
- Support optional parameters with sensible defaults

### 4. Documentation
- Add comprehensive examples showing expected output
- **Always include notes** using [`WithNotes()`](internal/command/base.go:57) for special behavior, limitations, or usage tips
- Document all parameters with types and descriptions
- Notes appear in help output and provide important context for users

### 5. Testing
- Test both success and error cases
- Test parameter validation
- Test edge cases and boundary conditions

## Integration

Once your commands are implemented and tested:

1. **Add to setup**: Register in [`SetupCommands()`](internal/command/setup.go:4)
2. **Run tests**: `go test ./internal/command/...`
3. **Build and test**: Test with a running minion
4. **Update documentation**: Add to [`COMMANDS.md`](documentation/COMMANDS.md) if needed

## Advanced Features

### Adding Command Notes
Always add notes to provide important context and usage information:

```go
base := NewBaseCommand(
    "my:command",
    "category",
    "Command description",
    "my:command [options]",
).WithNotes(
    "This command requires network connectivity",
    "Results are cached for 5 minutes",
    "Use with caution in production environments",
    "Supports both IPv4 and IPv6 addresses",
)
```

### Custom Result Types
```go
func (c *MyCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
    // Custom result with specific exit code
    return &pb.CommandResult{
        CommandId: ctx.CommandID,
        MinionId:  ctx.MinionID,
        Timestamp: ctx.Timestamp,
        ExitCode:  42,
        Stdout:    "Custom output",
        Stderr:    "Custom error info",
    }, nil
}
```

### Using Context
```go
func (c *MyCommand) Execute(ctx *ExecutionContext, payload string) (*pb.CommandResult, error) {
    // Access logger
    ctx.Logger.Info("Executing command", zap.String("payload", payload))
    
    // Check for cancellation
    select {
    case <-ctx.Context.Done():
        return c.BaseCommand.CreateErrorResult(ctx, ctx.Context.Err()), nil
    default:
        // Continue execution
    }
    
    // Access minion ID
    output := fmt.Sprintf("Command executed on minion: %s", ctx.MinionID)
    return c.BaseCommand.CreateSuccessResult(ctx, output), nil
}
```

## Summary

The command system makes adding new commands straightforward:

1. **Create command struct** embedding `*BaseCommand`
2. **Implement constructor** with metadata using fluent API
3. **Implement Execute method** with business logic
4. **Register command** in setup function
5. **Add tests** to verify functionality

This approach eliminates boilerplate, provides type safety, and makes commands self-documenting while maintaining consistency across the codebase.