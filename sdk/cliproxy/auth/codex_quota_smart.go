package auth

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

const (
	routingCodexSmartStateMetadataKey = "routing_codex_smart_state"
	codexSmartPrewarmCooldown         = 10 * time.Minute
	codexSmartFiveHourWindow          = 5 * time.Hour
	codexSmartFiveHourThreshold       = 0.30
	codexSmartWeeklyFullThreshold     = 0.99
	codexSmartCandidatePoolMin        = 2
	codexSmartCandidatePoolMax        = 4
	codexSmartCandidatePoolRatio      = 0.90
	codexSmartDefaultPlanWeight       = 1.0
	codexSmartPaidPlanWeight          = 5.0
)

// CodexQuotaSmartSelector prioritizes Codex auths using weekly urgency first
// and only uses 5-hour quota data for local smoothing among top weekly candidates.
type CodexQuotaSmartSelector struct{}

type codexQuotaSmartWindow struct {
	RemainingFraction *float64 `json:"remaining_fraction,omitempty"`
	ResetAt           string   `json:"reset_at,omitempty"`
}

type codexQuotaSmartWeeklyWindow struct {
	Started bool `json:"started"`
	codexQuotaSmartWindow
}

type codexQuotaSmartState struct {
	SnapshotAt    string                      `json:"snapshot_at,omitempty"`
	PlanType      string                      `json:"plan_type,omitempty"`
	Weekly        codexQuotaSmartWeeklyWindow `json:"weekly"`
	FiveHour      codexQuotaSmartWindow       `json:"five_hour"`
	Local5HEvents []int64                     `json:"local_5h_events,omitempty"`
	LastPrewarmAt string                      `json:"last_prewarm_at,omitempty"`
}

type codexQuotaSmartWindowSnapshot struct {
	Exists            bool
	RemainingFraction *float64
	ResetAt           time.Time
}

type codexQuotaSmartCandidate struct {
	entry             *scheduledAuth
	state             codexQuotaSmartState
	planType          string
	planWeight        float64
	snapshotAt        time.Time
	lastPrewarmAt     time.Time
	weeklyResetAt     time.Time
	fiveHourResetAt   time.Time
	localEventCount   int
	fiveHourRemaining float64
	hasFiveHour       bool
	weeklyUrgency     float64
	hasWeeklyUrgency  bool
	prewarmEligible   bool
}

func (s *CodexQuotaSmartSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	_ = ctx
	_ = opts
	now := time.Now()
	available, err := getAvailableAuths(auths, provider, model, now)
	if err != nil {
		return nil, err
	}
	available = preferCodexWebsocketAuths(ctx, provider, available)
	entries := make([]*scheduledAuth, 0, len(available))
	for _, auth := range available {
		entries = append(entries, &scheduledAuth{auth: auth})
	}
	cursor := 0
	picked := pickCodexQuotaSmartReady(entries, canonicalModelKey(model), &cursor, nil)
	if picked == nil {
		return nil, &Error{Code: "auth_not_found", Message: "no auth available"}
	}
	return picked.auth, nil
}

func ReadCodexQuotaSmartState(auth *Auth, now time.Time) (codexQuotaSmartState, bool) {
	if auth == nil || len(auth.Metadata) == 0 {
		return codexQuotaSmartState{}, false
	}
	raw, ok := auth.Metadata[routingCodexSmartStateMetadataKey]
	if !ok || raw == nil {
		return codexQuotaSmartState{}, false
	}

	var state codexQuotaSmartState
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return codexQuotaSmartState{}, false
		}
		if err := json.Unmarshal([]byte(value), &state); err != nil {
			return codexQuotaSmartState{}, false
		}
	case map[string]any:
		data, err := json.Marshal(value)
		if err != nil {
			return codexQuotaSmartState{}, false
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return codexQuotaSmartState{}, false
		}
	default:
		return codexQuotaSmartState{}, false
	}

	state.Local5HEvents = pruneCodexSmartLocalEvents(state.Local5HEvents, now)
	state.PlanType = codexQuotaSmartResolvePlanType(auth, state)
	return state, true
}

func StoreCodexQuotaSmartState(auth *Auth, state codexQuotaSmartState) {
	if auth == nil {
		return
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	state.Local5HEvents = pruneCodexSmartLocalEvents(state.Local5HEvents, time.Now().UTC())
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	auth.Metadata[routingCodexSmartStateMetadataKey] = string(data)
}

func UpdateCodexQuotaSmartStateFromSnapshot(auth *Auth, payload string, snapshotAt time.Time) {
	if auth == nil {
		return
	}
	snapshotAt = snapshotAt.UTC()
	state, _ := ReadCodexQuotaSmartState(auth, snapshotAt)
	state.SnapshotAt = snapshotAt.Format(time.RFC3339)

	fiveHour, weekly, planType := extractCodexQuotaSmartSnapshot([]byte(payload), snapshotAt)
	if planType != "" {
		state.PlanType = planType
	}
	state.PlanType = codexQuotaSmartResolvePlanType(auth, state)

	state.FiveHour.RemainingFraction = fiveHour.RemainingFraction
	if fiveHour.ResetAt.IsZero() {
		state.FiveHour.ResetAt = ""
	} else {
		state.FiveHour.ResetAt = fiveHour.ResetAt.UTC().Format(time.RFC3339)
	}

	state.Weekly.Started = weekly.Exists
	state.Weekly.RemainingFraction = weekly.RemainingFraction
	if weekly.ResetAt.IsZero() {
		state.Weekly.ResetAt = ""
	} else {
		state.Weekly.ResetAt = weekly.ResetAt.UTC().Format(time.RFC3339)
	}

	StoreCodexQuotaSmartState(auth, state)
}

func pruneCodexSmartLocalEvents(events []int64, now time.Time) []int64 {
	if len(events) == 0 {
		return nil
	}
	now = now.UTC()
	cutoff := now.Add(-codexSmartFiveHourWindow).Unix()
	pruned := make([]int64, 0, len(events))
	for _, ts := range events {
		if ts <= 0 {
			continue
		}
		if ts < cutoff {
			continue
		}
		pruned = append(pruned, ts)
	}
	if len(pruned) == 0 {
		return nil
	}
	slices.Sort(pruned)
	return pruned
}

func codexQuotaSmartShouldTrack(auth *Auth) bool {
	if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
		return false
	}
	if auth.Disabled {
		return false
	}
	if auth.Attributes != nil {
		if apiKey := strings.TrimSpace(auth.Attributes["api_key"]); apiKey != "" {
			return false
		}
	}
	return hasCodexQuotaAccountID(auth)
}

func codexQuotaSmartShouldPrewarm(auth *Auth, now time.Time) bool {
	if !codexQuotaSmartShouldTrack(auth) {
		return false
	}
	state, _ := ReadCodexQuotaSmartState(auth, now)
	if state.Weekly.Started {
		return false
	}
	if value, ok := codexQuotaSmartRemainingValue(state.Weekly.RemainingFraction); ok && value < codexSmartWeeklyFullThreshold {
		return false
	}
	if value, ok := codexQuotaSmartRemainingValue(state.FiveHour.RemainingFraction); ok && value < codexSmartFiveHourThreshold {
		return false
	}
	lastPrewarmAt, _ := parseTimeValue(state.LastPrewarmAt)
	if !lastPrewarmAt.IsZero() && now.Sub(lastPrewarmAt) < codexSmartPrewarmCooldown {
		return false
	}
	return true
}

func codexQuotaSmartRecordSuccess(auth *Auth, now time.Time, prewarm bool) bool {
	if !codexQuotaSmartShouldTrack(auth) {
		return false
	}
	state, _ := ReadCodexQuotaSmartState(auth, now)
	state.Local5HEvents = append(state.Local5HEvents, now.UTC().Unix())
	state.Local5HEvents = pruneCodexSmartLocalEvents(state.Local5HEvents, now)
	if prewarm {
		state.LastPrewarmAt = now.UTC().Format(time.RFC3339)
	}
	StoreCodexQuotaSmartState(auth, state)
	return true
}

func codexQuotaSmartCandidateState(entry *scheduledAuth, now time.Time) codexQuotaSmartCandidate {
	candidate := codexQuotaSmartCandidate{entry: entry}
	if entry == nil || entry.auth == nil {
		return candidate
	}
	state, _ := ReadCodexQuotaSmartState(entry.auth, now)
	candidate.state = state
	candidate.planType = state.PlanType
	candidate.planWeight = codexQuotaSmartPlanWeight(candidate.planType)
	candidate.localEventCount = len(state.Local5HEvents)
	candidate.prewarmEligible = codexQuotaSmartShouldPrewarm(entry.auth, now)
	candidate.snapshotAt, _ = parseTimeValue(state.SnapshotAt)
	candidate.lastPrewarmAt, _ = parseTimeValue(state.LastPrewarmAt)
	candidate.weeklyResetAt, _ = parseTimeValue(state.Weekly.ResetAt)
	candidate.fiveHourResetAt, _ = parseTimeValue(state.FiveHour.ResetAt)
	candidate.fiveHourRemaining, candidate.hasFiveHour = codexQuotaSmartRemainingValue(state.FiveHour.RemainingFraction)
	candidate.weeklyUrgency, candidate.hasWeeklyUrgency = codexQuotaSmartWeeklyUrgency(state, candidate.planWeight, now)
	return candidate
}

func pickCodexQuotaSmartReady(entries []*scheduledAuth, model string, cursor *int, predicate func(*scheduledAuth) bool) *scheduledAuth {
	_ = model
	if len(entries) == 0 {
		return nil
	}
	now := time.Now().UTC()
	candidates := make([]codexQuotaSmartCandidate, 0, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.auth == nil {
			continue
		}
		if predicate != nil && !predicate(entry) {
			continue
		}
		candidates = append(candidates, codexQuotaSmartCandidateState(entry, now))
	}
	if len(candidates) == 0 {
		return nil
	}

	if !strings.EqualFold(strings.TrimSpace(candidates[0].entry.auth.Provider), "codex") {
		return pickCodexQuotaSmartRoundRobin(candidates, cursor)
	}

	prewarm := make([]codexQuotaSmartCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.prewarmEligible {
			prewarm = append(prewarm, candidate)
		}
	}
	if len(prewarm) > 0 {
		slices.SortFunc(prewarm, compareCodexSmartPrewarmCandidate)
		return prewarm[0].entry
	}

	started := make([]codexQuotaSmartCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.state.Weekly.Started {
			started = append(started, candidate)
		}
	}
	if len(started) == 0 {
		return pickCodexQuotaSmartRoundRobin(candidates, cursor)
	}

	slices.SortFunc(started, compareCodexSmartWeeklyCandidate)
	topUrgency := started[0].weeklyUrgency
	threshold := topUrgency * codexSmartCandidatePoolRatio
	// 5-hour data is a local smoothing signal inside the weekly-urgent pool, not
	// a hard exclusion rule. Actual unavailability still comes from cooldown state.
	pool := make([]codexQuotaSmartCandidate, 0, min(codexSmartCandidatePoolMax, len(started)))
	for _, candidate := range started {
		if len(pool) >= codexSmartCandidatePoolMax {
			break
		}
		if len(pool) < codexSmartCandidatePoolMin || candidate.weeklyUrgency >= threshold {
			pool = append(pool, candidate)
		}
	}
	if len(pool) == 0 {
		return started[0].entry
	}

	best := pool[0]
	ties := []codexQuotaSmartCandidate{best}
	for i := 1; i < len(pool); i++ {
		candidate := pool[i]
		comparison := compareCodexSmartLocalSmooth(candidate, best)
		if comparison < 0 {
			best = candidate
			ties = []codexQuotaSmartCandidate{candidate}
			continue
		}
		if comparison == 0 {
			ties = append(ties, candidate)
		}
	}
	if len(ties) == 1 {
		return best.entry
	}
	return pickCodexQuotaSmartRoundRobin(ties, cursor)
}

func pickCodexQuotaSmartRoundRobin(candidates []codexQuotaSmartCandidate, cursor *int) *scheduledAuth {
	if len(candidates) == 0 {
		return nil
	}
	start := 0
	if cursor != nil && len(candidates) > 0 {
		start = *cursor % len(candidates)
	}
	for offset := 0; offset < len(candidates); offset++ {
		index := (start + offset) % len(candidates)
		entry := candidates[index].entry
		if entry == nil || entry.auth == nil {
			continue
		}
		if cursor != nil {
			*cursor = index + 1
		}
		return entry
	}
	return nil
}

func compareCodexSmartPrewarmCandidate(left, right codexQuotaSmartCandidate) int {
	if cmp := compareTimeAscending(left.lastPrewarmAt, right.lastPrewarmAt); cmp != 0 {
		return cmp
	}
	if cmp := compareTimeAscending(left.snapshotAt, right.snapshotAt); cmp != 0 {
		return cmp
	}
	return strings.Compare(left.entry.auth.ID, right.entry.auth.ID)
}

func compareCodexSmartWeeklyCandidate(left, right codexQuotaSmartCandidate) int {
	if cmp := compareFloatDesc(left.weeklyUrgency, right.weeklyUrgency); cmp != 0 {
		return cmp
	}
	if cmp := compareTimeAscending(left.weeklyResetAt, right.weeklyResetAt); cmp != 0 {
		return cmp
	}
	return strings.Compare(left.entry.auth.ID, right.entry.auth.ID)
}

func compareCodexSmartLocalSmooth(left, right codexQuotaSmartCandidate) int {
	if cmp := compareOptionalFloatDesc(left.fiveHourRemaining, left.hasFiveHour, right.fiveHourRemaining, right.hasFiveHour); cmp != 0 {
		return cmp
	}
	if left.localEventCount != right.localEventCount {
		if left.localEventCount < right.localEventCount {
			return -1
		}
		return 1
	}
	return strings.Compare(left.entry.auth.ID, right.entry.auth.ID)
}

func compareOptionalFloatDesc(left float64, leftOK bool, right float64, rightOK bool) int {
	if leftOK != rightOK {
		if leftOK {
			return -1
		}
		return 1
	}
	if !leftOK {
		return 0
	}
	return compareFloatDesc(left, right)
}

func compareFloatDesc(left, right float64) int {
	switch {
	case left > right:
		return -1
	case left < right:
		return 1
	default:
		return 0
	}
}

func compareTimeAscending(left, right time.Time) int {
	if left.IsZero() != right.IsZero() {
		if left.IsZero() {
			return -1
		}
		return 1
	}
	switch {
	case left.Before(right):
		return -1
	case left.After(right):
		return 1
	default:
		return 0
	}
}

func codexQuotaSmartRemainingValue(value *float64) (float64, bool) {
	if value == nil {
		return 0, false
	}
	return *value, true
}

func codexQuotaSmartWeeklyUrgency(state codexQuotaSmartState, planWeight float64, now time.Time) (float64, bool) {
	if !state.Weekly.Started {
		return 0, false
	}
	remaining, ok := codexQuotaSmartRemainingValue(state.Weekly.RemainingFraction)
	if !ok {
		return 0, false
	}
	resetAt, ok := parseTimeValue(state.Weekly.ResetAt)
	if !ok || resetAt.IsZero() {
		return 0, false
	}
	hours := resetAt.Sub(now).Hours()
	if hours <= 0 {
		hours = 1.0 / 60.0
	}
	if planWeight <= 0 {
		planWeight = codexSmartDefaultPlanWeight
	}
	return remaining * planWeight / hours, true
}

func codexQuotaSmartResolvePlanType(auth *Auth, state codexQuotaSmartState) string {
	if planType := codexQuotaSmartNormalizePlanType(state.PlanType); planType != "" {
		return planType
	}
	if auth == nil {
		return ""
	}
	if auth.Attributes != nil {
		if planType := codexQuotaSmartNormalizePlanType(auth.Attributes["plan_type"]); planType != "" {
			return planType
		}
	}
	if auth.Metadata != nil {
		if planType := codexQuotaSmartNormalizePlanType(stringFromMetadataMap(auth.Metadata, "plan_type", "planType")); planType != "" {
			return planType
		}
	}
	return ""
}

func codexQuotaSmartPlanWeight(planType string) float64 {
	switch codexQuotaSmartNormalizePlanType(planType) {
	case "team", "plus", "pro":
		return codexSmartPaidPlanWeight
	default:
		return codexSmartDefaultPlanWeight
	}
}

func codexQuotaSmartNormalizePlanType(raw string) string {
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

func stringFromMetadataMap(metadata map[string]any, keys ...string) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func extractCodexQuotaSmartSnapshot(payload []byte, now time.Time) (codexQuotaSmartWindowSnapshot, codexQuotaSmartWindowSnapshot, string) {
	if len(payload) == 0 {
		return codexQuotaSmartWindowSnapshot{}, codexQuotaSmartWindowSnapshot{}, ""
	}
	root := gjson.ParseBytes(payload)
	planType := strings.ToLower(strings.TrimSpace(firstExistingCodexSmartResult(root, "plan_type", "planType").String()))

	rateLimit := firstExistingCodexSmartResult(root, "rate_limit", "rateLimit")
	if !rateLimit.Exists() || rateLimit.Type == gjson.Null {
		return codexQuotaSmartWindowSnapshot{}, codexQuotaSmartWindowSnapshot{}, planType
	}
	primaryWindow := firstExistingCodexSmartResult(rateLimit, "primary_window", "primaryWindow")
	secondaryWindow := firstExistingCodexSmartResult(rateLimit, "secondary_window", "secondaryWindow")
	fiveHourWindow, weeklyWindow := classifyCodexSmartWindows(primaryWindow, secondaryWindow)
	return parseCodexSmartWindow(fiveHourWindow, now), parseCodexSmartWindow(weeklyWindow, now), planType
}

func classifyCodexSmartWindows(primaryWindow, secondaryWindow gjson.Result) (gjson.Result, gjson.Result) {
	const (
		fiveHourSeconds = 18000
		weekSeconds     = 604800
	)

	windows := []gjson.Result{primaryWindow, secondaryWindow}
	fiveHour := gjson.Result{}
	weekly := gjson.Result{}
	for _, window := range windows {
		if !window.Exists() || window.Type == gjson.Null {
			continue
		}
		seconds := normalizeCodexSmartWindowSeconds(window)
		switch seconds {
		case fiveHourSeconds:
			if !fiveHour.Exists() {
				fiveHour = window
			}
		case weekSeconds:
			if !weekly.Exists() {
				weekly = window
			}
		}
	}

	if !fiveHour.Exists() && primaryWindow.Exists() && primaryWindow.Type != gjson.Null && !sameCodexSmartWindow(primaryWindow, weekly) {
		fiveHour = primaryWindow
	}
	if !weekly.Exists() && secondaryWindow.Exists() && secondaryWindow.Type != gjson.Null && !sameCodexSmartWindow(secondaryWindow, fiveHour) {
		weekly = secondaryWindow
	}
	return fiveHour, weekly
}

func sameCodexSmartWindow(left, right gjson.Result) bool {
	if !left.Exists() || !right.Exists() {
		return false
	}
	return left.Raw == right.Raw
}

func normalizeCodexSmartWindowSeconds(window gjson.Result) int64 {
	seconds := firstExistingCodexSmartResult(window, "limit_window_seconds", "limitWindowSeconds")
	if !seconds.Exists() || seconds.Type == gjson.Null {
		return 0
	}
	if seconds.Type == gjson.Number {
		return seconds.Int()
	}
	text := strings.TrimSpace(seconds.String())
	if text == "" {
		return 0
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func parseCodexSmartWindow(window gjson.Result, now time.Time) codexQuotaSmartWindowSnapshot {
	if !window.Exists() || window.Type == gjson.Null {
		return codexQuotaSmartWindowSnapshot{}
	}
	snapshot := codexQuotaSmartWindowSnapshot{Exists: true}
	if remaining, ok := codexSmartRemainingFraction(window); ok {
		snapshot.RemainingFraction = &remaining
	}
	if resetAt, ok := extractCodexSmartWindowResetAt(window, now); ok {
		snapshot.ResetAt = resetAt.UTC()
	}
	return snapshot
}

func codexSmartRemainingFraction(window gjson.Result) (float64, bool) {
	usedPercent := firstExistingCodexSmartResult(window, "used_percent", "usedPercent")
	if !usedPercent.Exists() || usedPercent.Type == gjson.Null {
		return 0, false
	}
	var used float64
	switch usedPercent.Type {
	case gjson.Number:
		used = usedPercent.Float()
	default:
		value, err := strconv.ParseFloat(strings.TrimSpace(usedPercent.String()), 64)
		if err != nil {
			return 0, false
		}
		used = value
	}
	remaining := 1 - used/100
	remaining = math.Max(0, math.Min(1, remaining))
	return remaining, true
}

func firstExistingCodexSmartResult(parent gjson.Result, paths ...string) gjson.Result {
	for _, path := range paths {
		result := parent.Get(path)
		if result.Exists() {
			return result
		}
	}
	return gjson.Result{}
}

func extractCodexSmartWindowResetAt(window gjson.Result, now time.Time) (time.Time, bool) {
	resetAt := firstExistingCodexSmartResult(window, "reset_at", "resetAt")
	if resetAt.Exists() {
		if unix := resetAt.Int(); unix > 0 {
			return time.Unix(unix, 0).UTC(), true
		}
	}

	resetAfter := firstExistingCodexSmartResult(window, "reset_after_seconds", "resetAfterSeconds")
	if resetAfter.Exists() {
		if seconds := resetAfter.Int(); seconds > 0 {
			return now.UTC().Add(time.Duration(seconds) * time.Second), true
		}
	}
	return time.Time{}, false
}
