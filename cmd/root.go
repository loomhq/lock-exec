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

	cmd.PersistentFlags().StringVarP(&c.region, "region", "r", "", "override region to use for dynamodb table")
	cmd.PersistentFlags().StringVarP(&c.table, "table", "t", "lock-exec", "table name in dynamodb to use for locking")
	cmd.PersistentFlags().DurationVarP(&c.expire, "expire", "e", time.Hour*24, "lock duration in the event that the post-run unlock fails") //nolint:mnd
	cmd.PersistentFlags().StringVar(&c.backend, "backend", "dynamodb", "storage backend to use for locking: dynamodb or ers")
	cmd.PersistentFlags().StringVar(&c.ersEnv, "ers-env", "local", "ERS environment: local, staging or production. Only used when --backend=ers")
	cmd.PersistentFlags().StringVar(&c.ersPartitionID, "ers-partition-id", "", "ERS partition id to write locks into (partitionId from 'atlas tdp get-partitions'); required for staging/production, defaults to the local dev seed. Only used when --backend=ers")
	cmd.PersistentFlags().StringVar(&c.ersRegion, "ers-region", "", "ERS region of the partition (environment from 'atlas tdp get-partitions', e.g. stg-west2), not the cluster's region; required for staging/production. Only used when --backend=ers")
	cmd.PersistentFlags().StringVar(&c.ersEndpoint, "ers-endpoint", "", "override the ERS base URL (defaults to local ERS-in-a-box, or the istio-egress-internal gateway for staging/production). Only used when --backend=ers")

	cmd.AddCommand(
		c.newRunCmd(),
		c.newUnlockCmd(),
		c.newVersionCmd(),
	)

	return cmd
}
