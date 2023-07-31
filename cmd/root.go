package cmd

import (
	"time"

	"github.com/spf13/cobra"
)

// newRootCmd creates our base cobra command to add all subcommands to.
func (c *cli) newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock-exec",
		Short: "a tool for running single commands in distributed environments using dynamodb for locking",

		// prevents docs from adding promotional message footer
		DisableAutoGenTag: true,
	}

	cmd.PersistentFlags().StringVarP(&c.table, "table", "t", "lock-exec", "table name in dynamodb to use for locking")
	cmd.PersistentFlags().DurationVarP(&c.expire, "expire", "e", time.Hour*24, "lock duration in the event that the post-run unlock fails") //nolint:gomnd

	cmd.AddCommand(
		c.newRunCmd(),
		c.newUnlockCmd(),
		c.newVersionCmd(),
	)

	return cmd
}
