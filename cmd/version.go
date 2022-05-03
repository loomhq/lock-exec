package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCmd creates a new command that prints the version.
func (c *cli) newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "version",
		Short:   "print the current version",
		Example: "lock-exec version",
		Args:    cobra.NoArgs,

		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(c.version)
		},
	}

	return cmd
}
