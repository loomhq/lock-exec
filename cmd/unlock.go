package cmd

import (
	"github.com/spf13/cobra"
)

// newUnlockCmd creates a command for unlocking locked keys.
func (c *cli) newUnlockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "unlock <key>",
		Short:   "unlock a currently locked key",
		Example: "lock-exec unlock examplekey",
		Args:    cobra.ExactArgs(1),

		Run: func(cmd *cobra.Command, args []string) {
			locker := c.newLocker()

			err := locker.Unlock(c.cmd.Context(), args[0])
			c.fatalErr(err, "failed to unlock key")
		},
	}

	return cmd
}
