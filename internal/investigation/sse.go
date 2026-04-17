package investigation

import (
	"encoding/json"
	"sync"
)

// StepEvent is the payload sent to SSE subscribers.
type StepEvent struct {
	InvestigationID string `json:"investigation_id"`
	Step            Step   `json:"step"`
	TotalSteps      int    `json:"total_steps"`
}

// SSEHub manages per-investigation SSE subscribers.
type SSEHub struct {
	mu          sync.RWMutex
	subscribers map[string][]chan StepEvent // investigation_id → channels
}

func NewSSEHub() *SSEHub {
	return &SSEHub{
		subscribers: make(map[string][]chan StepEvent),
	}
}

// Subscribe returns a channel that receives step events for the given
// investigation. The caller must call Unsubscribe when done.
func (h *SSEHub) Subscribe(invID string) chan StepEvent {
	ch := make(chan StepEvent, 16)
	h.mu.Lock()
	h.subscribers[invID] = append(h.subscribers[invID], ch)
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes a channel from the hub and closes it.
func (h *SSEHub) Unsubscribe(invID string, ch chan StepEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs := h.subscribers[invID]
	for i, sub := range subs {
		if sub == ch {
			h.subscribers[invID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
	if len(h.subscribers[invID]) == 0 {
		delete(h.subscribers, invID)
	}
}

// Broadcast sends a step event to all subscribers of the given investigation.
func (h *SSEHub) Broadcast(invID string, step Step, totalSteps int) {
	evt := StepEvent{
		InvestigationID: invID,
		Step:            step,
		TotalSteps:      totalSteps,
	}

	h.mu.RLock()
	subs := h.subscribers[invID]
	h.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
			// Drop if subscriber is slow
		}
	}
}

// MarshalEvent formats a StepEvent as an SSE data line.
func MarshalEvent(evt StepEvent) []byte {
	data, _ := json.Marshal(evt)
	// SSE format: "data: {json}\n\n"
	buf := make([]byte, 0, len(data)+8)
	buf = append(buf, "data: "...)
	buf = append(buf, data...)
	buf = append(buf, '\n', '\n')
	return buf
}
