package management

import (
	"crypto/subtle"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type role string

const (
	roleAdmin  role = "admin"
	roleViewer role = "viewer"
)

const managementRoleContextKey = "management.role"

type sessionCapabilities struct {
	AllowedRoutes      []string `json:"allowed_routes"`
	ConfigEdit         bool     `json:"config_edit"`
	UsageExport        bool     `json:"usage_export"`
	UsageImport        bool     `json:"usage_import"`
	LogsDownload       bool     `json:"logs_download"`
	LogsClear          bool     `json:"logs_clear"`
	ErrorLogs          bool     `json:"error_logs"`
	RequestLogDownload bool     `json:"request_log_download"`
	DashboardSensitive bool     `json:"dashboard_sensitive"`
	SystemModels       bool     `json:"system_models"`
}

type sessionResponse struct {
	Role         string              `json:"role"`
	Capabilities sessionCapabilities `json:"capabilities"`
}

var (
	adminAllowedRoutes = []string{
		"/",
		"/dashboard",
		"/config",
		"/ai-providers",
		"/auth-files",
		"/oauth",
		"/quota",
		"/usage",
		"/logs",
		"/system",
	}
	viewerAllowedRoutes = []string{
		"/",
		"/dashboard",
		"/quota",
		"/usage",
		"/logs",
		"/system",
	}
)

func (h *Handler) GetSession(c *gin.Context) {
	c.JSON(http.StatusOK, h.sessionResponse(c))
}

func (h *Handler) sessionResponse(c *gin.Context) sessionResponse {
	currentRole := h.roleFromContext(c)
	if currentRole == roleAdmin {
		return sessionResponse{
			Role: string(roleAdmin),
			Capabilities: sessionCapabilities{
				AllowedRoutes:      append([]string(nil), adminAllowedRoutes...),
				ConfigEdit:         true,
				UsageExport:        true,
				UsageImport:        true,
				LogsDownload:       true,
				LogsClear:          true,
				ErrorLogs:          true,
				RequestLogDownload: true,
				DashboardSensitive: true,
				SystemModels:       true,
			},
		}
	}

	return sessionResponse{
		Role: string(roleViewer),
		Capabilities: sessionCapabilities{
			AllowedRoutes:      append([]string(nil), viewerAllowedRoutes...),
			ConfigEdit:         false,
			UsageExport:        false,
			UsageImport:        false,
			LogsDownload:       false,
			LogsClear:          false,
			ErrorLogs:          false,
			RequestLogDownload: false,
			DashboardSensitive: false,
			SystemModels:       false,
		},
	}
}

func (h *Handler) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.roleFromContext(c) != roleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin access required"})
			return
		}
		c.Next()
	}
}

func (h *Handler) roleFromContext(c *gin.Context) role {
	if c == nil {
		return roleAdmin
	}
	value, ok := c.Get(managementRoleContextKey)
	if !ok {
		return roleAdmin
	}
	switch typed := value.(type) {
	case role:
		return typed
	case string:
		if strings.EqualFold(strings.TrimSpace(typed), string(roleViewer)) {
			return roleViewer
		}
	}
	return roleAdmin
}

func (h *Handler) setRoleContext(c *gin.Context, currentRole role) {
	if c == nil {
		return
	}
	c.Set(managementRoleContextKey, currentRole)
}

func (h *Handler) resolveRoleForKey(provided string, secretHash string, envSecret string, localClient bool) role {
	if localClient {
		if lp := strings.TrimSpace(h.localPassword); lp != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(lp)) == 1 {
			return roleAdmin
		}
	}

	if envSecret != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(envSecret)) == 1 {
		return roleAdmin
	}

	if secretHash != "" && compareSecretHash(secretHash, provided) {
		return roleAdmin
	}

	if h.viewerKeyAllowed(provided) {
		return roleViewer
	}

	return ""
}

func (h *Handler) viewerKeyAllowed(provided string) bool {
	if h == nil || h.cfg == nil {
		return false
	}
	trimmed := strings.TrimSpace(provided)
	if !strings.HasPrefix(strings.ToLower(trimmed), "sk-") {
		return false
	}
	for _, key := range h.cfg.APIKeys {
		candidate := strings.TrimSpace(key)
		if candidate == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(trimmed)) == 1 {
			return true
		}
	}
	return false
}

func (h *Handler) hasViewerManagementKeys() bool {
	if h == nil || h.cfg == nil {
		return false
	}
	for _, key := range h.cfg.APIKeys {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "sk-") {
			return true
		}
	}
	return false
}

func compareSecretHash(secretHash string, provided string) bool {
	return strings.TrimSpace(secretHash) != "" && bcrypt.CompareHashAndPassword([]byte(secretHash), []byte(provided)) == nil
}

func viewerConfigPayload(cfg configViewSource) gin.H {
	payload := gin.H{
		"usage-statistics-enabled": cfg.usageStatisticsEnabled(),
		"request-log":              cfg.requestLog(),
		"logging-to-file":          cfg.loggingToFile(),
		"routing": gin.H{
			"strategy": cfg.routingStrategy(),
		},
	}
	return payload
}

type configViewSource interface {
	usageStatisticsEnabled() bool
	requestLog() bool
	loggingToFile() bool
	routingStrategy() string
}

type handlerConfigView struct {
	h *Handler
}

func (v handlerConfigView) usageStatisticsEnabled() bool {
	return v.h != nil && v.h.cfg != nil && v.h.cfg.UsageStatisticsEnabled
}
func (v handlerConfigView) requestLog() bool {
	return v.h != nil && v.h.cfg != nil && v.h.cfg.RequestLog
}
func (v handlerConfigView) loggingToFile() bool {
	return v.h != nil && v.h.cfg != nil && v.h.cfg.LoggingToFile
}
func (v handlerConfigView) routingStrategy() string {
	if v.h == nil || v.h.cfg == nil {
		return ""
	}
	return strings.TrimSpace(v.h.cfg.Routing.Strategy)
}

func redactAuthFileEntryForViewer(entry gin.H) gin.H {
	if len(entry) == 0 {
		return gin.H{}
	}
	safe := gin.H{}
	allow := map[string]struct{}{
		"id":               {},
		"auth_index":       {},
		"name":             {},
		"type":             {},
		"provider":         {},
		"label":            {},
		"status":           {},
		"status_message":   {},
		"disabled":         {},
		"unavailable":      {},
		"runtime_only":     {},
		"source":           {},
		"size":             {},
		"email":            {},
		"account_type":     {},
		"account":          {},
		"created_at":       {},
		"modtime":          {},
		"updated_at":       {},
		"last_refresh":     {},
		"next_retry_after": {},
	}
	keys := make([]string, 0, len(entry))
	for key := range entry {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, ok := allow[key]; ok {
			safe[key] = entry[key]
		}
	}
	return safe
}
