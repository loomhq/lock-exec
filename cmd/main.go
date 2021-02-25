package cmd

import (
	"fmt"

	"github.com/loomhq/lock-exec/utils"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var keyName string
var tableName string
var regionName string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "lock-exec",
	Short: "A CLI tool for running any shell based commands in a distributed environment with DynamoDB locking",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	logrus.SetFormatter(&logrus.JSONFormatter{})
	tableNameMsg := fmt.Sprintf("Table Name (default: \"%s\")", utils.TableName)
	regionMsg := fmt.Sprintf("AWS Region Name (default: \"%s\")", utils.Region)

	rootCmd.PersistentFlags().StringVarP(&tableName, "table", "t", utils.TableName, tableNameMsg)
	rootCmd.PersistentFlags().StringVarP(&regionName, "region", "r", utils.Region, regionMsg)
	rootCmd.PersistentFlags().StringVarP(&keyName, "key", "k", "", "Name of the key (required)")

	unlockCmd.MarkPersistentFlagRequired("key") //nolint
}
