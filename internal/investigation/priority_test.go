package investigation

import (
	"sync"
	"testing"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
)

func makeJob(id string, severity alert.Severity) (*alert.Event, *Investigation) {
	return &alert.Event{ID: id, Severity: severity}, &Investigation{ID: id}
}

// ---- severityToPriority ------------------------------------------------------

func TestSeverityToPriority(t *testing.T) {
	tests := []struct {
		severity alert.Severity
		want     int
	}{
		{alert.SeverityCritical, 100},
		{alert.SeverityWarning, 50},
		{alert.SeverityInfo, 10},
		{alert.Severity("unknown"), 1},
		{alert.Severity(""), 1},
	}
	for _, tc := range tests {
		got := severityToPriority(tc.severity)
		if got != tc.want {
			t.Errorf("severityToPriority(%q) = %d, want %d", tc.severity, got, tc.want)
		}
	}
}

// ---- NewPriorityQueue / Len --------------------------------------------------

func TestPriorityQueue_StartsEmpty(t *testing.T) {
	pq := NewPriorityQueue()
	if pq.Len() != 0 {
		t.Errorf("expected empty queue, got Len=%d", pq.Len())
	}
}

// ---- Enqueue / Dequeue ordering ----------------------------------------------

func TestPriorityQueue_DequeueOrder(t *testing.T) {
	pq := NewPriorityQueue()

	// Enqueue in low→high severity order; expect dequeue in high→low order.
	infoEvt, infoInv := makeJob("info-1", alert.SeverityInfo)
	warnEvt, warnInv := makeJob("warn-1", alert.SeverityWarning)
	critEvt, critInv := makeJob("crit-1", alert.SeverityCritical)

	pq.Enqueue(infoEvt, nil, infoInv)
	pq.Enqueue(warnEvt, nil, warnInv)
	pq.Enqueue(critEvt, nil, critInv)

	if pq.Len() != 3 {
		t.Fatalf("expected Len 3, got %d", pq.Len())
	}

	expected := []string{"crit-1", "warn-1", "info-1"}
	for i, wantID := range expected {
		job := pq.Dequeue()
		if job == nil {
			t.Fatalf("step %d: Dequeue returned nil", i)
		}
		if job.evt.ID != wantID {
			t.Errorf("step %d: got %q, want %q", i, job.evt.ID, wantID)
		}
	}
}

func TestPriorityQueue_MultipleEnqueueSameSeverity(t *testing.T) {
	pq := NewPriorityQueue()

	for i := 0; i < 5; i++ {
		evt, inv := makeJob("warn", alert.SeverityWarning)
		pq.Enqueue(evt, nil, inv)
	}

	if pq.Len() != 5 {
		t.Fatalf("expected Len 5, got %d", pq.Len())
	}

	for i := 0; i < 5; i++ {
		if pq.Dequeue() == nil {
			t.Errorf("step %d: Dequeue returned nil unexpectedly", i)
		}
	}
}

func TestPriorityQueue_CriticalBeforeWarningBeforeInfo(t *testing.T) {
	pq := NewPriorityQueue()

	// Enqueue in mixed order
	pairs := []struct {
		id  string
		sev alert.Severity
	}{
		{"w1", alert.SeverityWarning},
		{"i1", alert.SeverityInfo},
		{"c1", alert.SeverityCritical},
		{"i2", alert.SeverityInfo},
		{"c2", alert.SeverityCritical},
	}
	for _, p := range pairs {
		evt, inv := makeJob(p.id, p.sev)
		pq.Enqueue(evt, nil, inv)
	}

	// Collect actual dequeue order priorities
	prev := 101
	for pq.Len() > 0 {
		job := pq.Dequeue()
		curr := job.priority
		if curr > prev {
			t.Errorf("out-of-order dequeue: got priority %d after %d", curr, prev)
		}
		prev = curr
	}
}

// ---- Close unblocks Dequeue --------------------------------------------------

func TestPriorityQueue_CloseUnblocksDequeue(t *testing.T) {
	pq := NewPriorityQueue()

	done := make(chan *investigationJob, 1)
	go func() {
		done <- pq.Dequeue()
	}()

	// Give the goroutine time to block on Dequeue
	time.Sleep(10 * time.Millisecond)

	pq.Close()

	select {
	case job := <-done:
		if job != nil {
			t.Errorf("expected nil after Close, got %v", job)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timed out: Dequeue did not unblock after Close")
	}
}

func TestPriorityQueue_CloseReturnsPendingJobsFirst(t *testing.T) {
	pq := NewPriorityQueue()

	evt, inv := makeJob("crit-1", alert.SeverityCritical)
	pq.Enqueue(evt, nil, inv)
	pq.Close()

	// Items already in the queue should still be returned
	job := pq.Dequeue()
	if job == nil {
		t.Fatal("expected queued job to be returned before nil")
	}
	if job.evt.ID != "crit-1" {
		t.Errorf("expected crit-1, got %s", job.evt.ID)
	}

	// Next call should return nil (queue empty and closed)
	if pq.Dequeue() != nil {
		t.Error("expected nil after all items drained from closed queue")
	}
}

// ---- Len ---------------------------------------------------------------------

func TestPriorityQueue_LenTracksEnqueueDequeue(t *testing.T) {
	pq := NewPriorityQueue()

	for i := 0; i < 3; i++ {
		evt, inv := makeJob("info", alert.SeverityInfo)
		pq.Enqueue(evt, nil, inv)
	}

	if pq.Len() != 3 {
		t.Errorf("after 3 enqueues: Len = %d, want 3", pq.Len())
	}

	pq.Dequeue()
	if pq.Len() != 2 {
		t.Errorf("after 1 dequeue: Len = %d, want 2", pq.Len())
	}
}

// ---- Concurrent access -------------------------------------------------------

func TestPriorityQueue_ConcurrentEnqueueDequeue(t *testing.T) {
	const producers = 10
	const jobsPerProducer = 100

	pq := NewPriorityQueue()

	var wgProd sync.WaitGroup
	wgProd.Add(producers)

	for p := 0; p < producers; p++ {
		sev := alert.SeverityInfo
		if p%3 == 0 {
			sev = alert.SeverityCritical
		} else if p%3 == 1 {
			sev = alert.SeverityWarning
		}
		go func(severity alert.Severity) {
			defer wgProd.Done()
			for i := 0; i < jobsPerProducer; i++ {
				evt, inv := makeJob("id", severity)
				pq.Enqueue(evt, nil, inv)
			}
		}(sev)
	}

	total := producers * jobsPerProducer
	received := make(chan struct{}, total)

	var wgCons sync.WaitGroup
	wgCons.Add(1)
	go func() {
		defer wgCons.Done()
		for i := 0; i < total; i++ {
			job := pq.Dequeue()
			if job == nil {
				return
			}
			received <- struct{}{}
		}
	}()

	wgProd.Wait()
	wgCons.Wait()

	if len(received) != total {
		t.Errorf("expected %d jobs consumed, got %d", total, len(received))
	}
}

func TestPriorityQueue_ConcurrentClose(t *testing.T) {
	pq := NewPriorityQueue()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pq.Dequeue() // may return nil if closed before item arrives
		}()
	}

	time.Sleep(5 * time.Millisecond)
	pq.Close()
	wg.Wait() // all goroutines must unblock
}
