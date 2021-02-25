package utils

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
)

const (
	// Region is default AWS region when none provided
	Region = "us-west-2"

	// TableName is the default table name when none provided
	TableName = "lock-exec"
)

// AWSSession generates an aws api session in us-west-2
func AWSSession(region string) *session.Session {
	return session.Must(session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String(region),
		},
		SharedConfigState: session.SharedConfigEnable,
	}))
}
