package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gin "github.com/gin-gonic/gin"
	proxyconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	internallogging "github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()

	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: sdkconfig.SDKConfig{
			APIKeys: []string{"test-key"},
		},
		Port:                   0,
		AuthDir:                authDir,
		Debug:                  true,
		LoggingToFile:          false,
		UsageStatisticsEnabled: false,
	}

	authManager := auth.NewManager(nil, nil, nil)
	accessManager := sdkaccess.NewManager()

	configPath := filepath.Join(tmpDir, "config.yaml")
	return NewServer(cfg, authManager, accessManager, configPath)
}

func newManagementTestServer(t *testing.T, cfg *proxyconfig.Config, opts ...ServerOption) *Server {
	t.Helper()

	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	if cfg == nil {
		cfg = &proxyconfig.Config{}
	}
	cfg.AuthDir = authDir
	cfg.Port = 0
	cfg.Debug = true
	cfg.LoggingToFile = false
	cfg.UsageStatisticsEnabled = false

	authManager := auth.NewManager(nil, nil, nil)
	accessManager := sdkaccess.NewManager()

	configPath := filepath.Join(tmpDir, "config.yaml")
	return NewServer(cfg, authManager, accessManager, configPath, opts...)
}

func TestHealthz(t *testing.T) {
	server := newTestServer(t)

	t.Run("GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}

		var resp struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response JSON: %v; body=%s", err, rr.Body.String())
		}
		if resp.Status != "ok" {
			t.Fatalf("unexpected response status: got %q want %q", resp.Status, "ok")
		}
	})

	t.Run("HEAD", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/healthz", nil)
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("unexpected status code: got %d want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}
		if rr.Body.Len() != 0 {
			t.Fatalf("expected empty body for HEAD request, got %q", rr.Body.String())
		}
	})
}

func TestAmpProviderModelRoutes(t *testing.T) {
	testCases := []struct {
		name         string
		path         string
		wantStatus   int
		wantContains string
	}{
		{
			name:         "openai root models",
			path:         "/api/provider/openai/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "groq root models",
			path:         "/api/provider/groq/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "openai models",
			path:         "/api/provider/openai/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "anthropic models",
			path:         "/api/provider/anthropic/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"data"`,
		},
		{
			name:         "google models v1",
			path:         "/api/provider/google/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"models"`,
		},
		{
			name:         "google models v1beta",
			path:         "/api/provider/google/v1beta/models",
			wantStatus:   http.StatusOK,
			wantContains: `"models"`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			server := newTestServer(t)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer test-key")

			rr := httptest.NewRecorder()
			server.engine.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("unexpected status code for %s: got %d want %d; body=%s", tc.path, rr.Code, tc.wantStatus, rr.Body.String())
			}
			if body := rr.Body.String(); !strings.Contains(body, tc.wantContains) {
				t.Fatalf("response body for %s missing %q: %s", tc.path, tc.wantContains, body)
			}
		})
	}
}

func TestDefaultRequestLoggerFactory_UsesResolvedLogDirectory(t *testing.T) {
	t.Setenv("WRITABLE_PATH", "")
	t.Setenv("writable_path", "")

	originalWD, errGetwd := os.Getwd()
	if errGetwd != nil {
		t.Fatalf("failed to get current working directory: %v", errGetwd)
	}

	tmpDir := t.TempDir()
	if errChdir := os.Chdir(tmpDir); errChdir != nil {
		t.Fatalf("failed to switch working directory: %v", errChdir)
	}
	defer func() {
		if errChdirBack := os.Chdir(originalWD); errChdirBack != nil {
			t.Fatalf("failed to restore working directory: %v", errChdirBack)
		}
	}()

	// Force ResolveLogDirectory to fallback to auth-dir/logs by making ./logs not a writable directory.
	if errWriteFile := os.WriteFile(filepath.Join(tmpDir, "logs"), []byte("not-a-directory"), 0o644); errWriteFile != nil {
		t.Fatalf("failed to create blocking logs file: %v", errWriteFile)
	}

	configDir := filepath.Join(tmpDir, "config")
	if errMkdirConfig := os.MkdirAll(configDir, 0o755); errMkdirConfig != nil {
		t.Fatalf("failed to create config dir: %v", errMkdirConfig)
	}
	configPath := filepath.Join(configDir, "config.yaml")

	authDir := filepath.Join(tmpDir, "auth")
	if errMkdirAuth := os.MkdirAll(authDir, 0o700); errMkdirAuth != nil {
		t.Fatalf("failed to create auth dir: %v", errMkdirAuth)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: proxyconfig.SDKConfig{
			RequestLog: false,
		},
		AuthDir:           authDir,
		ErrorLogsMaxFiles: 10,
	}

	logger := defaultRequestLoggerFactory(cfg, configPath)
	fileLogger, ok := logger.(*internallogging.FileRequestLogger)
	if !ok {
		t.Fatalf("expected *FileRequestLogger, got %T", logger)
	}

	errLog := fileLogger.LogRequestWithOptions(
		"/v1/chat/completions",
		http.MethodPost,
		map[string][]string{"Content-Type": []string{"application/json"}},
		[]byte(`{"input":"hello"}`),
		http.StatusBadGateway,
		map[string][]string{"Content-Type": []string{"application/json"}},
		[]byte(`{"error":"upstream failure"}`),
		nil,
		nil,
		nil,
		nil,
		nil,
		true,
		"issue-1711",
		time.Now(),
		time.Now(),
	)
	if errLog != nil {
		t.Fatalf("failed to write forced error request log: %v", errLog)
	}

	authLogsDir := filepath.Join(authDir, "logs")
	authEntries, errReadAuthDir := os.ReadDir(authLogsDir)
	if errReadAuthDir != nil {
		t.Fatalf("failed to read auth logs dir %s: %v", authLogsDir, errReadAuthDir)
	}
	foundErrorLogInAuthDir := false
	for _, entry := range authEntries {
		if strings.HasPrefix(entry.Name(), "error-") && strings.HasSuffix(entry.Name(), ".log") {
			foundErrorLogInAuthDir = true
			break
		}
	}
	if !foundErrorLogInAuthDir {
		t.Fatalf("expected forced error log in auth fallback dir %s, got entries: %+v", authLogsDir, authEntries)
	}

	configLogsDir := filepath.Join(configDir, "logs")
	configEntries, errReadConfigDir := os.ReadDir(configLogsDir)
	if errReadConfigDir != nil && !os.IsNotExist(errReadConfigDir) {
		t.Fatalf("failed to inspect config logs dir %s: %v", configLogsDir, errReadConfigDir)
	}
	for _, entry := range configEntries {
		if strings.HasPrefix(entry.Name(), "error-") && strings.HasSuffix(entry.Name(), ".log") {
			t.Fatalf("unexpected forced error log in config dir %s", configLogsDir)
		}
	}
}

func TestNewServerTrustsOnlyLocalReverseProxy(t *testing.T) {
	server := newTestServer(t)
	server.engine.GET("/debug/client-ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	remoteReq := httptest.NewRequest(http.MethodGet, "/debug/client-ip", nil)
	remoteReq.RemoteAddr = "198.51.100.10:12345"
	remoteReq.Header.Set("X-Forwarded-For", "203.0.113.20")
	remoteResp := httptest.NewRecorder()
	server.engine.ServeHTTP(remoteResp, remoteReq)
	if remoteResp.Code != http.StatusOK {
		t.Fatalf("remote status = %d, want %d", remoteResp.Code, http.StatusOK)
	}
	if got := strings.TrimSpace(remoteResp.Body.String()); got != "198.51.100.10" {
		t.Fatalf("remote client ip = %q, want %q", got, "198.51.100.10")
	}

	localReq := httptest.NewRequest(http.MethodGet, "/debug/client-ip", nil)
	localReq.RemoteAddr = "127.0.0.1:12345"
	localReq.Header.Set("X-Forwarded-For", "203.0.113.20")
	localResp := httptest.NewRecorder()
	server.engine.ServeHTTP(localResp, localReq)
	if localResp.Code != http.StatusOK {
		t.Fatalf("local status = %d, want %d", localResp.Code, http.StatusOK)
	}
	if got := strings.TrimSpace(localResp.Body.String()); got != "203.0.113.20" {
		t.Fatalf("local client ip = %q, want %q", got, "203.0.113.20")
	}
}

func TestManagementOAuthHelpersRequireManagementAuth(t *testing.T) {
	t.Parallel()

	server := newManagementTestServer(t, &proxyconfig.Config{
		RemoteManagement: proxyconfig.RemoteManagement{
			SecretKey:           "admin-secret",
			IssueAPIKeyPassword: "issue-me",
		},
	})

	t.Run("helper route keeps allow-remote protection", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/management/get-auth-status", nil)
		resp := httptest.NewRecorder()
		server.engine.ServeHTTP(resp, req)

		if resp.Code != http.StatusForbidden {
			t.Fatalf("remote helper status = %d, want %d; body=%s", resp.Code, http.StatusForbidden, resp.Body.String())
		}
	})

	t.Run("helper route requires management key for localhost", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/management/get-auth-status", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		resp := httptest.NewRecorder()
		server.engine.ServeHTTP(resp, req)

		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("local helper status = %d, want %d; body=%s", resp.Code, http.StatusUnauthorized, resp.Body.String())
		}
	})

	t.Run("public issue-api-key stays outside management auth middleware", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v0/management/public/issue-api-key", strings.NewReader(`{}`))
		req.RemoteAddr = "127.0.0.1:12345"
		resp := httptest.NewRecorder()
		server.engine.ServeHTTP(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("public issue-api-key status = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
		}
	})
}

func TestUpdateClientsKeepsLocalPasswordManagementRoutesEnabled(t *testing.T) {
	t.Parallel()

	cfg := &proxyconfig.Config{}
	server := newManagementTestServer(t, cfg, WithLocalManagementPassword("local-secret"))

	requestSession := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v0/management/session", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer local-secret")
		resp := httptest.NewRecorder()
		server.engine.ServeHTTP(resp, req)
		return resp
	}

	before := requestSession()
	if before.Code != http.StatusOK {
		t.Fatalf("session before reload = %d, want %d; body=%s", before.Code, http.StatusOK, before.Body.String())
	}

	reloadedCfg := *cfg
	reloadedCfg.RequestLog = true
	server.UpdateClients(&reloadedCfg)

	after := requestSession()
	if after.Code != http.StatusOK {
		t.Fatalf("session after reload = %d, want %d; body=%s", after.Code, http.StatusOK, after.Body.String())
	}
}
