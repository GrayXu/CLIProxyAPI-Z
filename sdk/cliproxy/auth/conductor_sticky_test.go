package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type stickyTrackingExecutor struct {
	id string

	mu      sync.Mutex
	authIDs []string
}

func (e *stickyTrackingExecutor) Identifier() string { return e.id }

func (e *stickyTrackingExecutor) Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = req
	_ = opts
	e.mu.Lock()
	e.authIDs = append(e.authIDs, auth.ID)
	e.mu.Unlock()
	return cliproxyexecutor.Response{Payload: []byte(auth.ID)}, nil
}

func (e *stickyTrackingExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	_ = ctx
	_ = auth
	_ = req
	_ = opts
	ch := make(chan cliproxyexecutor.StreamChunk, 1)
	close(ch)
	return &cliproxyexecutor.StreamResult{Chunks: ch}, nil
}

func (e *stickyTrackingExecutor) Refresh(ctx context.Context, auth *Auth) (*Auth, error) {
	_ = ctx
	return auth, nil
}

func (e *stickyTrackingExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	_ = opts
	return cliproxyexecutor.Response{}, nil
}

func (e *stickyTrackingExecutor) HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error) {
	_ = ctx
	_ = auth
	_ = req
	return nil, nil
}

func (e *stickyTrackingExecutor) AuthIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.authIDs))
	copy(out, e.authIDs)
	return out
}

func newQuotaStickyTestManager(t *testing.T, auths ...*Auth) (*Manager, *stickyTrackingExecutor) {
	t.Helper()
	executor := &stickyTrackingExecutor{id: "gemini"}
	manager := NewManager(nil, &QuotaStickySelector{}, nil)
	t.Cleanup(manager.Close)
	manager.RegisterExecutor(executor)
	for _, auth := range auths {
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register auth %s: %v", auth.ID, err)
		}
	}
	reg := registry.GetGlobalRegistry()
	for _, auth := range auths {
		reg.RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	}
	t.Cleanup(func() {
		for _, auth := range auths {
			reg.UnregisterClient(auth.ID)
		}
	})
	return manager, executor
}

func TestManagerExecute_QuotaStickyBindsConversationToFirstSelectedAuth(t *testing.T) {
	t.Parallel()

	authAID := "auth-a-" + t.Name()
	authBID := "auth-b-" + t.Name()
	authA := &Auth{
		ID:       authAID,
		Provider: "gemini",
		Status:   StatusActive,
		Metadata: map[string]any{routingQuotaScoreMetadataKey: 10},
	}
	authB := &Auth{
		ID:       authBID,
		Provider: "gemini",
		Status:   StatusActive,
		Metadata: map[string]any{routingQuotaScoreMetadataKey: 20},
	}
	manager, executor := newQuotaStickyTestManager(t, authA, authB)

	opts := cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.StickyConversationMetadataKey: "conv-1",
		},
	}
	if _, err := manager.Execute(context.Background(), []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, opts); err != nil {
		t.Fatalf("first execute: %v", err)
	}

	updatedA, _ := manager.GetByID(authAID)
	updatedA.Metadata[routingQuotaScoreMetadataKey] = 100
	if _, err := manager.Update(context.Background(), updatedA); err != nil {
		t.Fatalf("update auth-a: %v", err)
	}
	updatedB, _ := manager.GetByID(authBID)
	updatedB.Metadata[routingQuotaScoreMetadataKey] = 0
	if _, err := manager.Update(context.Background(), updatedB); err != nil {
		t.Fatalf("update auth-b: %v", err)
	}

	if _, err := manager.Execute(context.Background(), []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, opts); err != nil {
		t.Fatalf("second execute: %v", err)
	}

	got := executor.AuthIDs()
	want := []string{authBID, authBID}
	if len(got) != len(want) {
		t.Fatalf("auth IDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("auth IDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestManagerExecute_QuotaStickyRebindsWhenMappedAuthBecomesUnavailable(t *testing.T) {
	t.Parallel()

	authAID := "auth-a-" + t.Name()
	authBID := "auth-b-" + t.Name()
	authA := &Auth{
		ID:       authAID,
		Provider: "gemini",
		Status:   StatusActive,
		Metadata: map[string]any{routingQuotaScoreMetadataKey: 10},
	}
	authB := &Auth{
		ID:       authBID,
		Provider: "gemini",
		Status:   StatusActive,
		Metadata: map[string]any{routingQuotaScoreMetadataKey: 20},
	}
	manager, executor := newQuotaStickyTestManager(t, authA, authB)

	opts := cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.StickyConversationMetadataKey: "conv-2",
		},
	}
	if _, err := manager.Execute(context.Background(), []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, opts); err != nil {
		t.Fatalf("first execute: %v", err)
	}

	updatedB, _ := manager.GetByID(authBID)
	updatedB.ModelStates = map[string]*ModelState{
		"test-model": {
			Status:         StatusError,
			Unavailable:    true,
			NextRetryAfter: time.Now().Add(time.Minute),
			Quota: QuotaState{
				Exceeded:      true,
				Reason:        "quota",
				NextRecoverAt: time.Now().Add(time.Minute),
			},
		},
	}
	if _, err := manager.Update(context.Background(), updatedB); err != nil {
		t.Fatalf("update auth-b: %v", err)
	}

	if _, err := manager.Execute(context.Background(), []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, opts); err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if _, err := manager.Execute(context.Background(), []string{"gemini"}, cliproxyexecutor.Request{Model: "test-model"}, opts); err != nil {
		t.Fatalf("third execute: %v", err)
	}

	got := executor.AuthIDs()
	want := []string{authBID, authAID, authAID}
	if len(got) != len(want) {
		t.Fatalf("auth IDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("auth IDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
