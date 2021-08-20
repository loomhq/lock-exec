package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// Version is defined at compile time via -ldflags.
	Version = "undefined1"
)

// unlockCmd represents the unlock command.
var versionkCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",

	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Version)
	},
}

func init() {
	rootCmd.AddCommand(versionkCmd)
}
