package cmd

import (
	"errors"

	"github.com/loomhq/lock-exec/lock"
	"github.com/spf13/cobra"
)

// newRunCmd creates a command for running distributed commands.
func (c *cli) newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "run <key> <command>",
		Short:   "run a command once, using dynamodb as a distributed lock table",
		Example: "lock-exec run examplekey 'echo \"hello world\"'",
		Args:    cobra.ExactArgs(2),

		Run: func(cmd *cobra.Command, args []string) {
			log := c.log.With("key", args[0], "command", args[1])
			locker := c.newLocker()

			log.Info("running command")
			err := locker.Run(c.cmd.Context(), args[0], args[1])
			if err != nil {
				if errors.Is(err, lock.ErrLocked) {
					log.Info("did not run command. key is locked")
					return
				}

				log.Fatalw("command failed", "error", err)
			}

			log.Infow("command succeeded")
		},
	}

	return cmd
}
