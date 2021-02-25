package cmd

import (
	"github.com/loomhq/lock-exec/lock"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// unlockCmd represents the unlock command
var unlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Force release an already acquired lock using key name",
	Run: func(cmd *cobra.Command, args []string) {
		c := lock.NewDynamoClient(regionName)
		if err := c.ReleaseLock(keyName, tableName); err != nil {
			logrus.Fatal(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(unlockCmd)
}
