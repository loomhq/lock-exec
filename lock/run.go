package lock

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Run acquires a lock under the specified key, executes the command, and then unlocks the key.
// Returns ErrLocked if the key is already locked. Otherwise returns combined stdout and stderr
// of the command and the command error. If the unlock step fails the lock expires after 24 hours.
func (c *Client) Run(ctx context.Context, key, command string) error {
	// use context.Background here so that unlock runs even if the context is cancelled
	defer c.Unlock(context.Background(), key) //nolint:errcheck,contextcheck

	err := c.Lock(ctx, key, c.expire)
	if err != nil {
		return fmt.Errorf("lock failed: %w", err)
	}

	// Build command
	fields := strings.Fields(command)
	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...) //nolint:gosec

	// Write to std outputs
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}
	return nil
}
