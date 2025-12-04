package lock

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRun(t *testing.T) {
	t.Parallel()

	tc := NewTestClient(t)
	ctx := context.Background()
	wg := &sync.WaitGroup{}

	wg.Add(1)
	go func() {
		err := tc.Run(ctx, "locktest", "sleep 1")
		assert.NoError(t, err)
		wg.Done()
	}()
	time.Sleep(time.Millisecond * 500)

	err := tc.Run(ctx, "locktest", "sleep 5")
	assert.ErrorIs(t, err, ErrLocked)

	wg.Wait()

	err = tc.Run(ctx, "locktest", "echo hello test")
	assert.NoError(t, err)
}
