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
		out, err := tc.Run(ctx, "locktest", "sleep 1")
		assert.Empty(t, out)
		assert.NoError(t, err)
		wg.Done()
	}()
	time.Sleep(time.Millisecond * 500)

	out, err := tc.Run(ctx, "locktest", "sleep 5")
	assert.Empty(t, out)
	assert.ErrorIs(t, err, ErrLocked)

	out, err = tc.Run(ctx, "locktest", "echo hello test")
	assert.NoError(t, err)
	assert.Equal(t, "hello test", out)

	wg.Wait()
}
