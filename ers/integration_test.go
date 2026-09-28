//go:build ers_integration

// Integration tests that exercise a real local ERS instance (`atlas tdp ers local up`,
// see loom's projects/repo-tools/devenv/tdp/ers/). These are gated behind the
// ers_integration build tag so `go test ./...` doesn't require ERS to be running:
//
//	go test -tags ers_integration ./ers/... -v
package ers_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/loomhq/lock-exec/v2/ers"
	"github.com/loomhq/lock-exec/v2/lock"
)

// `atlas tdp ers local up` starts two separate services (confirmed via docker ps + each
// service's own /api/oas title): the ERS *control* plane (schema/partition management) on
// 9301, and the ERS *data* plane (node CRUD -- what lock-exec actually needs) on 9300.
// Schemas and partitions must be managed on 9301; node Create/Get/Update/Delete calls must
// go to 9300 -- pointing lock-exec's client at 9301 fails with a generic Spring
// "No static resource nodes" 404, not a helpful error.
const (
	controlPlaneBaseURL = "http://localhost:9301"
	dataPlaneBaseURL    = "http://localhost:9300"

	// testPartitionID must match local-env-seed/partitions/test-partition-id.json's
	// "partitionId". Confirmed working end-to-end (GET /partitions/tdp-ers-local -> 200,
	// active) *only* when ERS is started via start-local-tdp-ers.sh (which passes
	// `-d "${SEED_DIR}"` to `atlas tdp ers local up`) -- a bare `atlas tdp ers local up`
	// with no seeding path does not apply this repo's seed data at all, which looks
	// identical to "the seed file's format is wrong" from the outside (GET returns
	// "doesn't exist!") unless you specifically check how the instance was started.
	testPartitionID = "tdp-ers-local"
)

// createTestSchema creates a fresh ERS schema for a single test run, using a random type
// name suffix to avoid clashing with other test runs or any pre-seeded schemas (same
// approach the official ers-nodejs-sdk examples use in
// spec/api-examples/schema.ts:createSchema). Returns the schema type name to use.
func createTestSchema(t *testing.T) string {
	t.Helper()

	suffix := make([]byte, 8) //nolint:mnd
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("failed to generate random schema suffix: %v", err)
	}
	schemaType := fmt.Sprintf("ati:loom:infra:lock-exec-test:%s", hex.EncodeToString(suffix))

	schema := map[string]any{
		"configFormat":   1,
		"type":           schemaType,
		"description":    "lock-exec integration test schema (safe to delete)",
		"requestedState": "active",
		"owners":         []string{"loom/loom"},
		"idPolicy":       "consumer-provided",
		"skPolicy":       "none",
		"ttlPolicy":      "optional",
		"properties": map[string]any{
			"expire": map[string]any{
				"type":           "long",
				"required":       true,
				"description":    "lock expiry, nanosecond unix epoch",
				"classification": []string{"DataType:Atlassian/Configuration"},
			},
		},
	}

	body, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("failed to encode schema: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, controlPlaneBaseURL+"/schemas/nodes", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to build schema create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range ers.NewLocalDevHeaders("") {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to create schema -- is `atlas tdp ers local up` running? %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= http.StatusBadRequest {
		t.Fatalf("failed to create schema: unexpected status %d", resp.StatusCode)
	}

	return schemaType
}

// newTestLockClient builds a real lock.Client backed by local ERS, using a fresh schema
// against the seeded test partition.
func newTestLockClient(t *testing.T) *lock.Client {
	t.Helper()

	schemaType := createTestSchema(t)
	client := ers.NewClient(nil, dataPlaneBaseURL, schemaType, 1, ers.NewLocalDevHeaders(testPartitionID))
	storage := ers.NewStorage(client)

	return lock.New(storage, "lock-exec-integration-test")
}

// TestIntegration_LockUnlockLocked exercises the basic Lock/Locked/Unlock cycle against
// real local ERS.
func TestIntegration_LockUnlockLocked(t *testing.T) {
	client := newTestLockClient(t)
	ctx := context.Background()
	key := "integration-test-basic"

	locked, err := client.Locked(ctx, key)
	if err != nil {
		t.Fatalf("Locked() before lock: %v", err)
	}
	if locked {
		t.Fatal("expected key to be unlocked before Lock()")
	}

	if err := client.Lock(ctx, key, time.Minute); err != nil {
		t.Fatalf("Lock() failed: %v", err)
	}

	locked, err = client.Locked(ctx, key)
	if err != nil {
		t.Fatalf("Locked() after lock: %v", err)
	}
	if !locked {
		t.Fatal("expected key to be locked after Lock()")
	}

	if err := client.Unlock(ctx, key); err != nil {
		t.Fatalf("Unlock() failed: %v", err)
	}

	locked, err = client.Locked(ctx, key)
	if err != nil {
		t.Fatalf("Locked() after unlock: %v", err)
	}
	if locked {
		t.Fatal("expected key to be unlocked after Unlock()")
	}
}

// TestIntegration_ConcurrentLockConflicts verifies that a second Lock() attempt on an
// already-held, unexpired key fails with ErrLocked, against real ERS (not the in-memory
// fake or a mocked HTTP server).
func TestIntegration_ConcurrentLockConflicts(t *testing.T) {
	client := newTestLockClient(t)
	ctx := context.Background()
	key := "integration-test-conflict"

	if err := client.Lock(ctx, key, time.Minute); err != nil {
		t.Fatalf("first Lock() failed: %v", err)
	}
	defer client.Unlock(ctx, key) //nolint:errcheck

	err := client.Lock(ctx, key, time.Minute)
	if err == nil {
		t.Fatal("expected second Lock() to fail while first lock is held")
	}
	if !isLockedErr(err) {
		t.Fatalf("expected ErrLocked, got: %v", err)
	}
}

// TestIntegration_ReacquireExpiredLock verifies that a lock can be re-acquired once its
// expire time has passed, exercising the real version-checked ERS update path (not just
// the create path).
func TestIntegration_ReacquireExpiredLock(t *testing.T) {
	client := newTestLockClient(t)
	ctx := context.Background()
	key := "integration-test-expiry"

	if err := client.Lock(ctx, key, 500*time.Millisecond); err != nil { //nolint:mnd
		t.Fatalf("first Lock() failed: %v", err)
	}

	time.Sleep(750 * time.Millisecond) //nolint:mnd

	if err := client.Lock(ctx, key, time.Minute); err != nil {
		t.Fatalf("expected Lock() to succeed after expiry, got: %v", err)
	}
	defer client.Unlock(ctx, key) //nolint:errcheck
}

// TestIntegration_Run exercises the full Run() cycle (lock, execute, auto-unlock) used
// by the CLI's `run` command, against real local ERS.
func TestIntegration_Run(t *testing.T) {
	client := newTestLockClient(t)
	ctx := context.Background()
	key := "integration-test-run"

	if err := client.Run(ctx, key, "true"); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	locked, err := client.Locked(ctx, key)
	if err != nil {
		t.Fatalf("Locked() after Run(): %v", err)
	}
	if locked {
		t.Fatal("expected key to be unlocked after Run() completes (auto-unlock)")
	}
}

func isLockedErr(err error) bool {
	return errors.Is(err, lock.ErrLocked)
}
