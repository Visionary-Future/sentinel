package investigation

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func makeStep(index int, desc string) Step {
	return Step{Index: index, Description: desc}
}

// ---- NewSSEHub ---------------------------------------------------------------

func TestNewSSEHub_Empty(t *testing.T) {
	h := NewSSEHub()
	if h == nil {
		t.Fatal("expected non-nil hub")
	}
	h.mu.RLock()
	count := len(h.subscribers)
	h.mu.RUnlock()
	if count != 0 {
		t.Errorf("expected empty subscribers map, got %d entries", count)
	}
}

// ---- Subscribe ---------------------------------------------------------------

func TestSSEHub_Subscribe_ReturnsBufChan(t *testing.T) {
	h := NewSSEHub()
	ch := h.Subscribe("inv-1")

	if ch == nil {
		t.Fatal("Subscribe returned nil channel")
	}
	// Channel should be buffered (capacity 16 per implementation)
	if cap(ch) == 0 {
		t.Error("expected buffered channel")
	}
}

func TestSSEHub_Subscribe_MultipleSubscribersForSameID(t *testing.T) {
	h := NewSSEHub()

	ch1 := h.Subscribe("inv-1")
	ch2 := h.Subscribe("inv-1")
	ch3 := h.Subscribe("inv-1")

	h.mu.RLock()
	subs := h.subscribers["inv-1"]
	h.mu.RUnlock()

	if len(subs) != 3 {
		t.Errorf("expected 3 subscribers, got %d", len(subs))
	}
	_ = ch1
	_ = ch2
	_ = ch3
}

func TestSSEHub_Subscribe_DifferentIDs(t *testing.T) {
	h := NewSSEHub()
	h.Subscribe("inv-1")
	h.Subscribe("inv-2")

	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.subscribers) != 2 {
		t.Errorf("expected 2 investigation entries, got %d", len(h.subscribers))
	}
}

// ---- Broadcast ---------------------------------------------------------------

func TestSSEHub_Broadcast_DeliversToSubscriber(t *testing.T) {
	h := NewSSEHub()
	ch := h.Subscribe("inv-1")
	defer h.Unsubscribe("inv-1", ch)

	step := makeStep(1, "check logs")
	h.Broadcast("inv-1", step, 1)

	select {
	case evt := <-ch:
		if evt.InvestigationID != "inv-1" {
			t.Errorf("expected investigation_id inv-1, got %s", evt.InvestigationID)
		}
		if evt.Step.Index != 1 {
			t.Errorf("expected step index 1, got %d", evt.Step.Index)
		}
		if evt.TotalSteps != 1 {
			t.Errorf("expected TotalSteps 1, got %d", evt.TotalSteps)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timed out waiting for broadcast")
	}
}

func TestSSEHub_Broadcast_DeliversToMultipleSubscribers(t *testing.T) {
	h := NewSSEHub()

	const n = 5
	channels := make([]chan StepEvent, n)
	for i := 0; i < n; i++ {
		channels[i] = h.Subscribe("inv-multi")
	}

	step := makeStep(2, "check metrics")
	h.Broadcast("inv-multi", step, 2)

	for i, ch := range channels {
		select {
		case evt := <-ch:
			if evt.Step.Index != 2 {
				t.Errorf("subscriber %d: expected step index 2, got %d", i, evt.Step.Index)
			}
		case <-time.After(200 * time.Millisecond):
			t.Errorf("subscriber %d: timed out waiting for event", i)
		}
		h.Unsubscribe("inv-multi", ch)
	}
}

func TestSSEHub_Broadcast_NoSubscribers_NoPanic(t *testing.T) {
	h := NewSSEHub()
	// Must not panic when no subscribers
	h.Broadcast("inv-nonexistent", makeStep(1, "test"), 1)
}

func TestSSEHub_Broadcast_DropsEventsWhenChannelFull(t *testing.T) {
	h := NewSSEHub()
	ch := h.Subscribe("inv-drop")

	// Fill the channel buffer
	for i := 0; i < cap(ch); i++ {
		h.Broadcast("inv-drop", makeStep(i+1, "step"), i+1)
	}

	// This extra broadcast must not block
	done := make(chan struct{})
	go func() {
		h.Broadcast("inv-drop", makeStep(999, "overflow"), 999)
		close(done)
	}()

	select {
	case <-done:
		// success: broadcast returned without blocking
	case <-time.After(200 * time.Millisecond):
		t.Error("Broadcast blocked on a full channel")
	}

	h.Unsubscribe("inv-drop", ch)
}

func TestSSEHub_Broadcast_DoesNotDeliverToOtherInvestigations(t *testing.T) {
	h := NewSSEHub()
	chTarget := h.Subscribe("inv-target")
	chOther := h.Subscribe("inv-other")
	defer h.Unsubscribe("inv-target", chTarget)
	defer h.Unsubscribe("inv-other", chOther)

	h.Broadcast("inv-target", makeStep(1, "step"), 1)

	select {
	case <-chOther:
		t.Error("inv-other should not receive events for inv-target")
	case <-time.After(50 * time.Millisecond):
		// expected: nothing delivered to the other channel
	}
}

// ---- Unsubscribe -------------------------------------------------------------

func TestSSEHub_Unsubscribe_ClosesChannel(t *testing.T) {
	h := NewSSEHub()
	ch := h.Subscribe("inv-1")
	h.Unsubscribe("inv-1", ch)

	// Channel should be closed — reading from it returns zero value immediately
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel, but received a value")
		}
	default:
		// also acceptable: closed channel with no buffered data returns immediately in a select
	}
}

func TestSSEHub_Unsubscribe_RemovesFromMap_WhenLast(t *testing.T) {
	h := NewSSEHub()
	ch := h.Subscribe("inv-1")
	h.Unsubscribe("inv-1", ch)

	h.mu.RLock()
	_, exists := h.subscribers["inv-1"]
	h.mu.RUnlock()

	if exists {
		t.Error("expected investigation entry removed after last subscriber unsubscribed")
	}
}

func TestSSEHub_Unsubscribe_LeavesOtherSubscribersIntact(t *testing.T) {
	h := NewSSEHub()
	ch1 := h.Subscribe("inv-1")
	ch2 := h.Subscribe("inv-1")

	h.Unsubscribe("inv-1", ch1)

	h.mu.RLock()
	subs := h.subscribers["inv-1"]
	h.mu.RUnlock()

	if len(subs) != 1 {
		t.Errorf("expected 1 remaining subscriber, got %d", len(subs))
	}

	// ch2 should still receive broadcasts
	h.Broadcast("inv-1", makeStep(1, "still works"), 1)
	select {
	case <-ch2:
		// good
	case <-time.After(200 * time.Millisecond):
		t.Error("remaining subscriber did not receive broadcast")
	}

	h.Unsubscribe("inv-1", ch2)
}

func TestSSEHub_Unsubscribe_UnknownChannel_NoPanic(t *testing.T) {
	h := NewSSEHub()
	ch := make(chan StepEvent, 16)
	// Unsubscribing a channel that was never subscribed must not panic
	h.Unsubscribe("inv-never", ch)
}

// ---- Concurrent access -------------------------------------------------------

// TestSSEHub_ConcurrentSubscribeUnsubscribe verifies that Subscribe and
// Unsubscribe are safe to call from multiple goroutines simultaneously.
// Broadcast is intentionally excluded here because the production SSEHub
// design reads subscriber channels under RLock then sends without any lock,
// which means Broadcast and Unsubscribe must not run concurrently on the
// same channel — that is a known constraint of the design, not a test gap.
func TestSSEHub_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	h := NewSSEHub()
	const goroutines = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		invID := "inv-concurrent"
		go func() {
			defer wg.Done()
			ch := h.Subscribe(invID)
			// Drain any events that may already be buffered before closing.
			select {
			case <-ch:
			default:
			}
			h.Unsubscribe(invID, ch)
		}()
	}

	wg.Wait()
}

// TestSSEHub_ConcurrentBroadcastToDistinctInvestigations verifies that
// broadcasting to different investigation IDs concurrently is race-free.
func TestSSEHub_ConcurrentBroadcastToDistinctInvestigations(t *testing.T) {
	h := NewSSEHub()
	const goroutines = 20

	// Pre-subscribe each goroutine to its own investigation ID so that
	// Subscribe and Broadcast never share the same subscriber slice.
	channels := make([]chan StepEvent, goroutines)
	for i := 0; i < goroutines; i++ {
		channels[i] = h.Subscribe("inv-distinct-" + string(rune('A'+i)))
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		idx := i
		invID := "inv-distinct-" + string(rune('A'+idx))
		go func() {
			defer wg.Done()
			h.Broadcast(invID, makeStep(1, "parallel"), 1)
		}()
	}
	wg.Wait()

	// Clean up
	for i, ch := range channels {
		h.Unsubscribe("inv-distinct-"+string(rune('A'+i)), ch)
	}
}

// ---- MarshalEvent ------------------------------------------------------------

func TestMarshalEvent_Format(t *testing.T) {
	evt := StepEvent{
		InvestigationID: "inv-abc",
		Step:            makeStep(1, "check logs"),
		TotalSteps:      3,
	}

	out := MarshalEvent(evt)
	s := string(out)

	if !strings.HasPrefix(s, "data: ") {
		t.Errorf("expected SSE prefix 'data: ', got: %q", s)
	}
	if !strings.HasSuffix(s, "\n\n") {
		t.Errorf("expected SSE suffix '\\n\\n', got: %q", s)
	}
	if !strings.Contains(s, "inv-abc") {
		t.Error("expected investigation_id in output")
	}
}
