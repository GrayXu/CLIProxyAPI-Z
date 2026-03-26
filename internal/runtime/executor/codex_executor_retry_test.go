package executor

import (
	"net/http"
	"strconv"
	"testing"
	"time"
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

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
