package auth

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type schedulerProviderTestExecutor struct {
	provider string
}

func (e schedulerProviderTestExecutor) Identifier() string { return e.provider }

func (e schedulerProviderTestExecutor) Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (e schedulerProviderTestExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (e schedulerProviderTestExecutor) Refresh(ctx context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e schedulerProviderTestExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (e schedulerProviderTestExecutor) HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error) {
	return nil, nil
}

type priorityBoostTestExecutor struct {
	provider      string
	mu            sync.Mutex
	refreshScores map[string]float64
	executeAuths  []string
	countAuths    []string
	streamAuths   []string
	failExec      map[string]int
}

func (e *priorityBoostTestExecutor) Identifier() string { return e.provider }

func (e *priorityBoostTestExecutor) Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = req
	_ = opts
	e.mu.Lock()
	defer e.mu.Unlock()
	e.executeAuths = append(e.executeAuths, auth.ID)
	if remaining := e.failExec[auth.ID]; remaining > 0 {
		e.failExec[auth.ID] = remaining - 1
		return cliproxyexecutor.Response{}, errors.New("execute failed")
	}
	return cliproxyexecutor.Response{Payload: []byte(auth.ID)}, nil
}

func (e *priorityBoostTestExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	_ = ctx
	_ = req
	_ = opts
	e.mu.Lock()
	e.streamAuths = append(e.streamAuths, auth.ID)
	e.mu.Unlock()
	ch := make(chan cliproxyexecutor.StreamChunk, 1)
	ch <- cliproxyexecutor.StreamChunk{Payload: []byte("chunk")}
	close(ch)
	return &cliproxyexecutor.StreamResult{Chunks: ch}, nil
}

func (e *priorityBoostTestExecutor) Refresh(ctx context.Context, auth *Auth) (*Auth, error) {
	_ = ctx
	updated := auth.Clone()
	if updated.Metadata == nil {
		updated.Metadata = make(map[string]any)
	}
	e.mu.Lock()
	score, ok := e.refreshScores[auth.ID]
	e.mu.Unlock()
	if ok {
		updated.Metadata[routingQuotaScoreMetadataKey] = score
	} else {
		delete(updated.Metadata, routingQuotaScoreMetadataKey)
	}
	return updated, nil
}

func (e *priorityBoostTestExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = req
	_ = opts
	e.mu.Lock()
	defer e.mu.Unlock()
	e.countAuths = append(e.countAuths, auth.ID)
	return cliproxyexecutor.Response{Payload: []byte(auth.ID)}, nil
}

func (e *priorityBoostTestExecutor) HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	return nil, nil
}

func (e *priorityBoostTestExecutor) ExecuteIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.executeAuths))
	copy(out, e.executeAuths)
	return out
}

func (e *priorityBoostTestExecutor) CountIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.countAuths))
	copy(out, e.countAuths)
	return out
}

func (e *priorityBoostTestExecutor) StreamIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.streamAuths))
	copy(out, e.streamAuths)
	return out
}

func TestManager_RefreshSchedulerEntry_RebuildsSupportedModelSetAfterModelRegistration(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name  string
		prime func(*Manager, *Auth) error
	}{
		{
			name: "register",
			prime: func(manager *Manager, auth *Auth) error {
				_, errRegister := manager.Register(ctx, auth)
				return errRegister
			},
		},
		{
			name: "update",
			prime: func(manager *Manager, auth *Auth) error {
				_, errRegister := manager.Register(ctx, auth)
				if errRegister != nil {
					return errRegister
				}
				updated := auth.Clone()
				updated.Metadata = map[string]any{"updated": true}
				_, errUpdate := manager.Update(ctx, updated)
				return errUpdate
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			manager := NewManager(nil, &RoundRobinSelector{}, nil)
			auth := &Auth{
				ID:       "refresh-entry-" + testCase.name,
				Provider: "gemini",
			}
			if errPrime := testCase.prime(manager, auth); errPrime != nil {
				t.Fatalf("prime auth %s: %v", testCase.name, errPrime)
			}

			registerSchedulerModels(t, "gemini", "scheduler-refresh-model", auth.ID)

			got, errPick := manager.scheduler.pickSingle(ctx, "gemini", "scheduler-refresh-model", cliproxyexecutor.Options{}, nil)
			var authErr *Error
			if !errors.As(errPick, &authErr) || authErr == nil {
				t.Fatalf("pickSingle() before refresh error = %v, want auth_not_found", errPick)
			}
			if authErr.Code != "auth_not_found" {
				t.Fatalf("pickSingle() before refresh code = %q, want %q", authErr.Code, "auth_not_found")
			}
			if got != nil {
				t.Fatalf("pickSingle() before refresh auth = %v, want nil", got)
			}

			manager.RefreshSchedulerEntry(auth.ID)

			got, errPick = manager.scheduler.pickSingle(ctx, "gemini", "scheduler-refresh-model", cliproxyexecutor.Options{}, nil)
			if errPick != nil {
				t.Fatalf("pickSingle() after refresh error = %v", errPick)
			}
			if got == nil || got.ID != auth.ID {
				t.Fatalf("pickSingle() after refresh auth = %v, want %q", got, auth.ID)
			}
		})
	}
}

func TestManager_PickNext_RebuildsSchedulerAfterModelCooldownError(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "gemini"})

	registerSchedulerModels(t, "gemini", "scheduler-cooldown-rebuild-model", "cooldown-stale-old")

	oldAuth := &Auth{
		ID:       "cooldown-stale-old",
		Provider: "gemini",
	}
	if _, errRegister := manager.Register(ctx, oldAuth); errRegister != nil {
		t.Fatalf("register old auth: %v", errRegister)
	}

	manager.MarkResult(ctx, Result{
		AuthID:   oldAuth.ID,
		Provider: "gemini",
		Model:    "scheduler-cooldown-rebuild-model",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "quota"},
	})

	newAuth := &Auth{
		ID:       "cooldown-stale-new",
		Provider: "gemini",
	}
	if _, errRegister := manager.Register(ctx, newAuth); errRegister != nil {
		t.Fatalf("register new auth: %v", errRegister)
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(newAuth.ID, "gemini", []*registry.ModelInfo{{ID: "scheduler-cooldown-rebuild-model"}})
	t.Cleanup(func() {
		reg.UnregisterClient(newAuth.ID)
	})

	got, errPick := manager.scheduler.pickSingle(ctx, "gemini", "scheduler-cooldown-rebuild-model", cliproxyexecutor.Options{}, nil)
	var cooldownErr *modelCooldownError
	if !errors.As(errPick, &cooldownErr) {
		t.Fatalf("pickSingle() before sync error = %v, want modelCooldownError", errPick)
	}
	if got != nil {
		t.Fatalf("pickSingle() before sync auth = %v, want nil", got)
	}

	got, executor, errPick := manager.pickNext(ctx, "gemini", "scheduler-cooldown-rebuild-model", cliproxyexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("pickNext() error = %v", errPick)
	}
	if executor == nil {
		t.Fatal("pickNext() executor = nil")
	}
	if got == nil || got.ID != newAuth.ID {
		t.Fatalf("pickNext() auth = %v, want %q", got, newAuth.ID)
	}
}

func TestManager_RefreshPriorityBoost_AppliesOnceAndResetsAfterExecuteSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	executor := &priorityBoostTestExecutor{
		provider:      "gemini",
		refreshScores: map[string]float64{"boosted": 100},
		failExec:      make(map[string]int),
	}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(executor)

	auths := []*Auth{
		{ID: "boosted", Provider: "gemini", Attributes: map[string]string{"priority": "0"}},
		{ID: "high", Provider: "gemini", Attributes: map[string]string{"priority": "10"}},
	}
	for _, auth := range auths {
		if _, err := manager.Register(ctx, auth); err != nil {
			t.Fatalf("register %s: %v", auth.ID, err)
		}
	}
	registerSchedulerModels(t, "gemini", "test-model", "boosted", "high")
	manager.RefreshSchedulerEntry("high")

	manager.refreshAuth(ctx, "boosted")

	if _, err := manager.Execute(ctx, []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if _, err := manager.Execute(ctx, []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("second execute: %v", err)
	}

	got := executor.ExecuteIDs()
	want := []string{"boosted", "high"}
	if len(got) != len(want) {
		t.Fatalf("execute IDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("execute IDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestManager_RefreshPriorityBoost_PersistsAfterFailureUntilSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	executor := &priorityBoostTestExecutor{
		provider:      "gemini",
		refreshScores: map[string]float64{"boosted": 100},
		failExec:      map[string]int{"boosted": 1},
	}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(executor)

	auths := []*Auth{
		{ID: "boosted", Provider: "gemini", Attributes: map[string]string{"priority": "0"}},
		{ID: "high", Provider: "gemini", Attributes: map[string]string{"priority": "10"}},
	}
	for _, auth := range auths {
		if _, err := manager.Register(ctx, auth); err != nil {
			t.Fatalf("register %s: %v", auth.ID, err)
		}
	}
	registerSchedulerModels(t, "gemini", "test-model", "boosted", "high")
	manager.RefreshSchedulerEntry("high")

	manager.refreshAuth(ctx, "boosted")

	if _, err := manager.Execute(ctx, []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if _, err := manager.Execute(ctx, []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("second execute: %v", err)
	}

	got := executor.ExecuteIDs()
	want := []string{"boosted", "high", "boosted"}
	if len(got) != len(want) {
		t.Fatalf("execute IDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("execute IDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestManager_RefreshPriorityBoost_CountSuccessConsumesBoost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	executor := &priorityBoostTestExecutor{
		provider:      "gemini",
		refreshScores: map[string]float64{"boosted": 100},
		failExec:      make(map[string]int),
	}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(executor)

	auths := []*Auth{
		{ID: "boosted", Provider: "gemini", Attributes: map[string]string{"priority": "0"}},
		{ID: "high", Provider: "gemini", Attributes: map[string]string{"priority": "10"}},
	}
	for _, auth := range auths {
		if _, err := manager.Register(ctx, auth); err != nil {
			t.Fatalf("register %s: %v", auth.ID, err)
		}
	}
	registerSchedulerModels(t, "gemini", "test-model", "boosted", "high")
	manager.RefreshSchedulerEntry("high")

	manager.refreshAuth(ctx, "boosted")

	if _, err := manager.ExecuteCount(ctx, []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("execute count: %v", err)
	}
	if _, err := manager.Execute(ctx, []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("execute: %v", err)
	}

	countIDs := executor.CountIDs()
	if len(countIDs) != 1 || countIDs[0] != "boosted" {
		t.Fatalf("count IDs = %v, want [boosted]", countIDs)
	}
	execIDs := executor.ExecuteIDs()
	if len(execIDs) != 1 || execIDs[0] != "high" {
		t.Fatalf("execute IDs = %v, want [high]", execIDs)
	}
}

func TestManager_RefreshPriorityBoost_StreamBootstrapConsumesBoost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	executor := &priorityBoostTestExecutor{
		provider:      "gemini",
		refreshScores: map[string]float64{"boosted": 100},
		failExec:      make(map[string]int),
	}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(executor)

	auths := []*Auth{
		{ID: "boosted", Provider: "gemini", Attributes: map[string]string{"priority": "0"}},
		{ID: "high", Provider: "gemini", Attributes: map[string]string{"priority": "10"}},
	}
	for _, auth := range auths {
		if _, err := manager.Register(ctx, auth); err != nil {
			t.Fatalf("register %s: %v", auth.ID, err)
		}
	}
	registerSchedulerModels(t, "gemini", "test-model", "boosted", "high")
	manager.RefreshSchedulerEntry("high")

	manager.refreshAuth(ctx, "boosted")

	streamResult, err := manager.ExecuteStream(ctx, []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("execute stream: %v", err)
	}
	for range streamResult.Chunks {
	}
	if _, err := manager.Execute(ctx, []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("execute: %v", err)
	}

	streamIDs := executor.StreamIDs()
	if len(streamIDs) != 1 || streamIDs[0] != "boosted" {
		t.Fatalf("stream IDs = %v, want [boosted]", streamIDs)
	}
	execIDs := executor.ExecuteIDs()
	if len(execIDs) != 1 || execIDs[0] != "high" {
		t.Fatalf("execute IDs = %v, want [high]", execIDs)
	}
}

func TestManager_RefreshPriorityBoost_NonFullRefreshClearsPendingBoost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	executor := &priorityBoostTestExecutor{
		provider:      "gemini",
		refreshScores: map[string]float64{"boosted": 100},
		failExec:      make(map[string]int),
	}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(executor)

	auths := []*Auth{
		{ID: "boosted", Provider: "gemini", Attributes: map[string]string{"priority": "0"}},
		{ID: "high", Provider: "gemini", Attributes: map[string]string{"priority": "10"}},
	}
	for _, auth := range auths {
		if _, err := manager.Register(ctx, auth); err != nil {
			t.Fatalf("register %s: %v", auth.ID, err)
		}
	}
	registerSchedulerModels(t, "gemini", "test-model", "boosted", "high")
	manager.RefreshSchedulerEntry("high")

	manager.refreshAuth(ctx, "boosted")
	executor.mu.Lock()
	executor.refreshScores["boosted"] = 50
	executor.mu.Unlock()
	manager.refreshAuth(ctx, "boosted")

	if _, err := manager.Execute(ctx, []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := executor.ExecuteIDs()
	if len(got) != 1 || got[0] != "high" {
		t.Fatalf("execute IDs = %v, want [high]", got)
	}
}
