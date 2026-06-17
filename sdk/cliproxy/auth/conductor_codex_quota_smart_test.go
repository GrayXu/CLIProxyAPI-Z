package auth

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type codexQuotaSmartTestExecutor struct {
	mu           sync.Mutex
	refreshCount int
}

func (e *codexQuotaSmartTestExecutor) Identifier() string { return "codex" }

func (e *codexQuotaSmartTestExecutor) Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	_ = opts
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

func (e *codexQuotaSmartTestExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	_ = ctx
	_ = auth
	_ = req
	_ = opts
	ch := make(chan cliproxyexecutor.StreamChunk)
	close(ch)
	return &cliproxyexecutor.StreamResult{Chunks: ch}, nil
}

func (e *codexQuotaSmartTestExecutor) Refresh(ctx context.Context, auth *Auth) (*Auth, error) {
	_ = ctx
	e.mu.Lock()
	e.refreshCount++
	e.mu.Unlock()
	updated := auth.Clone()
	UpdateCodexQuotaSmartStateFromSnapshot(updated, `{"plan_type":"pro","rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":20,"reset_after_seconds":120},"secondary_window":{"limit_window_seconds":604800,"used_percent":10,"reset_after_seconds":3600}}}`, time.Now().UTC())
	return updated, nil
}

func (e *codexQuotaSmartTestExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	_ = opts
	return cliproxyexecutor.Response{}, nil
}

func (e *codexQuotaSmartTestExecutor) HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	return nil, nil
}

func (e *codexQuotaSmartTestExecutor) RefreshCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.refreshCount
}

func TestManagerExecute_CodexQuotaSmartRecordsLocalUsageAndTriggersPrewarmRefresh(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &CodexQuotaSmartSelector{}, nil)
	t.Cleanup(manager.Close)
	executor := &codexQuotaSmartTestExecutor{}
	manager.RegisterExecutor(executor)

	auth := &Auth{
		ID:       "codex-smart-auth",
		Provider: "codex",
		Metadata: map[string]any{
			"account_id":   "acct-smart",
			"access_token": "token-smart",
		},
	}
	StoreCodexQuotaSmartState(auth, codexQuotaSmartState{
		SnapshotAt: time.Now().Add(-20 * time.Minute).UTC().Format(time.RFC3339),
		FiveHour: codexQuotaSmartWindow{
			RemainingFraction: floatPtr(0.8),
			ResetAt:           time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339),
		},
	})
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() { reg.UnregisterClient(auth.ID) })

	if _, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("execute: %v", err)
	}

	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatalf("expected auth after execute")
	}
	state, ok := ReadCodexQuotaSmartState(updated, time.Now().UTC())
	if !ok {
		t.Fatalf("expected codex smart state")
	}
	if len(state.Local5HEvents) != 1 {
		t.Fatalf("local_5h_events = %d, want 1", len(state.Local5HEvents))
	}
	if strings.TrimSpace(state.LastPrewarmAt) == "" {
		t.Fatalf("expected last_prewarm_at to be set")
	}

	waitForRefreshCount(t, executor, 1)
}

func TestManagerExecute_CodexQuotaSmartTriggersRefreshForStalePositiveSnapshot(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &CodexQuotaSmartSelector{}, nil)
	t.Cleanup(manager.Close)
	executor := &codexQuotaSmartTestExecutor{}
	manager.RegisterExecutor(executor)

	now := time.Now().UTC()
	auth := &Auth{
		ID:       "codex-smart-stale-positive-auth",
		Provider: "codex",
		Metadata: map[string]any{
			"account_id":   "acct-smart-stale-positive",
			"access_token": "token-smart-stale-positive",
		},
	}
	StoreCodexQuotaSmartState(auth, codexQuotaSmartState{
		SnapshotAt: now.Add(-2 * codexQuotaSnapshotRefreshInterval).Format(time.RFC3339),
		Weekly: codexQuotaSmartWeeklyWindow{
			Started: true,
			codexQuotaSmartWindow: codexQuotaSmartWindow{
				RemainingFraction: floatPtr(0.4),
				ResetAt:           now.Add(time.Hour).Format(time.RFC3339),
			},
		},
		FiveHour: codexQuotaSmartWindow{
			RemainingFraction: floatPtr(0.8),
			ResetAt:           now.Add(30 * time.Minute).Format(time.RFC3339),
		},
	})
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() { reg.UnregisterClient(auth.ID) })

	if _, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("execute: %v", err)
	}

	waitForRefreshCount(t, executor, 1)
}

func TestManagerExecute_CodexQuotaSmartWrappedBySessionAffinityRecordsLocalUsage(t *testing.T) {
	t.Parallel()

	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: &CodexQuotaSmartSelector{},
		TTL:      time.Minute,
	})
	manager := NewManager(nil, selector, nil)
	t.Cleanup(manager.Close)
	executor := &codexQuotaSmartTestExecutor{}
	manager.RegisterExecutor(executor)

	auth := &Auth{
		ID:       "codex-smart-sticky-auth",
		Provider: "codex",
		Metadata: map[string]any{
			"account_id":   "acct-smart-sticky",
			"access_token": "token-smart-sticky",
		},
	}
	StoreCodexQuotaSmartState(auth, codexQuotaSmartState{
		SnapshotAt: time.Now().Add(-20 * time.Minute).UTC().Format(time.RFC3339),
		FiveHour: codexQuotaSmartWindow{
			RemainingFraction: floatPtr(0.8),
			ResetAt:           time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339),
		},
	})
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() { reg.UnregisterClient(auth.ID) })

	opts := cliproxyexecutor.Options{
		OriginalRequest: []byte(`{"metadata":{"user_id":"user_xxx_account__session_03cf3439-1741-4476-a329-f04f81d8dd3a"}}`),
	}
	if _, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: "test-model"}, opts); err != nil {
		t.Fatalf("execute: %v", err)
	}

	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatalf("expected auth after execute")
	}
	state, ok := ReadCodexQuotaSmartState(updated, time.Now().UTC())
	if !ok {
		t.Fatalf("expected codex smart state")
	}
	if len(state.Local5HEvents) != 1 {
		t.Fatalf("local_5h_events = %d, want 1", len(state.Local5HEvents))
	}
	if strings.TrimSpace(state.LastPrewarmAt) == "" {
		t.Fatalf("expected last_prewarm_at to be set")
	}
}

func TestManagerRecordCodexQuotaSmartSuccess_DeduplicatesPrewarmRefresh(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &CodexQuotaSmartSelector{}, nil)
	t.Cleanup(manager.Close)
	executor := &codexQuotaSmartTestExecutor{}
	manager.RegisterExecutor(executor)

	auth := &Auth{
		ID:       "codex-smart-dedupe-auth",
		Provider: "codex",
		Metadata: map[string]any{
			"account_id":   "acct-smart-dedupe",
			"access_token": "token-smart-dedupe",
		},
	}
	StoreCodexQuotaSmartState(auth, codexQuotaSmartState{
		SnapshotAt: time.Now().Add(-20 * time.Minute).UTC().Format(time.RFC3339),
		FiveHour: codexQuotaSmartWindow{
			RemainingFraction: floatPtr(0.8),
			ResetAt:           time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339),
		},
	})
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manager.recordCodexQuotaSmartSuccess(context.Background(), auth.ID, true)
		}()
	}
	wg.Wait()

	waitForRefreshCount(t, executor, 1)
	if got := executor.RefreshCount(); got != 1 {
		t.Fatalf("refresh count = %d, want 1", got)
	}
}

func waitForRefreshCount(t *testing.T, executor *codexQuotaSmartTestExecutor, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if executor.RefreshCount() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected async refresh count to reach %d, got %d", want, executor.RefreshCount())
}
