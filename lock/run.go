package lock

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Run acquires a lock under the specified id, executes the command, and then unlocks the id.
// Returns ErrLocked if the id is already locked. Otherwise returns combined stdout and stderr
// of the command and the command error. If the unlock step fails the lock expires after 24 hours.
func (c *Client) Run(ctx context.Context, id, command string) (string, error) {
	// use context.Background here so that unlock runs even if the context is cancelled
	defer c.Unlock(context.Background(), id) //nolint:errcheck

	err := c.Lock(ctx, id, time.Hour*24) //nolint:gomnd
	if err != nil {
		return "", fmt.Errorf("lock failed: %w", err)
	}

	fields := strings.Fields(command)
	cmdout, cmderr := exec.CommandContext(ctx, fields[0], fields[1:]...).CombinedOutput() //nolint:gosec

	return string(cmdout), cmderr
}
