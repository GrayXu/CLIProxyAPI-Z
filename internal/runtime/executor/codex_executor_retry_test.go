package executor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestParseCodexRetryAfter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	t.Run("resets_in_seconds", func(t *testing.T) {
		body := []byte(`{"error":{"type":"usage_limit_reached","resets_in_seconds":123}}`)
		retryAfter := parseCodexRetryAfter(http.StatusTooManyRequests, body, now)
		if retryAfter == nil {
			t.Fatalf("expected retryAfter, got nil")
		}
		if *retryAfter != 123*time.Second {
			t.Fatalf("retryAfter = %v, want %v", *retryAfter, 123*time.Second)
		}
	})

	t.Run("prefers resets_at", func(t *testing.T) {
		resetAt := now.Add(5 * time.Minute).Unix()
		body := []byte(`{"error":{"type":"usage_limit_reached","resets_at":` + itoa(resetAt) + `,"resets_in_seconds":1}}`)
		retryAfter := parseCodexRetryAfter(http.StatusTooManyRequests, body, now)
		if retryAfter == nil {
			t.Fatalf("expected retryAfter, got nil")
		}
		if *retryAfter != 5*time.Minute {
			t.Fatalf("retryAfter = %v, want %v", *retryAfter, 5*time.Minute)
		}
	})

	t.Run("fallback when resets_at is past", func(t *testing.T) {
		resetAt := now.Add(-1 * time.Minute).Unix()
		body := []byte(`{"error":{"type":"usage_limit_reached","resets_at":` + itoa(resetAt) + `,"resets_in_seconds":77}}`)
		retryAfter := parseCodexRetryAfter(http.StatusTooManyRequests, body, now)
		if retryAfter == nil {
			t.Fatalf("expected retryAfter, got nil")
		}
		if *retryAfter != 77*time.Second {
			t.Fatalf("retryAfter = %v, want %v", *retryAfter, 77*time.Second)
		}
	})

	t.Run("non-429 status code", func(t *testing.T) {
		body := []byte(`{"error":{"type":"usage_limit_reached","resets_in_seconds":30}}`)
		if got := parseCodexRetryAfter(http.StatusBadRequest, body, now); got != nil {
			t.Fatalf("expected nil for non-429, got %v", *got)
		}
	})

	t.Run("non usage_limit_reached error type", func(t *testing.T) {
		body := []byte(`{"error":{"type":"server_error","resets_in_seconds":30}}`)
		if got := parseCodexRetryAfter(http.StatusTooManyRequests, body, now); got != nil {
			t.Fatalf("expected nil for non-usage_limit_reached, got %v", *got)
		}
	})
}

func TestNewCodexStatusErrTreatsCapacityAsRetryableRateLimit(t *testing.T) {
	body := []byte(`{"error":{"message":"Selected model is at capacity. Please try a different model."}}`)

	err := newCodexStatusErr(http.StatusBadRequest, body)

	if got := err.StatusCode(); got != http.StatusTooManyRequests {
		t.Fatalf("status code = %d, want %d", got, http.StatusTooManyRequests)
	}
	if err.RetryAfter() != nil {
		t.Fatalf("expected nil explicit retryAfter for capacity fallback, got %v", *err.RetryAfter())
	}
}

func TestExtractCodexWeeklyResetAt(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	t.Run("prefers weekly window by duration", func(t *testing.T) {
		resetAt := now.Add(3 * time.Hour).Unix()
		body := []byte(`{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"reset_at":1},"secondary_window":{"limit_window_seconds":604800,"reset_at":` + itoa(resetAt) + `}}}`)
		got, ok := extractCodexWeeklyResetAt(body, now)
		if !ok {
			t.Fatalf("expected weekly reset, got none")
		}
		if !got.Equal(time.Unix(resetAt, 0).UTC()) {
			t.Fatalf("weekly reset = %v, want %v", got, time.Unix(resetAt, 0).UTC())
		}
	})

	t.Run("falls back to secondary window for legacy payload", func(t *testing.T) {
		resetAt := now.Add(2 * time.Hour).Unix()
		body := []byte(`{"rate_limit":{"primary_window":{"reset_at":1},"secondary_window":{"reset_at":` + itoa(resetAt) + `}}}`)
		got, ok := extractCodexWeeklyResetAt(body, now)
		if !ok {
			t.Fatalf("expected weekly reset, got none")
		}
		if !got.Equal(time.Unix(resetAt, 0).UTC()) {
			t.Fatalf("weekly reset = %v, want %v", got, time.Unix(resetAt, 0).UTC())
		}
	})

	t.Run("uses reset_after_seconds when reset_at missing", func(t *testing.T) {
		body := []byte(`{"rate_limit":{"secondary_window":{"limit_window_seconds":604800,"reset_after_seconds":600}}}`)
		got, ok := extractCodexWeeklyResetAt(body, now)
		if !ok {
			t.Fatalf("expected weekly reset, got none")
		}
		want := now.Add(10 * time.Minute)
		if !got.Equal(want) {
			t.Fatalf("weekly reset = %v, want %v", got, want)
		}
	})

	t.Run("ignores code review weekly window", func(t *testing.T) {
		body := []byte(`{"code_review_rate_limit":{"secondary_window":{"limit_window_seconds":604800,"reset_after_seconds":600}}}`)
		if got, ok := extractCodexWeeklyResetAt(body, now); ok {
			t.Fatalf("expected no weekly reset, got %v", got)
		}
	})
}

func TestRefreshQuotaSnapshot_PreservesExistingWeeklyResetWithoutRecognizableWeeklyWindow(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	existingReset := now.Add(2 * time.Hour).UTC()
	auth := &cliproxyauth.Auth{
		ID: "codex-auth",
		Metadata: map[string]any{
			"access_token":            "token",
			"account_id":              "account",
			"routing_weekly_reset_at": existingReset.Format(time.RFC3339),
		},
	}

	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"rate_limit":{"daily_window":{"limit_window_seconds":86400,"reset_after_seconds":30}}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}))

	exec := NewCodexExecutor(nil)
	exec.refreshQuotaSnapshot(ctx, auth, now)

	if got := auth.Metadata["routing_weekly_reset_at"]; got != existingReset.Format(time.RFC3339) {
		t.Fatalf("routing_weekly_reset_at = %v, want %v", got, existingReset.Format(time.RFC3339))
	}
	if got := auth.Metadata["routing_weekly_snapshot_at"]; got != now.Format(time.RFC3339) {
		t.Fatalf("routing_weekly_snapshot_at = %v, want %v", got, now.Format(time.RFC3339))
	}
	if got := auth.Metadata["codex_quota_snapshot"]; got != `{"rate_limit":{"daily_window":{"limit_window_seconds":86400,"reset_after_seconds":30}}}` {
		t.Fatalf("codex_quota_snapshot = %v, want payload", got)
	}
}

func TestRefreshQuotaSnapshot_PreservesExistingWeeklyResetOnFetchError(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	existingReset := now.Add(2 * time.Hour).UTC()
	existingSnapshot := now.Add(-20 * time.Minute).UTC()
	auth := &cliproxyauth.Auth{
		ID: "codex-auth",
		Metadata: map[string]any{
			"access_token":               "token",
			"account_id":                 "account",
			"routing_weekly_reset_at":    existingReset.Format(time.RFC3339),
			"routing_weekly_snapshot_at": existingSnapshot.Format(time.RFC3339),
		},
	}

	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	}))

	exec := NewCodexExecutor(nil)
	exec.refreshQuotaSnapshot(ctx, auth, now)

	if got := auth.Metadata["routing_weekly_reset_at"]; got != existingReset.Format(time.RFC3339) {
		t.Fatalf("routing_weekly_reset_at = %v, want %v", got, existingReset.Format(time.RFC3339))
	}
	if got := auth.Metadata["routing_weekly_snapshot_at"]; got != existingSnapshot.Format(time.RFC3339) {
		t.Fatalf("routing_weekly_snapshot_at = %v, want %v", got, existingSnapshot.Format(time.RFC3339))
	}
	if _, ok := auth.Metadata["codex_quota_snapshot"]; ok {
		t.Fatalf("codex_quota_snapshot unexpectedly set")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
