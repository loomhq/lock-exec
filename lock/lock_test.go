package lock

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/stretchr/testify/assert"
)

type mockDynamoDBClient struct {
	dynamodbiface.DynamoDBAPI
	deleteItemCallback func(input *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error)
}

func (c *mockDynamoDBClient) DeleteItem(input *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
	return c.deleteItemCallback(input)
}

func TestDynamoReleaseLock(t *testing.T) {
	t.Run("release lock pass", func(t *testing.T) {
		client := &mockDynamoDBClient{}
		client.deleteItemCallback = func(input *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
			assert.Equal(t, "table-foo-bar", *input.TableName)
			assert.Equal(t, "custom-key", *input.Key["key"].S)

			return &dynamodb.DeleteItemOutput{}, nil
		}

		d := Dynamo{
			client,
		}

		err := d.ReleaseLock("custom-key", "table-foo-bar")
		assert.NoError(t, err)
	})

	t.Run("release lock fail", func(t *testing.T) {
		client := &mockDynamoDBClient{}
		client.deleteItemCallback = func(input *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
			return nil, awserr.New(dynamodb.ErrCodeConditionalCheckFailedException, "The conditional request failed.", errors.New("fake error"))
		}

		d := Dynamo{
			client,
		}

		err := d.ReleaseLock("key", "table")
		assert.Error(t, err)
	})
}
