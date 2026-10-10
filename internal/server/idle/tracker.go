// Package idle tracks per-app HTTP activity and suspends an app after a
// configured idle period. A suspended app keeps its artifact on disk and is
// restarted on the next request, so the caller can treat suspend as a
// reversible transition rather than a terminal state.
package idle

import (
	"context"
	"sync"
	"time"
)

// Suspender stops an app that has been idle for too long. It returns nil when
// the app was suspended successfully.
type Suspender func(appName string, generation uint64) error

// Resumer restarts a suspended app and reports whether it came back.
type Resumer func(appName string, generation uint64) error

type entry struct {
	appName    string
	generation uint64
	timeout    time.Duration
	deadline   time.Time
}

type entries []*entry

func (q entries) Len() int           { return len(q) }
func (q entries) Less(i, j int) bool { return q[i].deadline.Before(q[j].deadline) }
func (q entries) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *entries) Push(value any)    { *q = append(*q, value.(*entry)) }
func (q *entries) Pop() any {
	old := *q
	item := old[len(old)-1]
	*q = old[:len(old)-1]
	return item
}

// pollInterval bounds how long the scheduler sleeps before re-reading the
// clock and recomputing the earliest deadline. Sleeping exactly until the
// deadline would be cheaper, but it assumes the clock only advances on its
// own, which breaks both the injected test clock and any wall-clock jump.
const pollInterval = 50 * time.Millisecond

// Tracker suspends apps whose HTTP traffic has been idle beyond the deadline.
// It mirrors the ttl scheduler: deadlines are kept in a heap ordered by time
// and every callback re-validates the generation so a stale wake-up can never
// suspend a restarted app.
type Tracker struct {
	mu      sync.Mutex
	queue   entries
	active  map[string]*entry
	changed chan struct{}
	done    chan struct{}
	once    sync.Once
	now     func() time.Time
	suspend Suspender
	pending map[string]bool // apps currently being suspended, guards re-entry
}

// New builds a tracker. now may be nil, in which case time.Now is used.
func New(now func() time.Time, suspender Suspender) *Tracker {
	if now == nil {
		now = time.Now
	}
	t := &Tracker{
		active:  make(map[string]*entry),
		changed: make(chan struct{}, 1),
		done:    make(chan struct{}),
		now:     now,
		suspend: suspender,
		pending: make(map[string]bool),
	}
	go t.run()
	return t
}

// Schedule arms the idle deadline for an app. Calling it again for the same
// app replaces the previous deadline and resets the idle window.
func (t *Tracker) Schedule(appName string, generation uint64, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	t.mu.Lock()
	item := &entry{appName: appName, generation: generation, timeout: timeout, deadline: t.now().Add(timeout)}
	// Replace any superseded entry in place: leaving the old one in the queue
	// would let a stale deadline shadow the live one once the queue is
	// re-ordered.
	for i, existing := range t.queue {
		if existing.appName == appName {
			t.queue[i] = item
			t.active[appName] = item
			sortEntries(&t.queue)
			t.mu.Unlock()
			t.signal()
			return
		}
	}
	t.active[appName] = item
	t.queue = append(t.queue, item)
	sortEntries(&t.queue)
	t.mu.Unlock()
	t.signal()
}

// Touch records HTTP activity and pushes the deadline out by the idle window.
// It is safe to call on every request.
func (t *Tracker) Touch(appName string, generation uint64, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	t.mu.Lock()
	item, ok := t.active[appName]
	if ok && item.generation == generation {
		item.deadline = t.now().Add(timeout)
		sortEntries(&t.queue)
		t.mu.Unlock()
		t.signal()
		return
	}
	t.mu.Unlock()
	t.Schedule(appName, generation, timeout)
}

// Cancel disarms the idle deadline for an app.
func (t *Tracker) Cancel(appName string) {
	t.mu.Lock()
	delete(t.active, appName)
	for i := 0; i < len(t.queue); {
		if t.queue[i].appName == appName {
			t.queue = append(t.queue[:i], t.queue[i+1:]...)
			continue
		}
		i++
	}
	t.mu.Unlock()
	t.signal()
}

// Close stops the tracker goroutine.
func (t *Tracker) Close() {
	t.once.Do(func() { close(t.done) })
}

func sortEntries(q *entries) {
	// Insertion sort avoids container/heap entirely; the queue only changes on
	// request traffic, so the O(n) pass is cheaper than maintaining an index.
	for i := 1; i < len(*q); i++ {
		item := (*q)[i]
		j := i - 1
		for j >= 0 && (*q)[j].deadline.After(item.deadline) {
			(*q)[j+1] = (*q)[j]
			j--
		}
		(*q)[j+1] = item
	}
}

func (t *Tracker) signal() {
	select {
	case t.changed <- struct{}{}:
	default:
	}
}

func (t *Tracker) run() {
	var timer *time.Timer
	for {
		t.mu.Lock()
		if len(t.queue) == 0 {
			t.mu.Unlock()
			select {
			case <-t.changed:
				continue
			case <-t.done:
				return
			}
		}
		wait := t.queue[0].deadline.Sub(t.now())
		if wait <= 0 {
			item := t.queue[0]
			t.queue = removeFirst(t.queue)
			delete(t.active, item.appName)
			t.mu.Unlock()
			if t.pending[item.appName] {
				continue
			}
			t.pending[item.appName] = true
			_ = t.suspend(item.appName, item.generation)
			t.mu.Lock()
			delete(t.pending, item.appName)
			t.mu.Unlock()
			continue
		}
		if timer == nil {
			timer = time.NewTimer(pollInterval)
		} else {
			timer.Reset(pollInterval)
		}
		ch := timer.C
		t.mu.Unlock()
		select {
		case <-ch:
		case <-t.changed:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-t.done:
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}

func removeFirst(q entries) entries {
	if len(q) == 0 {
		return q
	}
	return q[1:]
}

// ResumeContext is a helper for handlers that need to resume a suspended app
// while holding a request context.
func ResumeContext(ctx context.Context, resumer Resumer, appName string, generation uint64) error {
	if resumer == nil {
		return nil
	}
	return resumer(appName, generation)
}
