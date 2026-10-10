package idle_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jiangfire/dzjjy/internal/server/idle"
)

// fakeClock lets the test drive deadlines without sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// settle yields long enough for the scheduler goroutine to observe a clock
// change that the test made from another goroutine.
func settle() {
	time.Sleep(20 * time.Millisecond)
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return condition()
}

func TestTrackerSuspendsAfterIdleTimeout(t *testing.T) {
	clock := newFakeClock()
	type suspension struct {
		app        string
		generation uint64
	}
	suspended := make(chan suspension, 4)
	tracker := idle.New(clock.Now, func(app string, generation uint64) error {
		suspended <- suspension{app, generation}
		return nil
	})
	defer tracker.Close()

	tracker.Schedule("web", 3, 30*time.Second)
	clock.Advance(31 * time.Second)

	select {
	case got := <-suspended:
		assert.Equal(t, "web", got.app)
		assert.Equal(t, uint64(3), got.generation)
	case <-time.After(2 * time.Second):
		t.Fatal("expected the app to be suspended after the idle timeout")
	}
}

func TestTrackerDoesNotSuspendBeforeTimeout(t *testing.T) {
	clock := newFakeClock()
	suspended := make(chan string, 4)
	tracker := idle.New(clock.Now, func(app string, generation uint64) error {
		suspended <- app
		return nil
	})
	defer tracker.Close()

	tracker.Schedule("web", 1, time.Minute)
	clock.Advance(30 * time.Second)

	select {
	case app := <-suspended:
		t.Fatalf("app %q was suspended before the idle timeout elapsed", app)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestTouchPushesDeadlineOut(t *testing.T) {
	clock := newFakeClock()
	suspended := make(chan string, 4)
	tracker := idle.New(clock.Now, func(app string, generation uint64) error {
		suspended <- app
		return nil
	})
	defer tracker.Close()

	tracker.Schedule("web", 1, 30*time.Second)
	clock.Advance(20 * time.Second)
	// Traffic arrives, so the window restarts from the touch. Give the
	// scheduler a moment to observe the new deadline before asserting.
	tracker.Touch("web", 1, 30*time.Second)
	// Still inside the refreshed 30s window (touch at 20s, deadline at 50s).
	clock.Advance(20 * time.Second)
	settle()

	select {
	case app := <-suspended:
		t.Fatalf("app %q suspended despite recent traffic", app)
	case <-time.After(150 * time.Millisecond):
	}

	clock.Advance(15 * time.Second)
	settle()
	select {
	case app := <-suspended:
		assert.Equal(t, "web", app)
	case <-time.After(2 * time.Second):
		t.Fatal("expected suspension once the refreshed window elapsed")
	}
}

func TestCancelPreventsSuspension(t *testing.T) {
	clock := newFakeClock()
	suspended := make(chan string, 4)
	tracker := idle.New(clock.Now, func(app string, generation uint64) error {
		suspended <- app
		return nil
	})
	defer tracker.Close()

	tracker.Schedule("web", 1, 10*time.Second)
	tracker.Cancel("web")
	clock.Advance(time.Hour)

	select {
	case app := <-suspended:
		t.Fatalf("app %q was suspended after cancellation", app)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestGenerationChangeReschedules(t *testing.T) {
	clock := newFakeClock()
	var mu sync.Mutex
	var calls []uint64
	tracker := idle.New(clock.Now, func(app string, generation uint64) error {
		mu.Lock()
		calls = append(calls, generation)
		mu.Unlock()
		return nil
	})
	defer tracker.Close()

	tracker.Schedule("web", 1, 10*time.Second)
	tracker.Touch("web", 2, 10*time.Second)
	clock.Advance(11 * time.Second)

	require.True(t, waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) > 0
	}))
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, calls, 1)
	assert.Equal(t, uint64(2), calls[0], "a stale generation must never be suspended")
}

func TestZeroTimeoutIsIgnored(t *testing.T) {
	clock := newFakeClock()
	suspended := make(chan string, 4)
	tracker := idle.New(clock.Now, func(app string, generation uint64) error {
		suspended <- app
		return nil
	})
	defer tracker.Close()

	tracker.Schedule("web", 1, 0)
	clock.Advance(time.Hour)

	select {
	case app := <-suspended:
		t.Fatalf("app %q was suspended despite a zero timeout", app)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestConcurrentTouchIsSafe(t *testing.T) {
	tracker := idle.New(nil, func(app string, generation uint64) error { return nil })
	defer tracker.Close()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				tracker.Touch("web", 1, time.Minute)
			}
		}()
	}
	wg.Wait()
	tracker.Cancel("web")
}
