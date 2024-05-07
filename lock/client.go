package lock

import (
	"context"
	"errors"
	"time"

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
	expire  time.Duration
}

// New creates a new lock client.
func New(ddb storageI, table string) *Client {
	return &Client{
		storage: ddb,
		table:   table,
		expire:  time.Hour * 24, //nolint:mnd
	}
}

// SetExpire sets the default expire duration for locks. Defaults to 24 hours if not set.
func (c *Client) SetExpire(d time.Duration) {
	c.expire = d
}
