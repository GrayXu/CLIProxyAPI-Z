package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func setupIssueAPIKeyTestRouter(t *testing.T, rawConfig string) (*gin.Engine, *Handler, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(rawConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	h := NewHandler(cfg, configPath, nil)
	r := gin.New()
	mgmt := r.Group("/v0/management")
	mgmt.POST("/public/issue-api-key", h.IssueAPIKey)
	protected := mgmt.Group("")
	protected.Use(h.Middleware())
	protected.GET("/session", h.GetSession)
	admin := protected.Group("")
	admin.Use(h.RequireAdmin())
	admin.GET("/api-keys", h.GetAPIKeys)
	return r, h, configPath
}

func TestIssueAPIKeySuccessAndViewerAccess(t *testing.T) {
	t.Parallel()

	router, handler, configPath := setupIssueAPIKeyTestRouter(t, `
remote-management:
  allow-remote: true
  issue-api-key-password: issue-me
api-keys: []
`)

	var callbackCfg *config.Config
	handler.SetConfigUpdatedHook(func(cfg *config.Config) {
		callbackCfg = cfg
	})

	body := bytes.NewBufferString(`{"password":"issue-me"}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/public/issue-api-key", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("issue status = %d, want %d, body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}

	var payload issueAPIKeyResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(payload.APIKey, issuedAPIKeyPrefix) {
		t.Fatalf("issued api key = %q, want prefix %q", payload.APIKey, issuedAPIKeyPrefix)
	}
	if payload.Role != "viewer" {
		t.Fatalf("issued role = %q, want viewer", payload.Role)
	}
	if callbackCfg == nil || !containsStringExact(callbackCfg.APIKeys, payload.APIKey) {
		t.Fatalf("config update hook did not receive issued api key")
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/v0/management/session", nil)
	sessionReq.Header.Set("Authorization", "Bearer "+payload.APIKey)
	sessionResp := httptest.NewRecorder()
	router.ServeHTTP(sessionResp, sessionReq)
	if sessionResp.Code != http.StatusOK {
		t.Fatalf("session status = %d, want %d, body=%s", sessionResp.Code, http.StatusOK, sessionResp.Body.String())
	}

	var sessionPayload map[string]any
	if err := json.Unmarshal(sessionResp.Body.Bytes(), &sessionPayload); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	if got, _ := sessionPayload["role"].(string); got != "viewer" {
		t.Fatalf("session role = %q, want viewer", got)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/v0/management/api-keys", nil)
	adminReq.Header.Set("Authorization", "Bearer "+payload.APIKey)
	adminResp := httptest.NewRecorder()
	router.ServeHTTP(adminResp, adminReq)
	if adminResp.Code != http.StatusForbidden {
		t.Fatalf("admin route status = %d, want %d", adminResp.Code, http.StatusForbidden)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), payload.APIKey) {
		t.Fatalf("config file missing issued api key: %s", string(raw))
	}
}

func TestIssueAPIKeyRemoteDisabled(t *testing.T) {
	t.Parallel()

	router, _, _ := setupIssueAPIKeyTestRouter(t, `
remote-management:
  allow-remote: false
  issue-api-key-password: issue-me
api-keys: []
`)

	req := httptest.NewRequest(http.MethodPost, "/v0/management/public/issue-api-key", bytes.NewBufferString(`{"password":"issue-me"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.10:12345"
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusForbidden, resp.Body.String())
	}
}

func TestIssueAPIKeyDisabledWithoutPassword(t *testing.T) {
	t.Parallel()

	router, _, _ := setupIssueAPIKeyTestRouter(t, `
remote-management:
  allow-remote: true
api-keys: []
`)

	req := httptest.NewRequest(http.MethodPost, "/v0/management/public/issue-api-key", bytes.NewBufferString(`{"password":"issue-me"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusForbidden, resp.Body.String())
	}
}

func TestIssueAPIKeyBansAfterThreeFailures(t *testing.T) {
	t.Parallel()

	router, _, _ := setupIssueAPIKeyTestRouter(t, `
remote-management:
  allow-remote: true
  issue-api-key-password: issue-me
api-keys: []
`)

	for i := 0; i < managementMaxFailures; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v0/management/public/issue-api-key", bytes.NewBufferString(`{"password":"wrong-pass"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.10:12345"
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want %d, body=%s", i+1, resp.Code, http.StatusUnauthorized, resp.Body.String())
		}
	}

	blockedReq := httptest.NewRequest(http.MethodPost, "/v0/management/public/issue-api-key", bytes.NewBufferString(`{"password":"issue-me"}`))
	blockedReq.Header.Set("Content-Type", "application/json")
	blockedReq.RemoteAddr = "198.51.100.10:12345"
	blockedResp := httptest.NewRecorder()
	router.ServeHTTP(blockedResp, blockedReq)

	if blockedResp.Code != http.StatusForbidden {
		t.Fatalf("blocked status = %d, want %d, body=%s", blockedResp.Code, http.StatusForbidden, blockedResp.Body.String())
	}
	if !strings.Contains(blockedResp.Body.String(), "too_many_attempts") {
		t.Fatalf("blocked response missing too_many_attempts marker: %s", blockedResp.Body.String())
	}
}
