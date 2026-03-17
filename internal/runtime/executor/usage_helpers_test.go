package executor

import "testing"

func TestParseOpenAIUsageChatCompletions(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":5}}}`)
	detail := parseOpenAIUsage(data)
	if detail.InputTokens != 1 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 1)
	}
	if detail.OutputTokens != 2 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 2)
	}
	if detail.TotalTokens != 3 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 3)
	}
	if detail.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 4)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 5)
	}
}

func TestParseOpenAIUsageResponses(t *testing.T) {
	data := []byte(`{"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"input_tokens_details":{"cached_tokens":7},"output_tokens_details":{"reasoning_tokens":9}}}`)
	detail := parseOpenAIUsage(data)
	if detail.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 10)
	}
	if detail.OutputTokens != 20 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 20)
	}
	if detail.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 30)
	}
	if detail.CachedTokens != 7 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 7)
	}
	if detail.ReasoningTokens != 9 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 9)
	}
}

func TestParseCodexUsageIncludesServiceTier(t *testing.T) {
	data := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30},"service_tier":"priority"}}`)
	detail, ok := parseCodexUsage(data)
	if !ok {
		t.Fatal("expected codex usage to be parsed")
	}
	if detail.ServiceTier != "priority" {
		t.Fatalf("service tier = %q, want %q", detail.ServiceTier, "priority")
	}
}

func TestParseServiceTierFallbackPaths(t *testing.T) {
	if got := parseServiceTier([]byte(`{"service_tier":"priority"}`), "response.service_tier", "service_tier"); got != "priority" {
		t.Fatalf("service tier = %q, want %q", got, "priority")
	}
}

func TestRequestedFastModeFromPayload(t *testing.T) {
	if !requestedFastModeFromPayload([]byte(`{"service_tier":"priority"}`)) {
		t.Fatal("expected requested fast mode to be true")
	}
	if requestedFastModeFromPayload([]byte(`{"service_tier":"default"}`)) {
		t.Fatal("expected requested fast mode to be false")
	}
}
