package cmd

import (
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/loomhq/lock-exec/lock"
)

// newLocker returns a new lock client or logs and exits on failure.
func (c *cli) newLocker() *lock.Client {
	cfg, err := config.LoadDefaultConfig(c.cmd.Context())
	c.fatalErr(err, "failed to load aws config")

	return lock.New(dynamodb.NewFromConfig(cfg), c.table)
}

// fatalErr logs the message and error and then exits if the error is not nil.
func (c *cli) fatalErr(err error, msg string) {
	if err == nil {
		return
	}

	c.log.Fatalf("%s: %v", err, msg)
}
