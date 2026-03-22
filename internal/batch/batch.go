// Package batch provides a goroutine-safe generic batch accumulator
// with size-triggered and time-triggered flushing.
package batch

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrStopped is returned by Add when the batch has been stopped.
var ErrStopped = errors.New("batch: stopped")

// FlushFunc is called by the batch engine to deliver accumulated items.
type FlushFunc[T any] func(ctx context.Context, items []T) error

// Options configures a Batch.
type Options struct {
	// MaxSize is the number of items that triggers an immediate flush.
	// Default: 100.
	MaxSize int

	// FlushInterval is the maximum time between automatic flushes.
	// Default: 5s.
	FlushInterval time.Duration

	// FlushTimeout is the deadline used for the final flush on Stop.
	// Default: 10s.
	FlushTimeout time.Duration
}

// Batch accumulates items of type T and dispatches them in groups via FlushFunc.
// It is safe for concurrent use.
type Batch[T any] struct {
	mu            sync.Mutex
	items         []T
	maxSize       int
	flushInterval time.Duration
	flushTimeout  time.Duration
	flushFn       FlushFunc[T]

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	flushChan chan struct{}
	stopped   bool
}

// New creates and starts a Batch with the given options and flush function.
// Panics if fn is nil.
func New[T any](opts Options, fn FlushFunc[T]) *Batch[T] {
	if fn == nil {
		panic("batch: flush function cannot be nil")
	}
	if opts.MaxSize <= 0 {
		opts.MaxSize = 100
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 5 * time.Second
	}
	if opts.FlushTimeout <= 0 {
		opts.FlushTimeout = 10 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	b := &Batch[T]{
		items:         make([]T, 0, opts.MaxSize),
		maxSize:       opts.MaxSize,
		flushInterval: opts.FlushInterval,
		flushTimeout:  opts.FlushTimeout,
		flushFn:       fn,
		ctx:           ctx,
		cancel:        cancel,
		flushChan:     make(chan struct{}, 1),
	}

	b.wg.Add(1)
	go b.backgroundFlusher()
	return b
}

// Add appends item to the batch. If the batch reaches MaxSize, a flush is
// triggered asynchronously. Returns ErrStopped if the batch has been stopped.
func (b *Batch[T]) Add(item T) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.stopped {
		return ErrStopped
	}

	b.items = append(b.items, item)
	if len(b.items) >= b.maxSize {
		select {
		case b.flushChan <- struct{}{}:
		default:
			// flush already pending
		}
	}
	return nil
}

// Flush immediately drains all buffered items by calling FlushFunc.
// It is a no-op if the buffer is empty.
func (b *Batch[T]) Flush(ctx context.Context) error {
	b.mu.Lock()
	if len(b.items) == 0 {
		b.mu.Unlock()
		return nil
	}
	items := make([]T, len(b.items))
	copy(items, b.items)
	b.items = b.items[:0]
	b.mu.Unlock()

	return b.flushFn(ctx, items)
}

// Stop signals the background goroutine to exit, waits for it, then performs
// a final flush of any remaining items using FlushTimeout as the deadline.
func (b *Batch[T]) Stop() error {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return nil
	}
	b.stopped = true
	b.mu.Unlock()

	b.cancel()
	b.wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), b.flushTimeout)
	defer cancel()
	return b.Flush(ctx)
}

func (b *Batch[T]) backgroundFlusher() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.flushWithTimeout()
		case <-b.flushChan:
			b.flushWithTimeout()
		}
	}
}

// flushWithTimeout performs a single flush with its own context, independent
// of the batch lifecycle context. This prevents items from being lost when
// Stop cancels the lifecycle context while a flush is in progress.
func (b *Batch[T]) flushWithTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), b.flushTimeout)
	defer cancel()
	_ = b.Flush(ctx)
}
