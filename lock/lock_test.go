package lock

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLock(t *testing.T) {
	t.Parallel()

	tc := NewTestClient(t)
	ctx := context.Background()

	err := tc.Lock(ctx, "locktest", time.Minute)
	assert.NoError(t, err)

	err = tc.Lock(ctx, "locktest", time.Minute)
	assert.ErrorIs(t, err, ErrLocked)

	locked, err := tc.Locked(ctx, "locktest")
	assert.NoError(t, err)
	assert.True(t, locked)

	err = tc.Unlock(ctx, "locktest")
	assert.NoError(t, err)

	locked, err = tc.Locked(ctx, "locktest")
	assert.NoError(t, err)
	assert.False(t, locked)
}
