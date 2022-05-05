package lock

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Lock creates a new lock with "key" as a unique identifier. The lock will expire after the
// specified duration. Returns ErrLocked if the lock already exists and has not expired.
func (c *Client) Lock(ctx context.Context, key string, expire time.Duration) error {
	now := time.Now().UTC().UnixNano()
	expireat := time.Now().UTC().Add(expire).UnixNano()

	_, err := c.storage.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(c.table),

		ConditionExpression: aws.String("attribute_not_exists(#key) OR expire < :now"),
		ExpressionAttributeNames: map[string]string{
			"#key": "key",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":now": &types.AttributeValueMemberN{Value: strconv.Itoa(int(now))},
		},

		Item: map[string]types.AttributeValue{
			"key":    &types.AttributeValueMemberS{Value: key},
			"expire": &types.AttributeValueMemberN{Value: strconv.Itoa(int(expireat))},
		},
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("%w: %s", ErrLocked, key)
		}

		return fmt.Errorf("failed writing to dynamo for lock %s: %w", key, err)
	}

	return nil
}

// Unlock releases an existing lock of the specified key. Does not error if the lock doesn't exist.
func (c *Client) Unlock(ctx context.Context, key string) error {
	_, err := c.storage.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(c.table),

		Key: map[string]types.AttributeValue{
			"key": &types.AttributeValueMemberS{Value: key},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to unlock by deleting item '%s': %w", key, err)
	}

	return nil
}

// Locked checks if the current key is locked.
func (c *Client) Locked(ctx context.Context, key string) (bool, error) {
	get, err := c.storage.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(c.table),
		ConsistentRead: aws.Bool(true),

		Key: map[string]types.AttributeValue{
			"key": &types.AttributeValueMemberS{Value: key},
		},
	})
	if err != nil {
		return false, fmt.Errorf("failed checking lock status of '%s': %w", key, err)
	}

	item, ok := get.Item["expire"]
	if !ok {
		return false, nil // unlocked because item does not exist
	}

	attr, ok := item.(*types.AttributeValueMemberN)
	if !ok {
		return false, fmt.Errorf("failed checking item type for '%s': %w", key, err)
	}

	expire, err := strconv.ParseInt(attr.Value, 10, 64)
	if err != nil {
		return false, fmt.Errorf("failed to parse expiration time for '%s': %w", key, err)
	}

	if time.Now().UTC().UnixNano() < expire {
		return true, nil
	}

	return false, nil
}
