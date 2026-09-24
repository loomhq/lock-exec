package ers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Storage adapts Client to lock-exec's internal storage interface (see lock/client.go's
// unexported storageI), letting lock.New() be constructed with an ERS-backed store instead
// of a real DynamoDB client. It translates each DynamoDB-shaped call into the equivalent ERS
// node operation(s), and translates ERS responses/errors back into the exact DynamoDB SDK
// shapes lock-exec's core Lock/Unlock/Locked logic already expects -- so that code needs zero
// changes to support either backend.
//
// Design note (see the project scoping doc, "storageI ERS design"): DynamoDB's conditional
// PutItem can atomically check "attribute_not_exists(key) OR expire < :now" in one call,
// because DynamoDB condition expressions can compare against arbitrary attribute *values*.
// ERS's optimistic concurrency control only supports a version-equality check, not arbitrary
// value conditions -- so this cannot be a single atomic ERS call. Instead PutItem is
// implemented as: read the current node (if any); if it exists and is not expired, report a
// conflict immediately; otherwise create (if absent) or version-checked-update (if expired).
// The version check on the update path closes the race between the read and the write: if
// another process re-locked the key in between, our update's version will be stale and ERS
// will return 409, which we correctly report back as a lock conflict.
type Storage struct {
	client *Client
}

// NewStorage wraps an ERS Client as a lock-exec storage backend.
func NewStorage(client *Client) *Storage {
	return &Storage{client: client}
}

// PutItem implements the conditional "acquire lock" write. It mirrors DeleteItem/GetItem's
// dynamodb-shaped signature so Storage satisfies lock-exec's internal storageI interface.
func (s *Storage) PutItem(ctx context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	key, expire, err := itemToKeyExpire(in.Item)
	if err != nil {
		return nil, err
	}

	existing, err := s.client.Get(ctx, key)
	switch {
	case err == nil:
		// A node already exists. If it hasn't expired yet, the lock is still held --
		// report the same conflict DynamoDB's condition expression would.
		if existing.Expire >= time.Now().UTC().UnixNano() {
			return nil, &types.ConditionalCheckFailedException{}
		}

		// It has expired: re-acquire via a version-checked update, so a concurrent
		// re-acquire attempt from another process is caught as a conflict rather than
		// silently overwritten.
		if updateErr := s.client.Update(ctx, key, existing.Version, expire); updateErr != nil {
			if errors.Is(updateErr, ErrConflict) {
				return nil, &types.ConditionalCheckFailedException{}
			}
			return nil, fmt.Errorf("ers: failed to update expired lock: %w", updateErr)
		}

		return &dynamodb.PutItemOutput{}, nil

	case errors.Is(err, ErrNotFound):
		// No existing node: attempt to create it. idConflictPolicy=FAIL means a
		// concurrent create from another process racing us here comes back as 409.
		if createErr := s.client.Create(ctx, key, expire); createErr != nil {
			if errors.Is(createErr, ErrConflict) {
				return nil, &types.ConditionalCheckFailedException{}
			}
			return nil, fmt.Errorf("ers: failed to create lock: %w", createErr)
		}

		return &dynamodb.PutItemOutput{}, nil

	default:
		return nil, fmt.Errorf("ers: failed to read existing lock: %w", err)
	}
}

// GetItem implements the "is this locked" read used by Locked().
func (s *Storage) GetItem(ctx context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	keyAttr, ok := in.Key["key"].(*types.AttributeValueMemberS)
	if !ok {
		return nil, fmt.Errorf("ers: expected string key attribute")
	}

	existing, err := s.client.Get(ctx, keyAttr.Value)
	switch {
	case err == nil:
		return &dynamodb.GetItemOutput{
			Item: map[string]types.AttributeValue{
				"key":    &types.AttributeValueMemberS{Value: keyAttr.Value},
				"expire": &types.AttributeValueMemberN{Value: strconv.FormatInt(existing.Expire, 10)},
			},
		}, nil

	case errors.Is(err, ErrNotFound):
		// Matches DynamoDB's GetItem behaviour for a missing item: no error, empty Item.
		return &dynamodb.GetItemOutput{}, nil

	default:
		return nil, fmt.Errorf("ers: failed to read lock: %w", err)
	}
}

// DeleteItem implements Unlock(). Matches lock-exec's existing DynamoDB semantics: this is
// an unconditional delete with no ownership check (see the project scoping doc's Section 14
// -- lock-exec has never enforced lock ownership on release, on either backend).
func (s *Storage) DeleteItem(ctx context.Context, in *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	keyAttr, ok := in.Key["key"].(*types.AttributeValueMemberS)
	if !ok {
		return nil, fmt.Errorf("ers: expected string key attribute")
	}

	if err := s.client.Delete(ctx, keyAttr.Value); err != nil {
		return nil, fmt.Errorf("ers: failed to delete lock: %w", err)
	}

	return &dynamodb.DeleteItemOutput{}, nil
}

// itemToKeyExpire extracts the "key" (string) and "expire" (number) attributes lock-exec
// always writes -- see lock/lock.go's Lock(), which builds exactly this two-attribute Item.
func itemToKeyExpire(item map[string]types.AttributeValue) (key string, expire int64, err error) {
	keyAttr, ok := item["key"].(*types.AttributeValueMemberS)
	if !ok {
		return "", 0, fmt.Errorf("ers: expected string key attribute")
	}

	expireAttr, ok := item["expire"].(*types.AttributeValueMemberN)
	if !ok {
		return "", 0, fmt.Errorf("ers: expected numeric expire attribute")
	}

	expire, err = strconv.ParseInt(expireAttr.Value, 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("ers: failed to parse expire attribute: %w", err)
	}

	return keyAttr.Value, expire, nil
}
