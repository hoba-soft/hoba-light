package hobalight

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelFilesBoundAndCompletion(t *testing.T) {
	const count, workers = 19, 3
	entered := make(chan struct{}, count)
	release := make(chan struct{})
	done := make(chan struct{})
	calls := make([]atomic.Int32, count)
	progress := 0
	go func() {
		defer close(done)
		parallelFiles(count, workers, func(index int) {
			calls[index].Add(1)
			entered <- struct{}{}
			<-release
		}, func(index, completed int) {
			progress++
			if completed != progress {
				t.Errorf("progress %d != %d", completed, progress)
			}
		})
	}()
	defer func() { close(release); <-done }()
	for i := 0; i < workers; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("workers did not run concurrently")
		}
	}
	select {
	case <-entered:
		t.Fatal("worker limit exceeded")
	default:
	}
	// The deferred release and join ensure no test goroutines survive failures.
}

func TestParallelFilesProcessesEveryIndex(t *testing.T) {
	for _, workers := range []int{1, 4, 100, 0} {
		counts := make([]atomic.Int32, 23)
		completed := 0
		parallelFiles(len(counts), workers, func(index int) { counts[index].Add(1) }, func(index, done int) { completed = done })
		if completed != len(counts) {
			t.Fatal(completed)
		}
		for i := range counts {
			if counts[i].Load() != 1 {
				t.Fatalf("index %d processed %d times", i, counts[i].Load())
			}
		}
	}
	parallelFiles(0, 4, func(int) { t.Fatal("empty work") }, func(int, int) { t.Fatal("empty progress") })
}
