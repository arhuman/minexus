package command

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// ExecutionContext provides common context for all command executions
type ExecutionContext struct {
	Context     context.Context
	Logger      *zap.Logger
	AtomicLevel *zap.AtomicLevel
	MinionID    string
	CommandID   string
	Timestamp   int64
}

// NewExecutionContext creates a new execution context
func NewExecutionContext(ctx context.Context, logger *zap.Logger, atom *zap.AtomicLevel, minionID, commandID string) *ExecutionContext {
	return &ExecutionContext{
		Context:     ctx,
		Logger:      logger,
		AtomicLevel: atom,
		MinionID:    minionID,
		CommandID:   commandID,
		Timestamp:   time.Now().Unix(),
	}
}

// Definition represents information about a command that can be sent to minions
type Definition struct {
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	Description string    `json:"description"`
	Usage       string    `json:"usage"`
	Examples    []Example `json:"examples,omitempty"`
	Parameters  []Param   `json:"parameters,omitempty"`
	Notes       []string  `json:"notes,omitempty"`
}

// Example represents an example of how to use a command
type Example struct {
	Description string `json:"description"`
	Command     string `json:"command"`
	Expected    string `json:"expected,omitempty"`
}

// Param represents a command parameter
type Param struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Default     string `json:"default,omitempty"`
}
