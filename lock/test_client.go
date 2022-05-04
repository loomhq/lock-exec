package lock

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// NewTestClient returns a test client with in-memory storage that can be used for lock testing.
func NewTestClient(t *testing.T) *Client {
	t.Helper()

	storage := testStorage{
		t:     t,
		mu:    &sync.Mutex{},
		items: map[string]map[string]types.AttributeValue{},
	}

	return New(storage, "testtable")
}

// testStorage implements the storage interface using an in-memory map. This interface is intended
// for use with testing and only supports the exact dynamo features needed to run lock testing.
type testStorage struct {
	t     *testing.T
	mu    *sync.Mutex
	items map[string]map[string]types.AttributeValue
}

// DeleteItem implements an in-memory version of dynamodb DeleteItem to be used only for lock testing.
func (s testStorage) DeleteItem(ctx context.Context, in *dynamodb.DeleteItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	s.mu.Lock()
	item, ok := in.Key["key"].(*types.AttributeValueMemberS)
	if !ok {
		s.mu.Unlock()
		s.t.Fatalf("item is not string")
	}

	delete(s.items, item.Value)
	s.mu.Unlock()

	return &dynamodb.DeleteItemOutput{}, nil
}

// GetItem implements an in-memory version of dynamodb GetItem to be used only for lock testing.
func (s testStorage) GetItem(ctx context.Context, in *dynamodb.GetItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	item, ok := in.Key["key"].(*types.AttributeValueMemberS)
	if !ok {
		s.t.Fatalf("item is not string")
	}

	return &dynamodb.GetItemOutput{Item: s.items[item.Value]}, nil
}

// PutItem implements an in-memory version of dynamodb PutItem to be used only for lock testing.
func (s testStorage) PutItem(ctx context.Context, in *dynamodb.PutItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	item, ok := in.Item["key"].(*types.AttributeValueMemberS)
	if !ok {
		s.t.Fatalf("item is not string")
	}

	s.mu.Lock()
	existing, ok := s.items[item.Value]
	if !ok {
		s.items[item.Value] = in.Item
		s.mu.Unlock()
		return &dynamodb.PutItemOutput{}, nil
	}
	s.mu.Unlock()

	expireN, ok := existing["expire"].(*types.AttributeValueMemberN)
	if !ok {
		s.t.Fatalf("item is not number")
	}

	expire, err := strconv.ParseInt(expireN.Value, 10, 64)
	if err != nil {
		s.t.Fatalf("could not convert expire to int")
	}

	s.mu.Lock()
	if expire < time.Now().UTC().UnixNano() {
		s.items[item.Value] = in.Item
		s.mu.Unlock()
		return &dynamodb.PutItemOutput{}, nil
	}
	s.mu.Unlock()

	return nil, &types.ConditionalCheckFailedException{}
}
