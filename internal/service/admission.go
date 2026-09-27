package service

import (
	"context"
	"errors"
	"sync"
)

var errAdmissionStopped = errors.New("generation admission stopped")

// Admission limits concurrent generations to MaxConcurrentGenerations,
// which defaults to the number of logical CPUs. gRPC and the HTTP debug
// server share this one limiter; HTTP does not keep its own queue.
// Callers beyond the limit wait while their Context is alive. A deadline
// that expires before a slot is taken returns DeadlineExceeded. Each
// process creates exactly one instance.
type Admission struct {
	tokens    chan struct{}
	stopped   chan struct{}
	mutex     sync.Mutex
	accepting bool
	stopOnce  sync.Once
	active    sync.WaitGroup
}

// NewAdmission creates a limiter with the given positive concurrency.
func NewAdmission(limit int) *Admission {
	if limit < 1 {
		panic("admission limit must be greater than zero")
	}
	return &Admission{
		tokens:    make(chan struct{}, limit),
		stopped:   make(chan struct{}),
		accepting: true,
	}
}

func (admission *Admission) acquire(ctx context.Context) error {
	select {
	case <-admission.stopped:
		return errAdmissionStopped
	default:
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case admission.tokens <- struct{}{}:
		admission.mutex.Lock()
		if !admission.accepting {
			admission.mutex.Unlock()
			<-admission.tokens
			return errAdmissionStopped
		}
		// Add runs under the same mutex as Stop; after Stop returns,
		// Wait never races with a late Add on a zero counter.
		admission.active.Add(1)
		admission.mutex.Unlock()
		return nil
	case <-admission.stopped:
		return errAdmissionStopped
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (admission *Admission) release() {
	<-admission.tokens
	admission.active.Done()
}

// Stop stops new admissions and wakes calls waiting for a slot.
// Calls already admitted continue until their Contexts are canceled.
func (admission *Admission) Stop() {
	admission.stopOnce.Do(func() {
		admission.mutex.Lock()
		admission.accepting = false
		close(admission.stopped)
		admission.mutex.Unlock()
	})
}

// Wait waits for every admitted generation to finish or for the Context to expire.
func (admission *Admission) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		admission.active.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
