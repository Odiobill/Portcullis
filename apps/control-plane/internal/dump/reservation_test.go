package dump

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// blockingCommander blocks the first same-service start until released,
// simulating a slow/delayed process start that crosses the rate window.
type blockingCommander struct {
	mu        sync.Mutex
	starts    []startRecord
	first     bool
	entered   chan struct{}
	unblockCh chan error
}

func newBlockingCommander() *blockingCommander {
	return &blockingCommander{
		entered:   make(chan struct{}),
		unblockCh: make(chan error),
	}
}

func (b *blockingCommander) Start(_ context.Context, name string, args []string, env []string) (io.ReadCloser, func() error, func(), error) {
	b.mu.Lock()
	first := !b.first
	if first {
		b.first = true
	}
	b.starts = append(b.starts, startRecord{name: name, args: args})
	b.mu.Unlock()

	if first {
		b.entered <- struct{}{}
		if err := <-b.unblockCh; err != nil {
			return nil, nil, nil, err
		}
	}
	return io.NopCloser(strings.NewReader("dump-bytes")), func() error { return nil }, func() {}, nil
}

func (b *blockingCommander) startCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.starts)
}

// TestReleaseScopedToOwnReservation is the deterministic interleaving proof
// required by the Lead: a delayed first start that fails after its window
// has passed must release ONLY its own reservation. A newer successful
// same-service start must keep its slot, so a third request stays
// rate-limited and the commander is never started a third time.
func TestReleaseScopedToOwnReservation(t *testing.T) {
	clock := time.Unix(1785200000, 0)
	cmd := newBlockingCommander()
	d, err := New(Config{
		DBHost:    "postgres.internal",
		DBUser:    "dump_user",
		Commander: cmd,
		Now:       func() time.Time { return clock },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// First same-service start: acquires the slot at t0, then blocks inside
	// the commander.
	start1 := make(chan error, 1)
	go func() {
		_, _, _, err := d.Start(context.Background(), "svc-a", "portcullis_a")
		start1 <- err
	}()
	<-cmd.entered // first commander start entered; slot owned at t0

	// Advance beyond the window: a second request legitimately acquires a
	// newer slot at t0+6m and succeeds.
	clock = clock.Add(6 * time.Minute)
	if _, _, _, err := d.Start(context.Background(), "svc-a", "portcullis_a"); err != nil {
		t.Fatalf("second start after window: %v", err)
	}

	// The delayed first start now fails and releases its reservation.
	cmd.unblockCh <- errors.New("delayed first start failed")
	if err := <-start1; err == nil {
		t.Fatal("delayed first start must surface its failure")
	}

	// The newer successful slot must survive the stale release: a third
	// same-service request stays rate-limited and no third process starts.
	thirdErr := func() error {
		_, _, _, err3 := d.Start(context.Background(), "svc-a", "portcullis_a")
		return err3
	}()
	if !errors.Is(thirdErr, ErrRateLimited) {
		t.Fatalf("third request must remain rate-limited after stale release, got %v", thirdErr)
	}
	if got := cmd.startCount(); got != 2 {
		t.Fatalf("commander starts = %d, want 2 (no third start)", got)
	}
}
