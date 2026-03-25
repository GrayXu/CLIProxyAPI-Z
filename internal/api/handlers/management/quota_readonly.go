package management

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

const (
	antigravityQuotaPrimaryURL   = "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	antigravityQuotaSecondaryURL = "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels"
	antigravityQuotaFallbackURL  = "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	geminiCLIQuotaURL            = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota"
	geminiCLICodeAssistURL       = "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	claudeProfileURL             = "https://api.anthropic.com/api/oauth/profile"
	claudeUsageURL               = "https://api.anthropic.com/api/oauth/usage"
	codexUsageURL                = "https://chatgpt.com/backend-api/wham/usage"
	kimiUsageURL                 = "https://api.kimi.com/coding/v1/usages"
)

var (
	projectIDPattern        = regexp.MustCompile(`\(([^()]+)\)`)
	antigravityQuotaHeaders = map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    "antigravity/1.11.5 windows/amd64",
	}
	geminiCLIQuotaHeaders = map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
	}
	claudeQuotaHeaders = map[string]string{
		"Authorization":  "Bearer $TOKEN$",
		"Content-Type":   "application/json",
		"anthropic-beta": "oauth-2025-04-20",
	}
	codexQuotaHeaders = map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal",
	}
	kimiQuotaHeaders = map[string]string{
		"Authorization": "Bearer $TOKEN$",
	}
)

type claudeQuotaSnapshot struct {
	Usage   apiCallResponse  `json:"usage"`
	Profile *apiCallResponse `json:"profile,omitempty"`
}

type codexQuotaSnapshot struct {
	Usage    apiCallResponse `json:"usage"`
	PlanType string          `json:"plan_type,omitempty"`
}

type geminiCLIQuotaSnapshot struct {
	Quota      apiCallResponse  `json:"quota"`
	CodeAssist *apiCallResponse `json:"code_assist,omitempty"`
}

func (h *Handler) GetQuotaSnapshot(c *gin.Context) {
	provider := strings.ToLower(strings.TrimSpace(c.Param("provider")))
	authIndex := strings.TrimSpace(c.Query("auth_index"))
	if authIndex == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
		return
	}

	switch provider {
	case "antigravity":
		h.getAntigravityQuota(c, authIndex)
	case "claude":
		h.getClaudeQuota(c, authIndex)
	case "codex":
		h.getCodexQuota(c, authIndex)
	case "gemini-cli":
		h.getGeminiCLIQuota(c, authIndex)
	case "kimi":
		h.getKimiQuota(c, authIndex)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported provider"})
	}
}

func (h *Handler) getAntigravityQuota(c *gin.Context, authIndex string) {
	auth := h.authByIndex(authIndex)
	projectID := resolveAntigravityProjectID(auth)
	if projectID == "" {
		projectID = "bamboo-precept-lgxtn"
	}

	body, _ := json.Marshal(gin.H{"project": projectID})
	urls := []string{
		antigravityQuotaPrimaryURL,
		antigravityQuotaSecondaryURL,
		antigravityQuotaFallbackURL,
	}

	var (
		lastResp apiCallResponse
		lastErr  error
	)
	for _, url := range urls {
		resp, err := h.executeManagedRequest(c.Request.Context(), managedRequestSpec{
			AuthIndex: authIndex,
			Method:    http.MethodPost,
			URL:       url,
			Header:    antigravityQuotaHeaders,
			Data:      string(body),
		})
		if err != nil {
			lastErr = err
			continue
		}
		lastResp = resp
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.JSON(http.StatusOK, resp)
			return
		}
	}

	if lastResp.StatusCode != 0 {
		c.JSON(http.StatusOK, lastResp)
		return
	}
	c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("quota request failed: %v", lastErr)})
}

func (h *Handler) getClaudeQuota(c *gin.Context, authIndex string) {
	usageResp, err := h.executeManagedRequest(c.Request.Context(), managedRequestSpec{
		AuthIndex: authIndex,
		Method:    http.MethodGet,
		URL:       claudeUsageURL,
		Header:    claudeQuotaHeaders,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "quota request failed"})
		return
	}

	profileResp, err := h.executeManagedRequest(c.Request.Context(), managedRequestSpec{
		AuthIndex: authIndex,
		Method:    http.MethodGet,
		URL:       claudeProfileURL,
		Header:    claudeQuotaHeaders,
	})
	if err != nil {
		c.JSON(http.StatusOK, claudeQuotaSnapshot{Usage: usageResp})
		return
	}

	c.JSON(http.StatusOK, claudeQuotaSnapshot{
		Usage:   usageResp,
		Profile: &profileResp,
	})
}

func (h *Handler) getCodexQuota(c *gin.Context, authIndex string) {
	auth := h.authByIndex(authIndex)
	accountID := resolveCodexAccountID(auth)
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing codex account id"})
		return
	}

	headers := cloneStringMap(codexQuotaHeaders)
	headers["Chatgpt-Account-Id"] = accountID
	resp, err := h.executeManagedRequest(c.Request.Context(), managedRequestSpec{
		AuthIndex: authIndex,
		Method:    http.MethodGet,
		URL:       codexUsageURL,
		Header:    headers,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "quota request failed"})
		return
	}

	c.JSON(http.StatusOK, codexQuotaSnapshot{
		Usage:    resp,
		PlanType: resolveCodexPlanType(auth),
	})
}

func (h *Handler) getGeminiCLIQuota(c *gin.Context, authIndex string) {
	auth := h.authByIndex(authIndex)
	projectID := resolveGeminiCLIProjectID(auth)
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing gemini-cli project id"})
		return
	}

	body, _ := json.Marshal(gin.H{"project": projectID})
	quotaResp, err := h.executeManagedRequest(c.Request.Context(), managedRequestSpec{
		AuthIndex: authIndex,
		Method:    http.MethodPost,
		URL:       geminiCLIQuotaURL,
		Header:    geminiCLIQuotaHeaders,
		Data:      string(body),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "quota request failed"})
		return
	}

	codeAssistBody, _ := json.Marshal(gin.H{
		"cloudaicompanionProject": projectID,
		"metadata": gin.H{
			"ideType":    "IDE_UNSPECIFIED",
			"platform":   "PLATFORM_UNSPECIFIED",
			"pluginType": "GEMINI",
		},
	})
	codeAssistResp, err := h.executeManagedRequest(c.Request.Context(), managedRequestSpec{
		AuthIndex: authIndex,
		Method:    http.MethodPost,
		URL:       geminiCLICodeAssistURL,
		Header:    geminiCLIQuotaHeaders,
		Data:      string(codeAssistBody),
	})
	if err != nil {
		c.JSON(http.StatusOK, geminiCLIQuotaSnapshot{Quota: quotaResp})
		return
	}

	c.JSON(http.StatusOK, geminiCLIQuotaSnapshot{
		Quota:      quotaResp,
		CodeAssist: &codeAssistResp,
	})
}

func (h *Handler) getKimiQuota(c *gin.Context, authIndex string) {
	resp, err := h.executeManagedRequest(c.Request.Context(), managedRequestSpec{
		AuthIndex: authIndex,
		Method:    http.MethodGet,
		URL:       kimiUsageURL,
		Header:    kimiQuotaHeaders,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "quota request failed"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func cloneStringMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func resolveAntigravityProjectID(auth *coreauth.Auth) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	if projectID := stringFromMetadata(auth.Metadata, "project_id", "projectId"); projectID != "" {
		return projectID
	}
	if nested := nestedMap(auth.Metadata, "installed"); nested != nil {
		if projectID := stringFromMetadata(nested, "project_id", "projectId"); projectID != "" {
			return projectID
		}
	}
	if nested := nestedMap(auth.Metadata, "web"); nested != nil {
		if projectID := stringFromMetadata(nested, "project_id", "projectId"); projectID != "" {
			return projectID
		}
	}
	return ""
}

func resolveGeminiCLIProjectID(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	candidates := []string{
		stringFromMetadata(auth.Metadata, "account"),
		strings.TrimSpace(authAttribute(auth, "account")),
	}
	for _, candidate := range candidates {
		projectID := extractProjectIDFromAccount(candidate)
		if projectID != "" {
			return projectID
		}
	}
	return ""
}

func extractProjectIDFromAccount(raw string) string {
	matches := projectIDPattern.FindAllStringSubmatch(strings.TrimSpace(raw), -1)
	if len(matches) == 0 {
		return ""
	}
	last := matches[len(matches)-1]
	if len(last) < 2 {
		return ""
	}
	return strings.TrimSpace(last[1])
}

func resolveCodexAccountID(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if accountID := stringFromMetadata(auth.Metadata, "account_id"); accountID != "" {
		return accountID
	}
	claims := extractCodexIDTokenClaims(auth)
	if claims == nil {
		return ""
	}
	if accountID, ok := claims["chatgpt_account_id"].(string); ok {
		return strings.TrimSpace(accountID)
	}
	return ""
}

func resolveCodexPlanType(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if planType := stringFromMetadata(auth.Metadata, "plan_type", "planType"); planType != "" {
		return normalizePlanType(planType)
	}
	claims := extractCodexIDTokenClaims(auth)
	if claims == nil {
		return ""
	}
	if planType, ok := claims["plan_type"].(string); ok {
		return normalizePlanType(planType)
	}
	return ""
}

func normalizePlanType(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	switch trimmed {
	case "plus", "pro", "team", "free":
		return trimmed
	case "plan_plus":
		return "plus"
	case "plan_pro":
		return "pro"
	case "plan_team":
		return "team"
	case "plan_free":
		return "free"
	default:
		return trimmed
	}
}

func stringFromMetadata(metadata map[string]any, keys ...string) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func nestedMap(metadata map[string]any, key string) map[string]any {
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	nested, ok := value.(map[string]any)
	if ok {
		return nested
	}
	generic, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	return generic
}
