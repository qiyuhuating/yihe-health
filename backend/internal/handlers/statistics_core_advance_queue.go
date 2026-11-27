package handlers

import "sync"

// Projection work is disposable: readers validate the authoritative revision
// and fill missing changes from MySQL. Limit retained batches and run at most
// one worker per cache; an empty queue has no live goroutine.
const coreAdvanceQueueCapacity = 4

type coreAdvanceQueue struct {
	mu          sync.Mutex
	jobs        [coreAdvanceQueueCapacity]func()
	head, count int
	running     bool
}

func (q *coreAdvanceQueue) submit(job func()) bool {
	q.mu.Lock()
	if q.count == len(q.jobs) {
		q.mu.Unlock()
		return false
	}
	q.jobs[(q.head+q.count)%len(q.jobs)] = job
	q.count++
	if !q.running {
		q.running = true
		go q.drain()
	}
	q.mu.Unlock()
	return true
}

func (q *coreAdvanceQueue) drain() {
	for {
		q.mu.Lock()
		if q.count == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		job := q.jobs[q.head]
		q.jobs[q.head] = nil
		q.head = (q.head + 1) % len(q.jobs)
		q.count--
		q.mu.Unlock()
		job()
	}
}
