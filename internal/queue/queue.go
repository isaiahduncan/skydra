// Package queue is the bounded per-path queue. It is a Go channel behind a
// small abstraction so production can swap in a Kafka producer and consumer.
package queue

import "sync/atomic"

// Queue never blocks the producer: when full, Offer drops the newest event
// (the one being offered) and counts the drop.
type Queue[E any] struct {
	ch    chan E
	drops atomic.Uint64
}

func New[E any](size int) *Queue[E] {
	return &Queue[E]{ch: make(chan E, size)}
}

// Offer enqueues e without blocking. It reports false and counts a drop when
// the queue is full.
func (q *Queue[E]) Offer(e E) bool {
	select {
	case q.ch <- e:
		return true
	default:
		q.drops.Add(1)
		return false
	}
}

// Chan is the consumer side, read by the handler loop.
func (q *Queue[E]) Chan() <-chan E { return q.ch }

// Drops is the total number of events dropped because the queue was full.
func (q *Queue[E]) Drops() uint64 { return q.drops.Load() }

func (q *Queue[E]) Len() int { return len(q.ch) }
