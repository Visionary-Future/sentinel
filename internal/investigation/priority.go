package investigation

import (
	"container/heap"
	"sync"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/runbook"
)

// investigationJob represents a queued investigation request.
type investigationJob struct {
	evt      *alert.Event
	rb       *runbook.Runbook
	inv      *Investigation
	priority int // higher = more urgent
	index    int // heap index
}

// jobHeap implements heap.Interface for priority scheduling.
type jobHeap []*investigationJob

func (h jobHeap) Len() int           { return len(h) }
func (h jobHeap) Less(i, j int) bool { return h[i].priority > h[j].priority } // max-heap
func (h jobHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *jobHeap) Push(x any) {
	n := len(*h)
	job := x.(*investigationJob)
	job.index = n
	*h = append(*h, job)
}

func (h *jobHeap) Pop() any {
	old := *h
	n := len(old)
	job := old[n-1]
	old[n-1] = nil
	job.index = -1
	*h = old[:n-1]
	return job
}

// PriorityQueue is a thread-safe priority queue for investigation jobs.
type PriorityQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	heap   jobHeap
	closed bool
}

func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{}
	pq.cond = sync.NewCond(&pq.mu)
	heap.Init(&pq.heap)
	return pq
}

// Enqueue adds a job to the queue. Higher severity = higher priority.
func (pq *PriorityQueue) Enqueue(evt *alert.Event, rb *runbook.Runbook, inv *Investigation) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	job := &investigationJob{
		evt:      evt,
		rb:       rb,
		inv:      inv,
		priority: severityToPriority(evt.Severity),
	}
	heap.Push(&pq.heap, job)
	pq.cond.Signal()
}

// Dequeue blocks until a job is available, then returns it.
// Returns nil if the queue is closed.
func (pq *PriorityQueue) Dequeue() *investigationJob {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	for pq.heap.Len() == 0 && !pq.closed {
		pq.cond.Wait()
	}

	if pq.closed && pq.heap.Len() == 0 {
		return nil
	}

	return heap.Pop(&pq.heap).(*investigationJob)
}

// Len returns the current queue size.
func (pq *PriorityQueue) Len() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.heap.Len()
}

// Close signals all waiting Dequeue calls to return.
func (pq *PriorityQueue) Close() {
	pq.mu.Lock()
	pq.closed = true
	pq.mu.Unlock()
	pq.cond.Broadcast()
}

func severityToPriority(s alert.Severity) int {
	switch s {
	case alert.SeverityCritical:
		return 100
	case alert.SeverityWarning:
		return 50
	case alert.SeverityInfo:
		return 10
	default:
		return 1
	}
}
