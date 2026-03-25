package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func setupViewerTestRouter(t *testing.T, cfg *config.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	h := NewHandler(cfg, "", nil)
	r := gin.New()
	mgmt := r.Group("/v0/management")
	mgmt.Use(h.Middleware())
	mgmt.GET("/session", h.GetSession)
	mgmt.GET("/config", h.GetConfig)
	admin := mgmt.Group("")
	admin.Use(h.RequireAdmin())
	admin.GET("/api-keys", h.GetAPIKeys)
	return r
}

func TestViewerKeyAccessWithoutManagementSecret(t *testing.T) {
	t.Parallel()

	router := setupViewerTestRouter(t, &config.Config{
		SDKConfig:              sdkconfig.SDKConfig{RequestLog: true, APIKeys: []string{"sk-viewer-123", "plain-non-viewer"}},
		RemoteManagement:       config.RemoteManagement{AllowRemote: true},
		LoggingToFile:          true,
		UsageStatisticsEnabled: true,
		Routing:                config.RoutingConfig{Strategy: "quota-sticky"},
	})

	req := httptest.NewRequest(http.MethodGet, "/v0/management/session", nil)
	req.Header.Set("Authorization", "Bearer sk-viewer-123")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("session status = %d, want %d", resp.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	if got, _ := payload["role"].(string); got != "viewer" {
		t.Fatalf("session role = %q, want viewer", got)
	}
}

func TestViewerConfigIsRedacted(t *testing.T) {
	t.Parallel()

	router := setupViewerTestRouter(t, &config.Config{
		SDKConfig:              sdkconfig.SDKConfig{RequestLog: true, APIKeys: []string{"sk-viewer-123", "sk-hidden-admin-data"}},
		RemoteManagement:       config.RemoteManagement{AllowRemote: true},
		LoggingToFile:          true,
		UsageStatisticsEnabled: true,
		Routing:                config.RoutingConfig{Strategy: "quota-sticky"},
		GeminiKey:              []config.GeminiKey{{APIKey: "secret-gemini"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
	req.Header.Set("Authorization", "Bearer sk-viewer-123")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("config status = %d, want %d", resp.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if _, ok := payload["api-keys"]; ok {
		t.Fatal("viewer config unexpectedly exposed api-keys")
	}
	if _, ok := payload["gemini-api-key"]; ok {
		t.Fatal("viewer config unexpectedly exposed gemini-api-key")
	}
	if got, _ := payload["logging-to-file"].(bool); !got {
		t.Fatal("viewer config missing logging-to-file")
	}
}

func TestViewerCannotAccessAdminOnlyRoute(t *testing.T) {
	t.Parallel()

	router := setupViewerTestRouter(t, &config.Config{
		SDKConfig:        sdkconfig.SDKConfig{APIKeys: []string{"sk-viewer-123"}},
		RemoteManagement: config.RemoteManagement{AllowRemote: true},
	})

	req := httptest.NewRequest(http.MethodGet, "/v0/management/api-keys", nil)
	req.Header.Set("Authorization", "Bearer sk-viewer-123")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("api-keys status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}
