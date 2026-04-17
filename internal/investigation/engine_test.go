package investigation

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/sentinelai/sentinel/internal/alert"
	"github.com/sentinelai/sentinel/internal/investigation/tool"
	"github.com/sentinelai/sentinel/internal/llm"
	"github.com/sentinelai/sentinel/internal/runbook"
)

// --- Mock stores ---

type mockInvStore struct {
	mu            sync.Mutex
	invs          map[string]*Investigation
	creates       int
	byFingerprint map[string]*Investigation // optional; used by FindByAlertFingerprint
}

func newMockInvStore() *mockInvStore {
	return &mockInvStore{invs: make(map[string]*Investigation)}
}

func (m *mockInvStore) Create(_ context.Context, inv *Investigation) (*Investigation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creates++
	saved := *inv
	if saved.ID == "" {
		saved.ID = "inv-" + time.Now().Format("150405.000")
	}
	saved.CreatedAt = time.Now()
	saved.UpdatedAt = time.Now()
	m.invs[saved.ID] = &saved
	return &saved, nil
}

func (m *mockInvStore) UpdateStatus(_ context.Context, id string, status Status, inv *Investigation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.invs[id]; ok {
		existing.Status = status
		existing.Steps = inv.Steps
		existing.Summary = inv.Summary
		existing.RootCause = inv.RootCause
		existing.Resolution = inv.Resolution
	}
	return nil
}

func (m *mockInvStore) FindByID(_ context.Context, id string) (*Investigation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inv, ok := m.invs[id]; ok {
		copy := *inv
		return &copy, nil
	}
	return nil, ErrNotFound
}

func (m *mockInvStore) FindByAlertFingerprint(_ context.Context, fp string) (*Investigation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byFingerprint != nil {
		if inv, ok := m.byFingerprint[fp]; ok {
			return inv, nil
		}
	}
	return nil, ErrNotFound
}

type mockRBStore struct{}

func (m *mockRBStore) ListEnabled(_ context.Context) ([]*runbook.Runbook, error) {
	return nil, nil
}

// --- Mock LLM ---

type mockProvider struct {
	name     string
	model    string
	response *llm.Response
}

func (m *mockProvider) Name() string  { return m.name }
func (m *mockProvider) Model() string { return m.model }
func (m *mockProvider) Chat(_ context.Context, _ string, _ []llm.Message, _ []llm.Tool) (*llm.Response, error) {
	return m.response, nil
}

// --- Tests ---

func TestEngine_isDuplicate(t *testing.T) {
	e := &Engine{
		cfg:   EngineConfig{DedupWindow: 1 * time.Second}.withDefaults(),
		dedup: make(map[string]time.Time),
	}

	tests := []struct {
		name        string
		fingerprint string
		wantDup     bool
		sleep       time.Duration
	}{
		{"empty fingerprint never duplicate", "", false, 0},
		{"first time not duplicate", "fp-1", false, 0},
		{"second time within window is duplicate", "fp-1", true, 0},
		{"different fingerprint not duplicate", "fp-2", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.sleep > 0 {
				time.Sleep(tt.sleep)
			}
			got := e.isDuplicate(tt.fingerprint)
			if got != tt.wantDup {
				t.Errorf("isDuplicate(%q) = %v, want %v", tt.fingerprint, got, tt.wantDup)
			}
		})
	}

	// After dedup window expires, should no longer be duplicate
	t.Run("expired window not duplicate", func(t *testing.T) {
		time.Sleep(1100 * time.Millisecond)
		if e.isDuplicate("fp-1") {
			t.Error("expected not duplicate after window expiry")
		}
	})
}

func TestEngine_Investigate_DedupSkips(t *testing.T) {
	store := newMockInvStore()
	provider := &mockProvider{
		name:  "test",
		model: "test-model",
		response: &llm.Response{
			Content:    "## Root Cause\nTest\n\n## Resolution\nFix it\n\n## Summary\nDone",
			StopReason: llm.StopReasonEndTurn,
		},
	}

	e := newTestEngine(store, provider)

	evt := &alert.Event{
		ID:          "alert-1",
		Fingerprint: "fp-dedup",
		Severity:    alert.SeverityInfo,
		Title:       "test alert",
	}

	// First call should succeed
	inv, err := e.Investigate(context.Background(), evt)
	if err != nil {
		t.Fatalf("first investigate: %v", err)
	}
	if inv == nil {
		t.Fatal("expected investigation, got nil")
	}

	// Second call with same fingerprint should return dedup error
	_, err = e.Investigate(context.Background(), evt)
	if err != ErrDuplicateInvestigation {
		t.Fatalf("expected ErrDuplicateInvestigation, got %v", err)
	}
}

func TestEngine_Cancel(t *testing.T) {
	e := &Engine{
		cancels: make(map[string]context.CancelFunc),
		log:     slog.Default(),
	}

	// Cancel non-existent should return error
	if err := e.Cancel("not-here"); err != ErrInvestigationNotFound {
		t.Errorf("expected ErrInvestigationNotFound, got %v", err)
	}

	// Register a cancel function and verify it's called
	ctx, cancel := context.WithCancel(context.Background())
	e.cancels["inv-123"] = cancel

	if err := e.Cancel("inv-123"); err != nil {
		t.Errorf("cancel returned error: %v", err)
	}

	// Context should be cancelled
	select {
	case <-ctx.Done():
		// expected
	default:
		t.Error("expected context to be cancelled")
	}
}

func TestEngine_Shutdown(t *testing.T) {
	e := &Engine{
		log: slog.Default(),
	}

	// No active work — should return immediately
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		t.Errorf("shutdown with no active work: %v", err)
	}

	// With active work that completes in time
	e.wg.Add(1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		e.wg.Done()
	}()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()

	if err := e.Shutdown(ctx2); err != nil {
		t.Errorf("shutdown with completing work: %v", err)
	}
}

func TestEngine_Shutdown_Timeout(t *testing.T) {
	e := &Engine{
		log: slog.Default(),
	}

	e.wg.Add(1)
	defer e.wg.Done() // cleanup

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := e.Shutdown(ctx)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// newTestEngine creates an Engine with mock dependencies for testing.
func newTestEngine(store *mockInvStore, provider llm.Provider) *Engine {
	log := slog.Default()
	cfg := EngineConfig{
		Timeout:       30 * time.Second,
		MaxConcurrent: 2,
		DedupWindow:   5 * time.Minute,
	}.withDefaults()

	return &Engine{
		invRepo:  store,
		rbRepo:   &mockRBStore{},
		agent:    NewAgent(provider, tool.NewRegistry(), log),
		notifier: nil,
		hub:      NewSSEHub(),
		cfg:      cfg,
		log:      log,
		sem:      make(chan struct{}, cfg.MaxConcurrent),
		cancels:  make(map[string]context.CancelFunc),
		dedup:    make(map[string]time.Time),
	}
}

// newBareEngine creates a minimal Engine for testing internal helpers that do
// not need a real provider or store.
func newBareEngine(dedupWindow, resultCacheTTL time.Duration) *Engine {
	cfg := EngineConfig{
		DedupWindow:    dedupWindow,
		ResultCacheTTL: resultCacheTTL,
	}.withDefaults()
	return &Engine{
		cfg:     cfg,
		dedup:   make(map[string]time.Time),
		cancels: make(map[string]context.CancelFunc),
		hub:     NewSSEHub(),
		sem:     make(chan struct{}, 5),
	}
}

// ---- isDuplicate additional edge-case tests ----------------------------------

func TestIsDuplicate_EmptyFingerprintAlwaysFalse(t *testing.T) {
	e := newBareEngine(time.Minute, time.Hour)
	// Multiple calls with empty fingerprint must never return true
	for i := 0; i < 5; i++ {
		if e.isDuplicate("") {
			t.Errorf("call %d: empty fingerprint must never be a duplicate", i)
		}
	}
}

func TestIsDuplicate_DifferentFingerprintsAreIndependent(t *testing.T) {
	e := newBareEngine(time.Minute, time.Hour)

	e.isDuplicate("fp-A") // register fp-A
	e.isDuplicate("fp-B") // register fp-B

	// fp-A is a dup now
	if !e.isDuplicate("fp-A") {
		t.Error("fp-A should be duplicate after prior call")
	}
	// fp-C was never seen; must not be duplicate
	if e.isDuplicate("fp-C") {
		t.Error("fp-C was never registered; must not be duplicate")
	}
}

func TestIsDuplicate_EntryExpiredAndReregistered(t *testing.T) {
	e := newBareEngine(50*time.Millisecond, time.Hour)

	e.isDuplicate("fp-expire") // first call — registers

	// Manually back-date the entry so it looks expired
	e.dedupMu.Lock()
	e.dedup["fp-expire"] = time.Now().Add(-time.Second)
	e.dedupMu.Unlock()

	// Should NOT be a duplicate (entry is expired)
	if e.isDuplicate("fp-expire") {
		t.Error("expired entry must not be treated as duplicate")
	}

	// Should NOW be a duplicate (re-registered by the previous call)
	if !e.isDuplicate("fp-expire") {
		t.Error("second call after re-registration must be a duplicate")
	}
}

func TestIsDuplicate_StaleEntriesCleanedOnNextCall(t *testing.T) {
	e := newBareEngine(50*time.Millisecond, time.Hour)

	// Seed a stale entry directly (avoids waiting for real time)
	e.dedupMu.Lock()
	e.dedup["stale"] = time.Now().Add(-time.Hour)
	e.dedupMu.Unlock()

	// Calling isDuplicate with any fingerprint triggers lazy cleanup
	e.isDuplicate("trigger-cleanup")

	e.dedupMu.Lock()
	_, stillPresent := e.dedup["stale"]
	e.dedupMu.Unlock()

	if stillPresent {
		t.Error("stale entry should be removed during lazy cleanup")
	}
}

// ---- findCachedResult tests --------------------------------------------------

func newCacheEngine(store *mockInvStore, ttl time.Duration) *Engine {
	cfg := EngineConfig{
		DedupWindow:    time.Minute,
		ResultCacheTTL: ttl,
	}.withDefaults()
	return &Engine{
		invRepo: store,
		cfg:     cfg,
		dedup:   make(map[string]time.Time),
	}
}

func TestFindCachedResult_EmptyFingerprint_ReturnsNil(t *testing.T) {
	store := newMockInvStore()
	e := newCacheEngine(store, time.Hour)

	evt := &alert.Event{ID: "e1", Fingerprint: ""}
	if got := e.findCachedResult(context.Background(), evt); got != nil {
		t.Errorf("expected nil for empty fingerprint, got %+v", got)
	}
}

func TestFindCachedResult_ZeroTTL_ReturnsNil(t *testing.T) {
	store := newMockInvStore()
	// Build engine then force TTL to zero (withDefaults fills it; override after)
	e := newCacheEngine(store, time.Hour)
	e.cfg.ResultCacheTTL = 0

	now := time.Now()
	store.byFingerprint = map[string]*Investigation{
		"fp-1": {ID: "cached", Status: StatusCompleted, CompletedAt: &now},
	}

	evt := &alert.Event{Fingerprint: "fp-1"}
	if got := e.findCachedResult(context.Background(), evt); got != nil {
		t.Error("zero ResultCacheTTL must disable caching")
	}
}

func TestFindCachedResult_StoreWithoutInterface_ReturnsNil(t *testing.T) {
	// A store that only satisfies InvestigationStore (no FindByAlertFingerprint)
	type minStore struct{ mockInvStore }
	e := &Engine{
		invRepo: &minStore{},
		cfg:     EngineConfig{ResultCacheTTL: time.Hour}.withDefaults(),
		dedup:   make(map[string]time.Time),
	}

	evt := &alert.Event{Fingerprint: "fp-1"}
	if got := e.findCachedResult(context.Background(), evt); got != nil {
		t.Error("store without FindByAlertFingerprint must return nil")
	}
}

func TestFindCachedResult_CompletedWithinTTL_ReturnsCached(t *testing.T) {
	store := newMockInvStore()
	e := newCacheEngine(store, time.Hour)

	completedAt := time.Now().Add(-30 * time.Minute)
	store.byFingerprint = map[string]*Investigation{
		"fp-hit": {
			ID:          "cached-inv",
			Status:      StatusCompleted,
			Feedback:    FeedbackNone,
			CompletedAt: &completedAt,
		},
	}

	evt := &alert.Event{Fingerprint: "fp-hit"}
	got := e.findCachedResult(context.Background(), evt)
	if got == nil {
		t.Fatal("expected cached result, got nil")
	}
	if got.ID != "cached-inv" {
		t.Errorf("got ID %q, want cached-inv", got.ID)
	}
}

func TestFindCachedResult_CompletedOutsideTTL_ReturnsNil(t *testing.T) {
	store := newMockInvStore()
	e := newCacheEngine(store, time.Hour)

	completedAt := time.Now().Add(-2 * time.Hour)
	store.byFingerprint = map[string]*Investigation{
		"fp-old": {
			ID:          "old-inv",
			Status:      StatusCompleted,
			CompletedAt: &completedAt,
		},
	}

	evt := &alert.Event{Fingerprint: "fp-old"}
	if got := e.findCachedResult(context.Background(), evt); got != nil {
		t.Error("result outside TTL must not be reused")
	}
}

func TestFindCachedResult_StatusNotCompleted_ReturnsNil(t *testing.T) {
	store := newMockInvStore()
	e := newCacheEngine(store, time.Hour)

	now := time.Now()
	for _, status := range []Status{StatusPending, StatusRunning, StatusFailed, StatusReused} {
		key := "fp-" + string(status)
		store.byFingerprint = map[string]*Investigation{
			key: {ID: "inv-" + string(status), Status: status, CompletedAt: &now},
		}
		evt := &alert.Event{Fingerprint: key}
		if got := e.findCachedResult(context.Background(), evt); got != nil {
			t.Errorf("status %q must not be cached (got %+v)", status, got)
		}
	}
}

func TestFindCachedResult_FeedbackIncorrect_ReturnsNil(t *testing.T) {
	store := newMockInvStore()
	e := newCacheEngine(store, time.Hour)

	now := time.Now()
	store.byFingerprint = map[string]*Investigation{
		"fp-bad": {
			ID:          "bad-inv",
			Status:      StatusCompleted,
			Feedback:    FeedbackIncorrect,
			CompletedAt: &now,
		},
	}

	evt := &alert.Event{Fingerprint: "fp-bad"}
	if got := e.findCachedResult(context.Background(), evt); got != nil {
		t.Error("incorrectly rated investigation must not be reused")
	}
}

func TestFindCachedResult_FeedbackCorrect_ReturnsCached(t *testing.T) {
	store := newMockInvStore()
	e := newCacheEngine(store, time.Hour)

	now := time.Now()
	store.byFingerprint = map[string]*Investigation{
		"fp-good": {
			ID:          "good-inv",
			Status:      StatusCompleted,
			Feedback:    FeedbackCorrect,
			CompletedAt: &now,
		},
	}

	evt := &alert.Event{Fingerprint: "fp-good"}
	if got := e.findCachedResult(context.Background(), evt); got == nil {
		t.Error("correctly rated completed investigation should be reused")
	}
}

func TestFindCachedResult_NilCompletedAt_ReturnsNil(t *testing.T) {
	store := newMockInvStore()
	e := newCacheEngine(store, time.Hour)

	store.byFingerprint = map[string]*Investigation{
		"fp-notime": {
			ID:          "no-time-inv",
			Status:      StatusCompleted,
			CompletedAt: nil,
		},
	}

	evt := &alert.Event{Fingerprint: "fp-notime"}
	if got := e.findCachedResult(context.Background(), evt); got != nil {
		t.Error("nil CompletedAt must not be reused")
	}
}

func TestFindCachedResult_NotFoundInStore_ReturnsNil(t *testing.T) {
	store := newMockInvStore() // no entries in byFingerprint
	e := newCacheEngine(store, time.Hour)

	evt := &alert.Event{Fingerprint: "fp-missing"}
	if got := e.findCachedResult(context.Background(), evt); got != nil {
		t.Error("fingerprint not in store must return nil")
	}
}
