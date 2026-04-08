// Package management provides the management API handlers and middleware
// for configuring the server and managing auth files.
package management

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v6/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"gopkg.in/yaml.v3"
)

type attemptInfo struct {
	count        int
	blockedUntil time.Time
	lastActivity time.Time // track last activity for cleanup
}

// attemptCleanupInterval controls how often stale IP entries are purged
const attemptCleanupInterval = 1 * time.Hour

// attemptMaxIdleTime controls how long an IP can be idle before cleanup
const attemptMaxIdleTime = 2 * time.Hour

const managementMaxFailures = 3

const managementBanDuration = 30 * time.Minute

// Handler aggregates config reference, persistence path and helpers.
type Handler struct {
	cfg                 *config.Config
	configFilePath      string
	mu                  sync.Mutex
	attemptsMu          sync.Mutex
	failedAttempts      map[string]*attemptInfo // keyed by client IP
	authManager         *coreauth.Manager
	usageStats          *usage.RequestStatistics
	tokenStore          coreauth.Store
	localPassword       string
	allowRemoteOverride bool
	envSecret           string
	logDir              string
	postAuthHook        coreauth.PostAuthHook
	configUpdatedHook   func(*config.Config)
}

// NewHandler creates a new management handler instance.
func NewHandler(cfg *config.Config, configFilePath string, manager *coreauth.Manager) *Handler {
	envSecret, _ := os.LookupEnv("MANAGEMENT_PASSWORD")
	envSecret = strings.TrimSpace(envSecret)

	h := &Handler{
		cfg:                 cfg,
		configFilePath:      configFilePath,
		failedAttempts:      make(map[string]*attemptInfo),
		authManager:         manager,
		usageStats:          usage.GetRequestStatistics(),
		tokenStore:          sdkAuth.GetTokenStore(),
		allowRemoteOverride: envSecret != "",
		envSecret:           envSecret,
	}
	h.startAttemptCleanup()
	return h
}

// startAttemptCleanup launches a background goroutine that periodically
// removes stale IP entries from failedAttempts to prevent memory leaks.
func (h *Handler) startAttemptCleanup() {
	go func() {
		ticker := time.NewTicker(attemptCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			h.purgeStaleAttempts()
		}
	}()
}

// purgeStaleAttempts removes IP entries that have been idle beyond attemptMaxIdleTime
// and whose ban (if any) has expired.
func (h *Handler) purgeStaleAttempts() {
	now := time.Now()
	h.attemptsMu.Lock()
	defer h.attemptsMu.Unlock()
	for ip, ai := range h.failedAttempts {
		// Skip if still banned
		if !ai.blockedUntil.IsZero() && now.Before(ai.blockedUntil) {
			continue
		}
		// Remove if idle too long
		if now.Sub(ai.lastActivity) > attemptMaxIdleTime {
			delete(h.failedAttempts, ip)
		}
	}
}

// NewHandler creates a new management handler instance.
func NewHandlerWithoutConfigFilePath(cfg *config.Config, manager *coreauth.Manager) *Handler {
	return NewHandler(cfg, "", manager)
}

// SetConfig updates the in-memory config reference when the server hot-reloads.
func (h *Handler) SetConfig(cfg *config.Config) { h.cfg = cfg }

// SetAuthManager updates the auth manager reference used by management endpoints.
func (h *Handler) SetAuthManager(manager *coreauth.Manager) { h.authManager = manager }

// SetUsageStatistics allows replacing the usage statistics reference.
func (h *Handler) SetUsageStatistics(stats *usage.RequestStatistics) { h.usageStats = stats }

// SetLocalPassword configures the runtime-local password accepted for localhost requests.
func (h *Handler) SetLocalPassword(password string) { h.localPassword = password }

// SetLogDirectory updates the directory where main.log should be looked up.
func (h *Handler) SetLogDirectory(dir string) {
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	h.logDir = dir
}

// SetPostAuthHook registers a hook to be called after auth record creation but before persistence.
func (h *Handler) SetPostAuthHook(hook coreauth.PostAuthHook) {
	h.postAuthHook = hook
}

// SetConfigUpdatedHook registers a hook that runs after config is durably written.
func (h *Handler) SetConfigUpdatedHook(hook func(*config.Config)) {
	h.configUpdatedHook = hook
}

// Middleware enforces access control for management endpoints.
// All requests (local and remote) require a valid management key.
// Additionally, remote access requires allow-remote-management=true.
func (h *Handler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-CPA-VERSION", buildinfo.Version)
		c.Header("X-CPA-COMMIT", buildinfo.Commit)
		c.Header("X-CPA-BUILD-DATE", buildinfo.BuildDate)

		clientIP := c.ClientIP()
		localClient := isLoopbackClient(clientIP)
		cfg := h.cfg
		var (
			allowRemote bool
			secretHash  string
		)
		if cfg != nil {
			allowRemote = cfg.RemoteManagement.AllowRemote
			secretHash = cfg.RemoteManagement.SecretKey
		}
		if h.allowRemoteOverride {
			allowRemote = true
		}
		envSecret := h.envSecret

		fail := func() {}
		if !localClient {
			if remaining, blocked := h.blockedForClient(clientIP); blocked {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("IP banned due to too many failed attempts. Try again in %s", remaining)})
				return
			}

			if !allowRemote {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "remote management disabled"})
				return
			}

			fail = func() {
				h.recordFailedAttempt(clientIP)
			}
		}
		if secretHash == "" && envSecret == "" && strings.TrimSpace(h.localPassword) == "" && !h.hasViewerManagementKeys() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "remote management key not set"})
			return
		}

		// Accept either Authorization: Bearer <key> or X-Management-Key
		var provided string
		if ah := c.GetHeader("Authorization"); ah != "" {
			parts := strings.SplitN(ah, " ", 2)
			if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
				provided = parts[1]
			} else {
				provided = ah
			}
		}
		if provided == "" {
			provided = c.GetHeader("X-Management-Key")
		}

		if provided == "" {
			if !localClient {
				fail()
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing management key"})
			return
		}

		currentRole := h.resolveRoleForKey(provided, secretHash, envSecret, localClient)
		if currentRole == roleAdmin {
			if !localClient {
				h.clearFailedAttempts(clientIP)
			}
			h.setRoleContext(c, roleAdmin)
			c.Next()
			return
		}

		if currentRole == roleViewer {
			if !localClient {
				h.clearFailedAttempts(clientIP)
			}
			h.setRoleContext(c, roleViewer)
			c.Next()
			return
		}

		if currentRole == "" {
			if !localClient {
				fail()
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid management key"})
			return
		}
	}
}

func isLoopbackClient(clientIP string) bool {
	return clientIP == "127.0.0.1" || clientIP == "::1"
}

func (h *Handler) blockedForClient(clientIP string) (time.Duration, bool) {
	h.attemptsMu.Lock()
	defer h.attemptsMu.Unlock()
	ai := h.failedAttempts[clientIP]
	if ai == nil || ai.blockedUntil.IsZero() {
		return 0, false
	}
	if time.Now().Before(ai.blockedUntil) {
		return time.Until(ai.blockedUntil).Round(time.Second), true
	}
	ai.blockedUntil = time.Time{}
	ai.count = 0
	return 0, false
}

func (h *Handler) recordFailedAttempt(clientIP string) {
	h.attemptsMu.Lock()
	defer h.attemptsMu.Unlock()
	aip := h.failedAttempts[clientIP]
	if aip == nil {
		aip = &attemptInfo{}
		h.failedAttempts[clientIP] = aip
	}
	aip.count++
	aip.lastActivity = time.Now()
	if aip.count >= managementMaxFailures {
		aip.blockedUntil = time.Now().Add(managementBanDuration)
		aip.count = 0
	}
}

func (h *Handler) clearFailedAttempts(clientIP string) {
	h.attemptsMu.Lock()
	defer h.attemptsMu.Unlock()
	if ai := h.failedAttempts[clientIP]; ai != nil {
		ai.count = 0
		ai.blockedUntil = time.Time{}
		ai.lastActivity = time.Now()
	}
}

func cloneConfigSnapshot(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return cfg
	}
	var out config.Config
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return cfg
	}
	return &out
}

// persist saves the current in-memory config to disk.
func (h *Handler) persist(c *gin.Context) bool {
	var snapshot *config.Config
	hook := h.configUpdatedHook
	h.mu.Lock()
	// Preserve comments when writing
	if err := config.SaveConfigPreserveComments(h.configFilePath, h.cfg); err != nil {
		h.mu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save config: %v", err)})
		return false
	}
	snapshot = cloneConfigSnapshot(h.cfg)
	h.mu.Unlock()
	if hook != nil && snapshot != nil {
		hook(snapshot)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
	return true
}

// Helper methods for simple types
func (h *Handler) updateBoolField(c *gin.Context, set func(bool)) {
	var body struct {
		Value *bool `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	set(*body.Value)
	h.persist(c)
}

func (h *Handler) updateIntField(c *gin.Context, set func(int)) {
	var body struct {
		Value *int `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	set(*body.Value)
	h.persist(c)
}

func (h *Handler) updateStringField(c *gin.Context, set func(string)) {
	var body struct {
		Value *string `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	set(*body.Value)
	h.persist(c)
}
