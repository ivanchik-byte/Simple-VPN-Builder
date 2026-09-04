package manager

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInterfaceWatchdog_Lifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	healed := make([]string, 0)
	healFn := func(ctx context.Context, iface string) error {
		mu.Lock()
		defer mu.Unlock()
		healed = append(healed, iface)
		return nil
	}

	wd := NewInterfaceWatchdog([]string{"wg-test0"}, 50*time.Millisecond, healFn)
	wd.Start(ctx)

	// Explicitly invoke CheckAll on non-existent interface to test self-heal trigger
	wd.CheckAll(ctx)

	mu.Lock()
	assert.Contains(t, healed, "wg-test0")
	mu.Unlock()

	wd.Stop()
}
