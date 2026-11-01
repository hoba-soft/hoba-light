package hobalight

import "sync"

const defaultWorkers = 4

// Each index is processed once. Completion callbacks run on the caller goroutine.
func parallelFiles(count, workers int, process func(int), completed func(index, done int)) {
	if count == 0 {
		return
	}
	if workers < 1 {
		workers = defaultWorkers
	}
	workers = min(workers, count)
	jobs := make(chan int)
	finished := make(chan int, workers)
	var group sync.WaitGroup
	for n := 0; n < workers; n++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				process(index)
				finished <- index
			}
		}()
	}
	go func() {
		for index := 0; index < count; index++ {
			jobs <- index
		}
		close(jobs)
		group.Wait()
		close(finished)
	}()
	done := 0
	for index := range finished {
		done++
		completed(index, done)
	}
}
