package auth

import "testing"

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
