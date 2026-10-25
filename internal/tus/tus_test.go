package tus

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// One writer per upload, however the lock is handed over between waiting requests.
func TestLockIsExclusive(t *testing.T) {
	s := &Service{locks: map[uuid.UUID]*uploadLock{}}
	id := uuid.New()
	var inside, worst atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				unlock := s.lock(id)
				n := inside.Add(1)
				if n > worst.Load() {
					worst.Store(n)
				}
				inside.Add(-1)
				unlock()
			}
		}()
	}
	wg.Wait()
	if w := worst.Load(); w != 1 {
		t.Fatalf("%d writers held the lock at once", w)
	}
	if len(s.locks) != 0 {
		t.Fatalf("%d locks left behind", len(s.locks))
	}
}
