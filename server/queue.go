package server

import "sync"

// serialQueue runs submitted jobs one at a time, in submission order, on a
// goroutine it spawns on demand and lets go of when the queue drains.
//
// DIVERGENCE (deliberate): pi serialises this kind of work by chaining onto a
// promise it keeps in a field — `queue = queue.then(next)`. That both orders
// the jobs and lets each caller await its own. Go has no promise to chain, and
// every caller here is fire-and-forget, so the ordering is what matters. A
// queue plus one drain goroutine reproduces it, and Wait gives shutdown the
// join point a promise chain would not have needed.
//
// Wait is deliberately not a sync.WaitGroup: submissions keep arriving from
// other goroutines while shutdown waits, and Add racing an already-zero Wait is
// exactly the misuse a WaitGroup forbids.
//
// A job must not call Wait on the queue that is running it.
type serialQueue struct {
	mu      sync.Mutex
	idle    *sync.Cond
	jobs    []func()
	running bool
}

// Go submits a job. It never blocks.
func (q *serialQueue) Go(job func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, job)
	if q.running {
		return
	}
	q.running = true
	go q.drain()
}

func (q *serialQueue) drain() {
	for {
		q.mu.Lock()
		if len(q.jobs) == 0 {
			q.running = false
			if q.idle != nil {
				q.idle.Broadcast()
			}
			q.mu.Unlock()
			return
		}
		job := q.jobs[0]
		q.jobs = q.jobs[1:]
		q.mu.Unlock()
		job()
	}
}

// Wait blocks until the queue has drained. Jobs submitted while it waits are
// still run before it returns.
func (q *serialQueue) Wait() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.idle == nil {
		q.idle = sync.NewCond(&q.mu)
	}
	for q.running {
		q.idle.Wait()
	}
}
