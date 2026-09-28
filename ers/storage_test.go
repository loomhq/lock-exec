package ers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/loomhq/lock-exec/v2/ers"
)

const (
	// attrKey is the DynamoDB-shaped attribute name lock-exec uses for the lock key.
	attrKey = "key"
	// testKey is the lock key used throughout these tests.
	testKey = "my-lock"
)

// fakeNode mirrors the real ERS wire shape (client.go's nodeResponse/updateNodeRequest)
// closely enough for test servers to decode requests and encode responses: custom schema
// properties like "expire" are nested under "properties", confirmed against a live ERS
// instance (see client.go's package doc).
type fakeNode struct {
	ID         string             `json:"id"`
	Version    int64              `json:"version"`
	Properties fakeNodeProperties `json:"properties"`
}

type fakeNodeProperties struct {
	Expire int64 `json:"expire"`
}

func putItemInput(expire int64) *dynamodb.PutItemInput {
	return &dynamodb.PutItemInput{
		Item: map[string]types.AttributeValue{
			attrKey:  &types.AttributeValueMemberS{Value: testKey},
			"expire": &types.AttributeValueMemberN{Value: jsonNumber(expire)},
		},
	}
}

func getItemInput(key string) *dynamodb.GetItemInput {
	return &dynamodb.GetItemInput{
		Key: map[string]types.AttributeValue{
			attrKey: &types.AttributeValueMemberS{Value: key},
		},
	}
}

func deleteItemInput(key string) *dynamodb.DeleteItemInput {
	return &dynamodb.DeleteItemInput{
		Key: map[string]types.AttributeValue{
			attrKey: &types.AttributeValueMemberS{Value: key},
		},
	}
}

// writeJSON encodes v as a test server response. It uses t.Errorf, not t.Fatalf, because it
// runs on the server's goroutine.
func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()

	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("failed to encode test response: %v", err)
	}
}

// TestPutItem_CreatesWhenAbsent verifies that PutItem issues a plain create (POST) when no
// node exists yet for the key, matching DynamoDB's attribute_not_exists() branch.
func TestPutItem_CreatesWhenAbsent(t *testing.T) {
	t.Parallel()

	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	storage := ers.NewStorage(ers.NewClient(srv.Client(), srv.URL, "test-schema", 1, nil))

	expire := time.Now().Add(time.Hour).UnixNano()
	_, err := storage.PutItem(context.Background(), putItemInput(expire))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected last request to be POST, got %s", gotMethod)
	}
}

// TestPutItem_ConflictWhenLockedAndNotExpired verifies that PutItem reports a
// ConditionalCheckFailedException (the exact error type lock-exec's Lock() checks for) when
// an existing, unexpired node is found -- without attempting any write.
func TestPutItem_ConflictWhenLockedAndNotExpired(t *testing.T) {
	t.Parallel()

	futureExpire := time.Now().Add(time.Hour).UnixNano()

	writeAttempted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(t, w, fakeNode{ID: testKey, Version: 1, Properties: fakeNodeProperties{Expire: futureExpire}})
		case http.MethodPost, http.MethodPatch:
			writeAttempted = true
			w.WriteHeader(http.StatusConflict)
		}
	}))
	defer srv.Close()

	storage := ers.NewStorage(ers.NewClient(srv.Client(), srv.URL, "test-schema", 1, nil))

	_, err := storage.PutItem(context.Background(), putItemInput(time.Now().Add(time.Hour).UnixNano()))
	if err == nil {
		t.Fatal("expected an error")
	}

	var condErr *types.ConditionalCheckFailedException
	if !isConditionalCheckFailed(err, &condErr) {
		t.Fatalf("expected ConditionalCheckFailedException, got %v (%T)", err, err)
	}
	if writeAttempted {
		t.Fatal("expected no write attempt when lock is still held")
	}
}

// TestPutItem_ReacquiresExpiredLock verifies that PutItem re-acquires an existing but
// expired lock via a version-checked update (PATCH), matching DynamoDB's
// "expire < :now" branch of the condition expression.
func TestPutItem_ReacquiresExpiredLock(t *testing.T) {
	t.Parallel()

	pastExpire := time.Now().Add(-time.Hour).UnixNano()

	var patchedVersion int64 = -1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(t, w, fakeNode{ID: testKey, Version: 7, Properties: fakeNodeProperties{Expire: pastExpire}})
		case http.MethodPatch:
			// Only "version" is decoded here -- the real "properties" shape on a PATCH
			// request body nests an {action, value} object per property (see client.go's
			// updateNodeProperties), not a raw fakeNodeProperties value, so it isn't
			// meaningfully decodable via fakeNode; this test only cares about the version
			// sent.
			var body struct {
				Version int64 `json:"version"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("failed to decode PATCH body: %v", err)
			}
			patchedVersion = body.Version
			writeJSON(t, w, fakeNode{ID: testKey, Version: body.Version + 1})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	storage := ers.NewStorage(ers.NewClient(srv.Client(), srv.URL, "test-schema", 1, nil))

	_, err := storage.PutItem(context.Background(), putItemInput(time.Now().Add(time.Hour).UnixNano()))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if patchedVersion != 7 {
		t.Fatalf("expected update to use version read from GET (7), got %d", patchedVersion)
	}
}

// TestPutItem_ConflictWhenExpiredButRaceLost verifies that a 409 on the version-checked
// update (another process won the race to re-acquire an expired lock) is correctly surfaced
// as a ConditionalCheckFailedException.
func TestPutItem_ConflictWhenExpiredButRaceLost(t *testing.T) {
	t.Parallel()

	pastExpire := time.Now().Add(-time.Hour).UnixNano()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(t, w, fakeNode{ID: testKey, Version: 7, Properties: fakeNodeProperties{Expire: pastExpire}})
		case http.MethodPatch:
			w.WriteHeader(http.StatusConflict)
		}
	}))
	defer srv.Close()

	storage := ers.NewStorage(ers.NewClient(srv.Client(), srv.URL, "test-schema", 1, nil))

	_, err := storage.PutItem(context.Background(), putItemInput(time.Now().Add(time.Hour).UnixNano()))

	var condErr *types.ConditionalCheckFailedException
	if !isConditionalCheckFailed(err, &condErr) {
		t.Fatalf("expected ConditionalCheckFailedException, got %v (%T)", err, err)
	}
}

// TestGetItem_NotFoundReturnsEmptyItem verifies GetItem mirrors DynamoDB's behaviour for a
// missing key: no error, just an empty Item -- which is what lock-exec's Locked() expects.
func TestGetItem_NotFoundReturnsEmptyItem(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	storage := ers.NewStorage(ers.NewClient(srv.Client(), srv.URL, "test-schema", 1, nil))

	out, err := storage.GetItem(context.Background(), getItemInput("missing-lock"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out.Item) != 0 {
		t.Fatalf("expected empty item, got %v", out.Item)
	}
}

// TestGetItem_FoundReturnsExpire verifies GetItem returns the expire attribute in the exact
// DynamoDB AttributeValue shape lock-exec's Locked() parses.
func TestGetItem_FoundReturnsExpire(t *testing.T) {
	t.Parallel()

	expire := time.Now().Add(time.Hour).UnixNano()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, fakeNode{ID: testKey, Version: 1, Properties: fakeNodeProperties{Expire: expire}})
	}))
	defer srv.Close()

	storage := ers.NewStorage(ers.NewClient(srv.Client(), srv.URL, "test-schema", 1, nil))

	out, err := storage.GetItem(context.Background(), getItemInput(testKey))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expireAttr, ok := out.Item["expire"].(*types.AttributeValueMemberN)
	if !ok {
		t.Fatalf("expected numeric expire attribute, got %v", out.Item["expire"])
	}
	if expireAttr.Value != jsonNumber(expire) {
		t.Fatalf("expected expire %d, got %s", expire, expireAttr.Value)
	}
}

// TestDeleteItem_UnconditionalAndIdempotent verifies Unlock() semantics: deleting an
// existing lock succeeds, and deleting an already-absent one is also not an error --
// matching DynamoDB's DeleteItem idempotency (see the scoping doc's Section 14: lock-exec
// has never enforced ownership on release, on either backend).
func TestDeleteItem_UnconditionalAndIdempotent(t *testing.T) {
	t.Parallel()

	notFound := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected method %s", r.Method)
		}
		if notFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	storage := ers.NewStorage(ers.NewClient(srv.Client(), srv.URL, "test-schema", 1, nil))

	if _, err := storage.DeleteItem(context.Background(), deleteItemInput(testKey)); err != nil {
		t.Fatalf("expected no error deleting existing lock, got %v", err)
	}

	notFound = true
	if _, err := storage.DeleteItem(context.Background(), deleteItemInput(testKey)); err != nil {
		t.Fatalf("expected no error deleting already-absent lock, got %v", err)
	}
}

// jsonNumber formats v the way a DynamoDB number attribute holds it.
func jsonNumber(v int64) string {
	return strconv.FormatInt(v, 10)
}

func isConditionalCheckFailed(err error, target **types.ConditionalCheckFailedException) bool {
	if ce, ok := err.(*types.ConditionalCheckFailedException); ok { //nolint:errorlint
		*target = ce
		return true
	}
	return false
}
