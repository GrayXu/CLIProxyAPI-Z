package management

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

const (
	issuedAPIKeyPrefix      = "sk-dist-"
	issuedAPIKeyEntropySize = 18
)

var issuedAPIKeyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

type issueAPIKeyRequest struct {
	Password string `json:"password"`
}

type issueAPIKeyResponse struct {
	APIKey string `json:"api_key"`
	Role   string `json:"role"`
}

func (h *Handler) IssueAPIKey(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "management unavailable"})
		return
	}

	clientIP := c.ClientIP()
	localClient := isLoopbackClient(clientIP)
	allowRemote := h.cfg.RemoteManagement.AllowRemote
	if h.allowRemoteOverride {
		allowRemote = true
	}

	if !localClient {
		if remaining, blocked := h.blockedForClient(clientIP); blocked {
			c.JSON(http.StatusForbidden, gin.H{"error": "too_many_attempts", "message": "too many failed attempts", "retry_after": remaining.String()})
			return
		}
		if !allowRemote {
			c.JSON(http.StatusForbidden, gin.H{"error": "remote management disabled"})
			return
		}
	}

	passwordHash := strings.TrimSpace(h.cfg.RemoteManagement.IssueAPIKeyPassword)
	if passwordHash == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "issuance_disabled", "message": "api key issuance is disabled"})
		return
	}

	var body issueAPIKeyRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	provided := strings.TrimSpace(body.Password)
	if provided == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing password"})
		return
	}

	if !matchesIssuePassword(passwordHash, provided) {
		if !localClient {
			h.recordFailedAttempt(clientIP)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid password"})
		return
	}
	if !localClient {
		h.clearFailedAttempts(clientIP)
	}

	apiKey, err := h.issueAPIKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "issue_failed", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, issueAPIKeyResponse{
		APIKey: apiKey,
		Role:   string(roleViewer),
	})
}

func matchesIssuePassword(stored string, provided string) bool {
	trimmedStored := strings.TrimSpace(stored)
	trimmedProvided := strings.TrimSpace(provided)
	if trimmedStored == "" || trimmedProvided == "" {
		return false
	}
	if compareSecretHash(trimmedStored, trimmedProvided) {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(trimmedStored), []byte(trimmedProvided)) == 1
}

func (h *Handler) issueAPIKey() (string, error) {
	if h == nil || h.cfg == nil {
		return "", errors.New("management unavailable")
	}

	var (
		snapshot *config.Config
		hook     func(*config.Config)
		apiKey   string
	)

	h.mu.Lock()
	defer func() {
		if hook != nil && snapshot != nil {
			hook(snapshot)
		}
	}()

	for attempt := 0; attempt < 8; attempt++ {
		candidate, err := newIssuedAPIKey()
		if err != nil {
			h.mu.Unlock()
			return "", err
		}
		if containsStringExact(h.cfg.APIKeys, candidate) {
			continue
		}

		originalLen := len(h.cfg.APIKeys)
		h.cfg.APIKeys = append(h.cfg.APIKeys, candidate)
		if err := config.SaveConfigPreserveComments(h.configFilePath, h.cfg); err != nil {
			h.cfg.APIKeys = h.cfg.APIKeys[:originalLen]
			h.mu.Unlock()
			return "", err
		}
		snapshot = cloneConfigSnapshot(h.cfg)
		hook = h.configUpdatedHook
		apiKey = candidate
		h.mu.Unlock()
		return apiKey, nil
	}

	h.mu.Unlock()
	return "", errors.New("failed to generate a unique api key")
}

func newIssuedAPIKey() (string, error) {
	buf := make([]byte, issuedAPIKeyEntropySize)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return issuedAPIKeyPrefix + strings.ToLower(issuedAPIKeyEncoding.EncodeToString(buf)), nil
}

func containsStringExact(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
