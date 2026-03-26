package auth

import (
	"strings"
	"time"

	codexauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codex"
)

const (
	routingWeeklyResetAtMetadataKey    = "routing_weekly_reset_at"
	routingWeeklySnapshotAtMetadataKey = "routing_weekly_snapshot_at"
	codexQuotaSnapshotMetadataKey      = "codex_quota_snapshot"
	codexQuotaSnapshotAtMetadataKey    = "codex_quota_snapshot_at"
	codexQuotaSnapshotRefreshInterval  = 10 * time.Minute
)

func betterAuthForFillFirst(candidate *Auth, current *Auth) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}

	candidateProvider := strings.ToLower(strings.TrimSpace(candidate.Provider))
	currentProvider := strings.ToLower(strings.TrimSpace(current.Provider))
	if candidateProvider == "codex" && currentProvider == "codex" {
		candidateResetAt, candidateHasResetAt := authRoutingWeeklyResetAt(candidate)
		currentResetAt, currentHasResetAt := authRoutingWeeklyResetAt(current)
		if candidateHasResetAt != currentHasResetAt {
			return candidateHasResetAt
		}
		if candidateHasResetAt && !candidateResetAt.Equal(currentResetAt) {
			return candidateResetAt.Before(currentResetAt)
		}
	}

	return candidate.ID < current.ID
}

func authRoutingWeeklyResetAt(auth *Auth) (time.Time, bool) {
	if auth == nil || len(auth.Metadata) == 0 {
		return time.Time{}, false
	}
	return lookupMetadataTime(auth.Metadata, routingWeeklyResetAtMetadataKey)
}

func authRoutingWeeklySnapshotAt(auth *Auth) (time.Time, bool) {
	if auth == nil || len(auth.Metadata) == 0 {
		return time.Time{}, false
	}
	return lookupMetadataTime(auth.Metadata, routingWeeklySnapshotAtMetadataKey)
}

func codexQuotaSnapshotNeedsRefresh(auth *Auth, now time.Time) bool {
	if auth == nil || auth.Disabled {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
		return false
	}
	if len(auth.Metadata) == 0 {
		return false
	}
	if !hasMetadataString(auth.Metadata, "access_token") {
		return false
	}
	if !hasCodexQuotaAccountID(auth) {
		return false
	}

	snapshotAt, ok := authRoutingWeeklySnapshotAt(auth)
	if !ok {
		return true
	}
	return now.Sub(snapshotAt) >= codexQuotaSnapshotRefreshInterval
}

func StoreRoutingWeeklySnapshot(auth *Auth, resetAt *time.Time, snapshotAt time.Time) {
	if auth == nil {
		return
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}

	if snapshotAt.IsZero() {
		delete(auth.Metadata, routingWeeklySnapshotAtMetadataKey)
	} else {
		auth.Metadata[routingWeeklySnapshotAtMetadataKey] = snapshotAt.UTC().Format(time.RFC3339)
	}

	if resetAt == nil || resetAt.IsZero() {
		delete(auth.Metadata, routingWeeklyResetAtMetadataKey)
		return
	}
	auth.Metadata[routingWeeklyResetAtMetadataKey] = resetAt.UTC().Format(time.RFC3339)
}

func StoreRoutingWeeklySnapshotObservedAt(auth *Auth, snapshotAt time.Time) {
	if auth == nil {
		return
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	if snapshotAt.IsZero() {
		delete(auth.Metadata, routingWeeklySnapshotAtMetadataKey)
		return
	}
	auth.Metadata[routingWeeklySnapshotAtMetadataKey] = snapshotAt.UTC().Format(time.RFC3339)
}

func ReadCodexQuotaSnapshot(auth *Auth) (string, bool) {
	if auth == nil || len(auth.Metadata) == 0 {
		return "", false
	}
	value, ok := auth.Metadata[codexQuotaSnapshotMetadataKey]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func StoreCodexQuotaSnapshot(auth *Auth, payload string, snapshotAt time.Time) {
	if auth == nil {
		return
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	payload = strings.TrimSpace(payload)
	if payload == "" {
		delete(auth.Metadata, codexQuotaSnapshotMetadataKey)
		delete(auth.Metadata, codexQuotaSnapshotAtMetadataKey)
		return
	}
	auth.Metadata[codexQuotaSnapshotMetadataKey] = payload
	if snapshotAt.IsZero() {
		delete(auth.Metadata, codexQuotaSnapshotAtMetadataKey)
		return
	}
	auth.Metadata[codexQuotaSnapshotAtMetadataKey] = snapshotAt.UTC().Format(time.RFC3339)
}

func hasMetadataString(meta map[string]any, keys ...string) bool {
	if len(meta) == 0 {
		return false
	}
	for _, key := range keys {
		value, ok := meta[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return true
		}
	}
	return false
}

func hasCodexQuotaAccountID(auth *Auth) bool {
	if auth == nil || len(auth.Metadata) == 0 {
		return false
	}
	if hasMetadataString(auth.Metadata, "account_id") {
		return true
	}
	idToken, ok := metadataStringValue(auth.Metadata, "id_token")
	if !ok {
		return false
	}
	claims, err := codexauth.ParseJWTToken(idToken)
	if err != nil || claims == nil {
		return false
	}
	return strings.TrimSpace(claims.GetAccountID()) != ""
}

func metadataStringValue(meta map[string]any, key string) (string, bool) {
	if len(meta) == 0 {
		return "", false
	}
	value, ok := meta[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}
