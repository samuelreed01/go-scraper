package main

import (
	"fmt"
	"sync"
	"time"
)

// WorkerPool represents a pool of workers that process tasks concurrently
type WorkerPool[T any] struct {
	maxWorkers   int
	taskQueue    chan string
	resultQueue  chan TaskResult[T]
	results      []TaskResult[T]
	resultsMux   sync.RWMutex
	processed    map[string]bool // Track processed items
	processedMux sync.RWMutex    // Mutex for processed map
	wg           sync.WaitGroup
}

// TaskResult represents the result of processing a task
type TaskResult[T any] struct {
	Data   string
	Result T
	Error  error
}

// TaskFunction defines the signature for functions that process tasks
// Returns a result (any type) and an error
type TaskFunction[T any] func(string) (T, error)

// NewWorkerPool creates a new worker pool with the specified number of workers
func NewWorkerPool[T any](maxWorkers int) *WorkerPool[T] {
	return &WorkerPool[T]{
		maxWorkers:  maxWorkers,
		taskQueue:   make(chan string, maxWorkers*2), // Buffer to prevent blocking
		resultQueue: make(chan TaskResult[T], maxWorkers*2),
		results:     make([]TaskResult[T], 0),
		processed:   make(map[string]bool),
	}
}

// Start initializes and starts the worker pool
func (wp *WorkerPool[T]) Start(taskFunc TaskFunction[T]) {
	// Start result collector goroutine
	go wp.resultCollector()

	// Start the specified number of workers
	for i := 0; i < wp.maxWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker(i, taskFunc)
	}
}

// resultCollector collects results from workers
func (wp *WorkerPool[T]) resultCollector() {
	for result := range wp.resultQueue {
		wp.resultsMux.Lock()
		wp.results = append(wp.results, result)
		wp.resultsMux.Unlock()
	}
}

// worker is the goroutine that processes tasks from the queue
func (wp *WorkerPool[T]) worker(workerID int, taskFunc TaskFunction[T]) {
	defer wp.wg.Done()

	for data := range wp.taskQueue {
		// Execute the task function
		result, err := taskFunc(data)

		// Create task result
		taskResult := TaskResult[T]{
			Data:   data,
			Result: result,
			Error:  err,
		}

		// Send result to collector
		wp.resultQueue <- taskResult

		if err != nil {
			fmt.Printf("Worker %d: Error processing %s: %v\n", workerID, data, err)
		}
	}
}

// AddTask adds a new task to the queue if it hasn't been processed yet
// Returns true if the task was added, false if it was already processed/queued
func (wp *WorkerPool[T]) AddTask(data string) bool {
	wp.processedMux.Lock()
	defer wp.processedMux.Unlock()

	// Check if already processed or queued
	if wp.processed[data] {
		return false
	}

	// Mark as processed (queued) and add to queue
	wp.processed[data] = true
	wp.taskQueue <- data
	return true
}

// AddTasks adds multiple tasks from a string array, skipping duplicates
// Returns the number of tasks actually added
func (wp *WorkerPool[T]) AddTasks(items []string) int {
	added := 0
	for _, item := range items {
		if wp.AddTask(item) {
			added++
		}
	}
	return added
}

// HasBeenProcessed checks if a string has already been processed or queued
func (wp *WorkerPool[T]) HasBeenProcessed(data string) bool {
	wp.processedMux.RLock()
	defer wp.processedMux.RUnlock()
	return wp.processed[data]
}

// Stop closes the task queue and waits for all workers to finish
func (wp *WorkerPool[T]) Stop() {
	close(wp.taskQueue)
	wp.wait()
	close(wp.resultQueue)
	// Give result collector time to finish
	time.Sleep(time.Millisecond * 10)
}

// GetResults returns a copy of all collected results
func (wp *WorkerPool[T]) GetResults() []TaskResult[T] {
	wp.resultsMux.RLock()
	defer wp.resultsMux.RUnlock()

	// Return a copy to avoid race conditions
	resultsCopy := make([]TaskResult[T], len(wp.results))
	copy(resultsCopy, wp.results)
	return resultsCopy
}

// GetResultsMap returns results organized by data string for easy lookup
func (wp *WorkerPool[T]) GetResultsMap() map[string]TaskResult[T] {
	wp.resultsMux.RLock()
	defer wp.resultsMux.RUnlock()

	resultsMap := make(map[string]TaskResult[T])
	for _, result := range wp.results {
		resultsMap[result.Data] = result
	}
	return resultsMap
}

// Wait waits for all workers to complete their current tasks
func (wp *WorkerPool[T]) wait() {
	wp.wg.Wait()
}
