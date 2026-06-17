package auth

import (
	"testing"
	"time"
)

func TestCodexQuotaSmartPlanWeight(t *testing.T) {
	cases := []struct {
		name     string
		planType string
		want     float64
	}{
		{name: "free", planType: "free", want: codexSmartDefaultPlanWeight},
		{name: "team", planType: "team", want: codexSmartPaidPlanWeight},
		{name: "plus", planType: "plus", want: codexSmartPaidPlanWeight},
		{name: "pro", planType: "plan_pro", want: codexSmartPaidPlanWeight},
		{name: "unknown", planType: "enterprise", want: codexSmartDefaultPlanWeight},
		{name: "empty", planType: "", want: codexSmartDefaultPlanWeight},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := codexQuotaSmartPlanWeight(tc.planType); got != tc.want {
				t.Fatalf("codexQuotaSmartPlanWeight(%q) = %v, want %v", tc.planType, got, tc.want)
			}
		})
	}
}

func TestCodexQuotaSmartResolvePlanTypePrefersStateThenAuthFallback(t *testing.T) {
	auth := &Auth{
		Attributes: map[string]string{
			"plan_type": "team",
		},
		Metadata: map[string]any{
			"plan_type": "free",
		},
	}

	if got := codexQuotaSmartResolvePlanType(auth, codexQuotaSmartState{PlanType: "plan_pro"}); got != "pro" {
		t.Fatalf("plan type from state = %q, want %q", got, "pro")
	}
	if got := codexQuotaSmartResolvePlanType(auth, codexQuotaSmartState{}); got != "team" {
		t.Fatalf("plan type from auth fallback = %q, want %q", got, "team")
	}
	if got := codexQuotaSmartResolvePlanType(&Auth{Metadata: map[string]any{"plan_type": "plan_plus"}}, codexQuotaSmartState{}); got != "plus" {
		t.Fatalf("plan type from metadata fallback = %q, want %q", got, "plus")
	}
}

func TestCodexQuotaSmartAvailability_StalePositiveSnapshotDoesNotBlock(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	auth := &Auth{
		ID:       "codex-stale-positive",
		Provider: "codex",
		Metadata: map[string]any{"account_id": "acct-stale-positive"},
	}
	StoreCodexQuotaSmartState(auth, codexQuotaSmartState{
		SnapshotAt: now.Add(-2 * codexQuotaSnapshotRefreshInterval).Format(time.RFC3339),
		Weekly: codexQuotaSmartWeeklyWindow{
			Started: true,
			codexQuotaSmartWindow: codexQuotaSmartWindow{
				RemainingFraction: floatPtr(0.4),
				ResetAt:           now.Add(time.Hour).Format(time.RFC3339),
			},
		},
		FiveHour: codexQuotaSmartWindow{
			RemainingFraction: floatPtr(0.5),
			ResetAt:           now.Add(30 * time.Minute).Format(time.RFC3339),
		},
	})

	blocked, reason, next := codexQuotaSmartBlockedForQuotaSmart(auth, now)
	if blocked {
		t.Fatalf("blocked = true, want false (reason=%v next=%v)", reason, next)
	}
	if !codexQuotaSmartSnapshotNeedsRefresh(auth, now) {
		t.Fatalf("snapshot needs refresh = false, want true")
	}
}

func TestCodexQuotaSmartAvailability_StaleExhaustedSnapshotBlocks(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	auth := &Auth{
		ID:       "codex-stale-exhausted",
		Provider: "codex",
		Metadata: map[string]any{"account_id": "acct-stale-exhausted"},
	}
	StoreCodexQuotaSmartState(auth, codexQuotaSmartState{
		SnapshotAt: now.Add(-2 * codexQuotaSnapshotRefreshInterval).Format(time.RFC3339),
		Weekly: codexQuotaSmartWeeklyWindow{
			Started: true,
			codexQuotaSmartWindow: codexQuotaSmartWindow{
				RemainingFraction: floatPtr(0),
				ResetAt:           now.Add(time.Hour).Format(time.RFC3339),
			},
		},
	})

	blocked, reason, next := codexQuotaSmartBlockedForQuotaSmart(auth, now)
	if !blocked {
		t.Fatal("blocked = false, want true")
	}
	if reason != blockReasonCooldown {
		t.Fatalf("reason = %v, want %v", reason, blockReasonCooldown)
	}
	if next.IsZero() || !next.After(now) {
		t.Fatalf("next = %v, want future reset", next)
	}
}

func TestCodexQuotaSmartAvailability_MissingSnapshotBlocksTrackableCodexOAuth(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	auth := &Auth{
		ID:       "codex-missing-snapshot",
		Provider: "codex",
		Metadata: map[string]any{"account_id": "acct-missing-snapshot"},
	}

	blocked, reason, next := codexQuotaSmartBlockedForQuotaSmart(auth, now)
	if !blocked {
		t.Fatal("blocked = false, want true")
	}
	if reason != blockReasonCooldown {
		t.Fatalf("reason = %v, want %v", reason, blockReasonCooldown)
	}
	if next.IsZero() || !next.After(now) {
		t.Fatalf("next = %v, want retry time", next)
	}
}
