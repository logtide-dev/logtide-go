package batch_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/logtide-dev/logtide-sdk-go/internal/batch"
)

func TestSizeBasedFlushing(t *testing.T) {
	var total int32
	b := batch.New(batch.Options{MaxSize: 3, FlushInterval: time.Minute}, func(_ context.Context, items []string) error {
		atomic.AddInt32(&total, int32(len(items)))
		return nil
	})
	defer b.Stop()

	for i := 0; i < 10; i++ {
		b.Add("msg")
	}
	time.Sleep(100 * time.Millisecond)
	b.Stop()

	if atomic.LoadInt32(&total) != 10 {
		t.Errorf("total = %d, want 10", total)
	}
}

func TestTimeBasedFlushing(t *testing.T) {
	var flushes int32
	b := batch.New(batch.Options{MaxSize: 100, FlushInterval: 50 * time.Millisecond}, func(_ context.Context, _ []string) error {
		atomic.AddInt32(&flushes, 1)
		return nil
	})
	defer b.Stop()

	b.Add("msg1")
	b.Add("msg2")

	time.Sleep(120 * time.Millisecond)
	if atomic.LoadInt32(&flushes) < 1 {
		t.Error("expected at least one time-based flush")
	}
}

func TestManualFlush(t *testing.T) {
	var got []string
	var mu sync.Mutex
	b := batch.New(batch.Options{MaxSize: 100, FlushInterval: time.Minute}, func(_ context.Context, items []string) error {
		mu.Lock()
		got = append(got, items...)
		mu.Unlock()
		return nil
	})
	defer b.Stop()

	for i := 0; i < 5; i++ {
		b.Add("x")
	}
	if err := b.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n != 5 {
		t.Errorf("flushed %d, want 5", n)
	}
}

func TestStopFlushesRemaining(t *testing.T) {
	var total int32
	b := batch.New(batch.Options{MaxSize: 100, FlushInterval: time.Minute}, func(_ context.Context, items []string) error {
		atomic.AddInt32(&total, int32(len(items)))
		return nil
	})

	for i := 0; i < 7; i++ {
		b.Add("msg")
	}
	b.Stop()

	if atomic.LoadInt32(&total) != 7 {
		t.Errorf("total after stop = %d, want 7", total)
	}

	if err := b.Add("after-stop"); err != batch.ErrStopped {
		t.Errorf("Add after stop: got %v, want ErrStopped", err)
	}
}

func TestEmptyFlushIsNoop(t *testing.T) {
	called := false
	b := batch.New(batch.Options{MaxSize: 100, FlushInterval: time.Minute}, func(_ context.Context, _ []string) error {
		called = true
		return nil
	})
	defer b.Stop()

	b.Flush(context.Background())
	if called {
		t.Error("flush function should not be called for empty batch")
	}
}

func TestConcurrentAdds(t *testing.T) {
	var total int32
	b := batch.New(batch.Options{MaxSize: 50, FlushInterval: 50 * time.Millisecond}, func(_ context.Context, items []string) error {
		atomic.AddInt32(&total, int32(len(items)))
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				b.Add("msg")
			}
		}()
	}
	wg.Wait()
	b.Stop()

	if atomic.LoadInt32(&total) != 200 {
		t.Errorf("total = %d, want 200", total)
	}
}
