package cmd

import (
	"github.com/loomhq/lock-exec/exec"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var shellCommand string
var sleepStartRandom int
var holdLockBy int

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a shell command with acquire lock",
	Long: `Runs your supplied shell command by acquiring a lock
from DynamoDB table. At the end of operation it releases the
log and return exit code of the command.`,
	Run: func(cmd *cobra.Command, args []string) {
		tableName, err := cmd.Flags().GetString("table")
		if err != nil {
			logrus.Fatal(err)
		}

		regionName, err := cmd.Flags().GetString("region")
		if err != nil {
			logrus.Fatal(err)
		}

		e, err := exec.NewDynamoClient(regionName)
		if err != nil {
			logrus.Fatal(err)
		}

		if err := e.Run(tableName, keyName, shellCommand, sleepStartRandom, holdLockBy); err != nil {
			logrus.Fatal(err)
		}
	},
}

func init() {
	runCmd.Flags().StringVarP(&shellCommand, "command", "c", "", "Shell Command (required)")
	if err := runCmd.MarkFlagRequired("command"); err != nil {
		logrus.Fatal(err)
	}

	runCmd.Flags().IntVarP(&sleepStartRandom, "sleep-start-random", "s", 0, "Adds a randomized sleep before running the command to add jitter like effect. Value in seconds and is the upper bound for the randomized sleep duration.")
	runCmd.Flags().IntVarP(&holdLockBy, "hold-lock", "l", 0, "Adds a sleep after running the command and before releasing the lock.")

	rootCmd.AddCommand(runCmd)
}
