package lock

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

var (
	// ErrLocked when locking an key that is already locked.
	ErrLocked = errors.New("key is locked")
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
