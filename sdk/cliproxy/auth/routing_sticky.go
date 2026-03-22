package auth

import (
	"encoding/json"
	"hash/fnv"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

const (
	routingQuotaScoreMetadataKey  = "routing_quota_score"
	routingQuotaScoresMetadataKey = "routing_quota_scores"
	stickyRouteTTL                = 1 * time.Hour
	stickyRouteMaxEntries         = 10000
	stickyRouteShardCount         = 64
	stickyRouteCleanupInterval    = 5 * time.Minute
)

type stickyRouteEntry struct {
	authID   string
	lastUsed time.Time
}

type stickyRouter struct {
	shards          []stickyRouteShard
	ttl             time.Duration
	perShardLimit   int
	cleanupInterval time.Duration
	now             func() time.Time
	stopCh          chan struct{}
	stopOnce        sync.Once
	nextCleanup     atomic.Uint32
}

type stickyRouteShard struct {
	mu      sync.Mutex
	entries map[string]stickyRouteEntry
}

type stickySelectionState struct {
	conversationKey string
	pinnedAuthID    string
	canBind         bool
}

func newStickyRouter(ttl time.Duration, maxEntries int) *stickyRouter {
	if ttl <= 0 {
		ttl = stickyRouteTTL
	}
	if maxEntries <= 0 {
		maxEntries = stickyRouteMaxEntries
	}
	perShardLimit := maxEntries / stickyRouteShardCount
	if maxEntries%stickyRouteShardCount != 0 {
		perShardLimit++
	}
	router := &stickyRouter{
		shards:          make([]stickyRouteShard, stickyRouteShardCount),
		ttl:             ttl,
		perShardLimit:   perShardLimit,
		cleanupInterval: stickyRouteCleanupInterval,
		now:             time.Now,
		stopCh:          make(chan struct{}),
	}
	for index := range router.shards {
		router.shards[index].entries = make(map[string]stickyRouteEntry)
	}
	go router.runCleanupLoop()
	return router
}

func (r *stickyRouter) get(key string) (string, bool) {
	if r == nil {
		return "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	shard := r.shardForKey(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	now := r.now()
	entry, ok := shard.entries[key]
	if !ok || strings.TrimSpace(entry.authID) == "" {
		return "", false
	}
	if r.entryExpired(now, entry) {
		delete(shard.entries, key)
		return "", false
	}
	entry.lastUsed = now
	shard.entries[key] = entry
	return entry.authID, true
}

func (r *stickyRouter) set(key, authID string) {
	if r == nil {
		return
	}
	key = strings.TrimSpace(key)
	authID = strings.TrimSpace(authID)
	if key == "" || authID == "" {
		return
	}
	shard := r.shardForKey(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	now := r.now()
	shard.entries[key] = stickyRouteEntry{authID: authID, lastUsed: now}
	r.evictOneLocked(shard)
}

func (r *stickyRouter) delete(key string) {
	if r == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	shard := r.shardForKey(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	delete(shard.entries, key)
}

func (r *stickyRouter) Close() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() { close(r.stopCh) })
}

func (r *stickyRouter) runCleanupLoop() {
	if r == nil || r.cleanupInterval <= 0 {
		return
	}
	ticker := time.NewTicker(r.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.cleanupExpiredShard(int(r.nextCleanup.Add(1)-1) % len(r.shards))
		case <-r.stopCh:
			return
		}
	}
}

func (r *stickyRouter) cleanupExpiredShard(index int) {
	if r == nil || len(r.shards) == 0 {
		return
	}
	now := r.now()
	shard := &r.shards[index]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	r.pruneExpiredLocked(shard, now)
}

func (r *stickyRouter) shardForKey(key string) *stickyRouteShard {
	if len(r.shards) == 1 {
		return &r.shards[0]
	}
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(key))
	return &r.shards[hasher.Sum64()%uint64(len(r.shards))]
}

func (r *stickyRouter) pruneExpiredLocked(shard *stickyRouteShard, now time.Time) {
	if r == nil || shard == nil || len(shard.entries) == 0 || r.ttl <= 0 {
		return
	}
	for key, entry := range shard.entries {
		if r.entryExpired(now, entry) {
			delete(shard.entries, key)
		}
	}
}

func (r *stickyRouter) entryExpired(now time.Time, entry stickyRouteEntry) bool {
	if r == nil || r.ttl <= 0 {
		return false
	}
	return entry.lastUsed.IsZero() || now.Sub(entry.lastUsed) > r.ttl
}

func (r *stickyRouter) evictOneLocked(shard *stickyRouteShard) {
	if r == nil || shard == nil || r.perShardLimit <= 0 || len(shard.entries) <= r.perShardLimit {
		return
	}
	oldestKey := ""
	oldestTime := time.Time{}
	for key, entry := range shard.entries {
		if oldestKey == "" || entry.lastUsed.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.lastUsed
		}
	}
	if oldestKey != "" {
		delete(shard.entries, oldestKey)
	}
}

func stickyConversationKeyFromMetadata(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	raw, ok := meta[cliproxyexecutor.StickyConversationMetadataKey]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

func withPinnedAuthMetadata(opts cliproxyexecutor.Options, authID string) cliproxyexecutor.Options {
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return opts
	}
	if current := pinnedAuthIDFromMetadata(opts.Metadata); current == authID {
		return opts
	}
	if len(opts.Metadata) == 0 {
		opts.Metadata = map[string]any{cliproxyexecutor.PinnedAuthMetadataKey: authID}
		return opts
	}
	meta := make(map[string]any, len(opts.Metadata)+1)
	for key, value := range opts.Metadata {
		meta[key] = value
	}
	meta[cliproxyexecutor.PinnedAuthMetadataKey] = authID
	opts.Metadata = meta
	return opts
}

func authQuotaScore(auth *Auth, model string) float64 {
	if auth == nil || len(auth.Metadata) == 0 {
		return 0
	}
	model = canonicalModelKey(model)
	if raw, ok := auth.Metadata[routingQuotaScoresMetadataKey]; ok {
		if score, found := quotaScoreForModel(raw, model); found {
			return score
		}
	}
	if raw, ok := auth.Metadata[routingQuotaScoreMetadataKey]; ok {
		if score, found := parseQuotaScoreValue(raw); found {
			return score
		}
	}
	return 0
}

func quotaScoreForModel(raw any, model string) (float64, bool) {
	switch scores := raw.(type) {
	case map[string]any:
		return lookupQuotaScoreMap(scores, model)
	case map[string]float64:
		if score, ok := scores[model]; ok {
			return score, true
		}
	case map[string]int:
		if score, ok := scores[model]; ok {
			return float64(score), true
		}
	case map[string]json.Number:
		if score, ok := scores[model]; ok {
			return parseQuotaScoreValue(score)
		}
	}
	return 0, false
}

func lookupQuotaScoreMap(scores map[string]any, model string) (float64, bool) {
	if len(scores) == 0 {
		return 0, false
	}
	if model != "" {
		if raw, ok := scores[model]; ok {
			return parseQuotaScoreValue(raw)
		}
	}
	for key, raw := range scores {
		if canonicalModelKey(key) == model && model != "" {
			return parseQuotaScoreValue(raw)
		}
	}
	return 0, false
}

func parseQuotaScoreValue(raw any) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int8:
		return float64(value), true
	case int16:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint8:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	case json.Number:
		if score, err := value.Float64(); err == nil {
			return score, true
		}
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, false
		}
		score, err := strconv.ParseFloat(trimmed, 64)
		if err == nil {
			return score, true
		}
	}
	return 0, false
}

func betterAuthByQuotaScore(candidate *Auth, current *Auth, model string) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}
	candidateScore := authQuotaScore(candidate, model)
	currentScore := authQuotaScore(current, model)
	if candidateScore == currentScore {
		return candidate.ID < current.ID
	}
	return candidateScore > currentScore
}

func (m *Manager) stickySelectionState(providers []string, model string, opts cliproxyexecutor.Options, tried map[string]struct{}) stickySelectionState {
	state := stickySelectionState{
		conversationKey: stickyConversationKeyFromMetadata(opts.Metadata),
	}
	if state.conversationKey == "" {
		return state
	}
	if pinnedAuthIDFromMetadata(opts.Metadata) != "" {
		return state
	}
	mappedAuthID, ok := m.stickyRouter.get(state.conversationKey)
	if !ok {
		state.canBind = true
		return state
	}
	if len(tried) > 0 {
		if _, seen := tried[mappedAuthID]; seen {
			return state
		}
	}
	if m.stickyAuthUsable(mappedAuthID, providers, model) {
		state.pinnedAuthID = mappedAuthID
		return state
	}
	m.stickyRouter.delete(state.conversationKey)
	state.canBind = true
	return state
}

func (m *Manager) stickyAuthUsable(authID string, providers []string, model string) bool {
	if m == nil {
		return false
	}
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return false
	}
	providerSet := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		providerKey := strings.ToLower(strings.TrimSpace(provider))
		if providerKey != "" {
			providerSet[providerKey] = struct{}{}
		}
	}
	modelKey := canonicalModelKey(model)
	now := time.Now()

	m.mu.RLock()
	auth := m.auths[authID]
	m.mu.RUnlock()
	if auth == nil || auth.Disabled {
		return false
	}
	if len(providerSet) > 0 {
		if _, ok := providerSet[strings.ToLower(strings.TrimSpace(auth.Provider))]; !ok {
			return false
		}
	}
	if modelKey != "" {
		registryRef := registry.GetGlobalRegistry()
		if registryRef != nil && !registryRef.ClientSupportsModel(auth.ID, modelKey) {
			return false
		}
	}
	blocked, _, _ := isAuthBlockedForModel(auth, modelKey, now)
	return !blocked
}

func (m *Manager) bindStickyRoute(conversationKey, authID string) {
	if m == nil || m.stickyRouter == nil {
		return
	}
	m.stickyRouter.set(conversationKey, authID)
}
