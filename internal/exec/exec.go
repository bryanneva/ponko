// Package exec defines the CommandExecutor interface for shelling out to CLI tools.
package exec

import "context"

// CommandExecutor wraps exec.Command for testability.
type CommandExecutor interface {
	Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}
