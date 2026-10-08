package ttl

import (
	"container/heap"
	"sync"
	"time"
)

type entry struct {
	key        string
	generation uint64
	expiresAt  time.Time
	callback   func(string, uint64)
	index      int
}

type entries []*entry

func (q entries) Len() int           { return len(q) }
func (q entries) Less(i, j int) bool { return q[i].expiresAt.Before(q[j].expiresAt) }
func (q entries) Swap(i, j int)      { q[i], q[j] = q[j], q[i]; q[i].index, q[j].index = i, j }
func (q *entries) Push(value any) {
	item := value.(*entry)
	item.index = len(*q)
	*q = append(*q, item)
}
func (q *entries) Pop() any { old := *q; item := old[len(old)-1]; *q = old[:len(old)-1]; return item }

type Scheduler struct {
	mu      sync.Mutex
	queue   entries
	active  map[string]*entry
	changed chan struct{}
	done    chan struct{}
	once    sync.Once
}

func New() *Scheduler {
	s := &Scheduler{active: make(map[string]*entry), changed: make(chan struct{}, 1), done: make(chan struct{})}
	heap.Init(&s.queue)
	go s.run()
	return s
}

func (s *Scheduler) Schedule(key string, generation uint64, expiresAt time.Time, callback func(string, uint64)) {
	item := &entry{key: key, generation: generation, expiresAt: expiresAt, callback: callback}
	s.mu.Lock()
	s.active[key] = item
	heap.Push(&s.queue, item)
	s.mu.Unlock()
	s.signal()
}

func (s *Scheduler) Cancel(key string) {
	s.mu.Lock()
	delete(s.active, key)
	s.mu.Unlock()
	s.signal()
}

func (s *Scheduler) signal() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func (s *Scheduler) Close() {
	s.once.Do(func() { close(s.done) })
}

func (s *Scheduler) run() {
	var timer *time.Timer
	for {
		s.mu.Lock()
		for len(s.queue) > 0 && s.active[s.queue[0].key] != s.queue[0] {
			heap.Pop(&s.queue)
		}
		if len(s.queue) == 0 {
			s.mu.Unlock()
			select {
			case <-s.changed:
				continue
			case <-s.done:
				return
			}
		}
		wait := time.Until(s.queue[0].expiresAt)
		if wait <= 0 {
			item := heap.Pop(&s.queue).(*entry)
			if s.active[item.key] == item {
				delete(s.active, item.key)
			} else {
				s.mu.Unlock()
				continue
			}
			s.mu.Unlock()
			item.callback(item.key, item.generation)
			continue
		}
		if timer == nil {
			timer = time.NewTimer(wait)
		} else {
			timer.Reset(wait)
		}
		ch := timer.C
		s.mu.Unlock()
		select {
		case <-ch:
		case <-s.changed:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-s.done:
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}
