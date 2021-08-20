package lock

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/loomhq/lock-exec/utils"
	"github.com/sirupsen/logrus"
)

// Dynamo is a struct used to work with dynamo client interface.
type Dynamo struct {
	dynamodbiface.DynamoDBAPI
}

// NewDynamoClient returns a Dynamo struct with client session.
func NewDynamoClient(awsRegionName string) *Dynamo {
	sess := utils.AWSSession(awsRegionName)

	return &Dynamo{
		dynamodb.New(sess),
	}
}

// ReleaseLock force releases lock. It deletes the supplied key from DynamoDB.
// It returns error on failed operation.
func (d *Dynamo) ReleaseLock(keyName string, tableName string) error {
	input := &dynamodb.DeleteItemInput{
		Key: map[string]*dynamodb.AttributeValue{
			"key": {
				S: aws.String(keyName),
			},
		},
		TableName: aws.String(tableName),
	}

	if _, err := d.DeleteItem(input); err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"key":   keyName,
		"table": tableName,
	}).Info("Releasing lock")

	return nil
}
