package cliproxy

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

type testTokenClientProvider struct{}

func (testTokenClientProvider) Load(context.Context, *config.Config) (*TokenClientResult, error) {
	return &TokenClientResult{}, nil
}

type testAPIKeyClientProvider struct{}

func (testAPIKeyClientProvider) Load(context.Context, *config.Config) (*APIKeyClientResult, error) {
	return &APIKeyClientResult{}, nil
}

func selectorTypeName(v reflect.Value) string {
	for v.IsValid() && v.Kind() == reflect.Interface {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return ""
	}
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return ""
	}
	return v.Type().String()
}

func managerSelectorTypes(t *testing.T, mgr *coreauth.Manager) (string, string) {
	t.Helper()

	if mgr == nil {
		t.Fatal("manager is nil")
	}
	managerValue := reflect.ValueOf(mgr).Elem()
	selectorValue := managerValue.FieldByName("selector")
	selectorType := selectorTypeName(selectorValue)
	if selectorType == "" {
		t.Fatal("selector type is empty")
	}

	concreteSelector := selectorValue
	for concreteSelector.IsValid() && concreteSelector.Kind() == reflect.Interface {
		if concreteSelector.IsNil() {
			break
		}
		concreteSelector = concreteSelector.Elem()
	}
	if !concreteSelector.IsValid() || concreteSelector.Kind() != reflect.Ptr || concreteSelector.IsNil() {
		return selectorType, ""
	}

	fallbackValue := concreteSelector.Elem().FieldByName("fallback")
	return selectorType, selectorTypeName(fallbackValue)
}

func TestServiceApplyCoreAuthAddOrUpdate_DeleteReAddDoesNotInheritStaleRuntimeState(t *testing.T) {
	service := &Service{
		cfg:         &config.Config{},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}

	authID := "service-stale-state-auth"
	modelID := "stale-model"
	lastRefreshedAt := time.Date(2026, time.March, 1, 8, 0, 0, 0, time.UTC)
	nextRefreshAfter := lastRefreshedAt.Add(30 * time.Minute)

	t.Cleanup(func() {
		GlobalModelRegistry().UnregisterClient(authID)
	})

	service.applyCoreAuthAddOrUpdate(context.Background(), &coreauth.Auth{
		ID:               authID,
		Provider:         "claude",
		Status:           coreauth.StatusActive,
		LastRefreshedAt:  lastRefreshedAt,
		NextRefreshAfter: nextRefreshAfter,
		ModelStates: map[string]*coreauth.ModelState{
			modelID: {
				Quota: coreauth.QuotaState{BackoffLevel: 7},
			},
		},
	})

	service.applyCoreAuthRemoval(context.Background(), authID)

	disabled, ok := service.coreManager.GetByID(authID)
	if !ok || disabled == nil {
		t.Fatalf("expected disabled auth after removal")
	}
	if !disabled.Disabled || disabled.Status != coreauth.StatusDisabled {
		t.Fatalf("expected disabled auth after removal, got disabled=%v status=%v", disabled.Disabled, disabled.Status)
	}
	if disabled.LastRefreshedAt.IsZero() {
		t.Fatalf("expected disabled auth to still carry prior LastRefreshedAt for regression setup")
	}
	if disabled.NextRefreshAfter.IsZero() {
		t.Fatalf("expected disabled auth to still carry prior NextRefreshAfter for regression setup")
	}

	// Reconcile prunes unsupported model state during registration, so seed the
	// disabled snapshot explicitly before exercising delete -> re-add behavior.
	disabled.ModelStates = map[string]*coreauth.ModelState{
		modelID: {
			Quota: coreauth.QuotaState{BackoffLevel: 7},
		},
	}
	if _, err := service.coreManager.Update(context.Background(), disabled); err != nil {
		t.Fatalf("seed disabled auth stale ModelStates: %v", err)
	}

	disabled, ok = service.coreManager.GetByID(authID)
	if !ok || disabled == nil {
		t.Fatalf("expected disabled auth after stale state seeding")
	}
	if len(disabled.ModelStates) == 0 {
		t.Fatalf("expected disabled auth to carry seeded ModelStates for regression setup")
	}

	service.applyCoreAuthAddOrUpdate(context.Background(), &coreauth.Auth{
		ID:       authID,
		Provider: "claude",
		Status:   coreauth.StatusActive,
	})

	updated, ok := service.coreManager.GetByID(authID)
	if !ok || updated == nil {
		t.Fatalf("expected re-added auth to be present")
	}
	if updated.Disabled {
		t.Fatalf("expected re-added auth to be active")
	}
	if !updated.LastRefreshedAt.IsZero() {
		t.Fatalf("expected LastRefreshedAt to reset on delete -> re-add, got %v", updated.LastRefreshedAt)
	}
	if !updated.NextRefreshAfter.IsZero() {
		t.Fatalf("expected NextRefreshAfter to reset on delete -> re-add, got %v", updated.NextRefreshAfter)
	}
	if len(updated.ModelStates) != 0 {
		t.Fatalf("expected ModelStates to reset on delete -> re-add, got %d entries", len(updated.ModelStates))
	}
	if models := registry.GetGlobalRegistry().GetModelsForClient(authID); len(models) == 0 {
		t.Fatalf("expected re-added auth to re-register models in global registry")
	}
}

func TestServiceRunAndReloadKeepSessionAffinitySelector(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Host:    "127.0.0.1",
		Port:    0,
		AuthDir: filepath.Join(tmpDir, "auth"),
		Routing: internalconfig.RoutingConfig{
			Strategy:           "quota-sticky",
			SessionAffinity:    true,
			SessionAffinityTTL: "90m",
		},
	}

	var reload func(*config.Config)
	started := make(chan struct{})

	service, err := NewBuilder().
		WithConfig(cfg).
		WithConfigPath(filepath.Join(tmpDir, "config.yaml")).
		WithTokenClientProvider(testTokenClientProvider{}).
		WithAPIKeyClientProvider(testAPIKeyClientProvider{}).
		WithWatcherFactory(func(configPath, authDir string, fn func(*config.Config)) (*WatcherWrapper, error) {
			reload = fn
			return &WatcherWrapper{
				start: func(context.Context) error { return nil },
				stop:  func() error { return nil },
			}, nil
		}).
		WithHooks(Hooks{
			OnAfterStart: func(*Service) {
				select {
				case <-started:
				default:
					close(started)
				}
			},
		}).
		Build()
	if err != nil {
		t.Fatalf("build service: %v", err)
	}

	selectorType, fallbackType := managerSelectorTypes(t, service.coreManager)
	if selectorType != "*auth.SessionAffinitySelector" {
		t.Fatalf("selector before run = %q, want %q", selectorType, "*auth.SessionAffinitySelector")
	}
	if fallbackType != "*auth.QuotaStickySelector" {
		t.Fatalf("fallback before run = %q, want %q", fallbackType, "*auth.QuotaStickySelector")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- service.Run(ctx)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for service start")
	}

	selectorType, fallbackType = managerSelectorTypes(t, service.coreManager)
	if selectorType != "*auth.SessionAffinitySelector" {
		t.Fatalf("selector after run = %q, want %q", selectorType, "*auth.SessionAffinitySelector")
	}
	if fallbackType != "*auth.QuotaStickySelector" {
		t.Fatalf("fallback after run = %q, want %q", fallbackType, "*auth.QuotaStickySelector")
	}

	if reload == nil {
		t.Fatal("reload callback was not captured")
	}

	reloadedCfg := *cfg
	reloadedCfg.Routing = internalconfig.RoutingConfig{
		Strategy:           "quota-smart",
		SessionAffinity:    true,
		SessionAffinityTTL: "30m",
	}
	reload(&reloadedCfg)

	selectorType, fallbackType = managerSelectorTypes(t, service.coreManager)
	if selectorType != "*auth.SessionAffinitySelector" {
		t.Fatalf("selector after reload = %q, want %q", selectorType, "*auth.SessionAffinitySelector")
	}
	if fallbackType != "*auth.CodexQuotaSmartSelector" {
		t.Fatalf("fallback after reload = %q, want %q", fallbackType, "*auth.CodexQuotaSmartSelector")
	}

	cancel()
	if err := <-runErrCh; err != nil && err != context.Canceled {
		t.Fatalf("run returned error: %v", err)
	}
}
