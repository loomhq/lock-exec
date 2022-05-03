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

var (
	// ErrLocked when locking an id that is already locked.
	ErrLocked = errors.New("id is locked")
)

// storageI is the interface needed to store our locks.
type storageI interface {
	DeleteItem(context.Context, *dynamodb.DeleteItemInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
}

// Client holds lock configuration.
type Client struct {
	storage storageI
	table   string
}

// New creates a new lock client.
func New(ddb storageI, table string) *Client {
	return &Client{storage: ddb, table: table}
}

// Lock creates a new lock with "id" as a unique identifier. The lock will expire after the
// specified duration. Returns ErrLocked if the lock already exists and has not expired.
func (c *Client) Lock(ctx context.Context, id string, expire time.Duration) error {
	now := time.Now().UTC().UnixNano()
	expireat := time.Now().UTC().Add(expire).UnixNano()

	_, err := c.storage.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(c.table),

		ConditionExpression: aws.String("attribute_not_exists(id) OR expire < :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":now": &types.AttributeValueMemberN{Value: strconv.Itoa(int(now))},
		},

		Item: map[string]types.AttributeValue{
			"id":     &types.AttributeValueMemberS{Value: id},
			"expire": &types.AttributeValueMemberN{Value: strconv.Itoa(int(expireat))},
		},
	})
	if err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			return fmt.Errorf("%w: %s", ErrLocked, id)
		}

		return fmt.Errorf("failed writing to dynamo for lock %s: %w", id, err)
	}

	return nil
}

// Unlock releases an existing lock of the specified id. Does not error if the lock doesn't exist.
func (c *Client) Unlock(ctx context.Context, id string) error {
	_, err := c.storage.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(c.table),

		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to unlock by deleting item '%s': %w", id, err)
	}

	return nil
}

// Locked checks if the current id is locked.
func (c *Client) Locked(ctx context.Context, id string) (bool, error) {
	get, err := c.storage.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(c.table),
		ConsistentRead: aws.Bool(true),

		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		return false, fmt.Errorf("failed checking lock status of '%s': %w", id, err)
	}

	item, ok := get.Item["expire"]
	if !ok {
		return false, nil // unlocked because item does not exist
	}

	attr, ok := item.(*types.AttributeValueMemberN)
	if !ok {
		return false, fmt.Errorf("failed checking item type for '%s': %w", id, err)
	}

	expire, err := strconv.ParseInt(attr.Value, 10, 64)
	if err != nil {
		return false, fmt.Errorf("failed to parse expiration time for '%s': %w", id, err)
	}

	if time.Now().UTC().UnixNano() < expire {
		return true, nil
	}

	return false, nil
}
