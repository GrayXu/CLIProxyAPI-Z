package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

func TestBuildStickyConversationKey_OpenAIChatUsesFirstUserMessage(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-5.2",
		"messages":[
			{"role":"system","content":"You are helpful."},
			{"role":"user","content":"  hello   world  "},
			{"role":"user","content":"second message"}
		]
	}`)
	normalizedWhitespace := []byte(`{
		"model":"gpt-5.2",
		"messages":[
			{"role":"user","content":"hello world"}
		]
	}`)

	first := buildStickyConversationKey("openai", "gpt-5.2(high)", raw)
	second := buildStickyConversationKey("openai", "gpt-5.2(low)", normalizedWhitespace)
	if first == "" {
		t.Fatal("expected non-empty sticky conversation key")
	}
	if first != second {
		t.Fatalf("conversation key mismatch: %q != %q", first, second)
	}
}

func TestBuildStickyConversationKey_OpenAIResponsesUsesFirstUserInput(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-5.2",
		"input":[
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"previous"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"first prompt"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"second prompt"}]}
		]
	}`)

	key := buildStickyConversationKey("openai-response", "gpt-5.2", raw)
	if key == "" {
		t.Fatal("expected non-empty sticky conversation key")
	}

	other := buildStickyConversationKey("openai-response", "gpt-5.2", []byte(`{
		"model":"gpt-5.2",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"different prompt"}]}]
	}`))
	if key == other {
		t.Fatalf("expected different conversation key for different first prompt")
	}
}

func TestBuildStickyConversationKey_ReturnsEmptyWithoutUserText(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"gpt-5.2","messages":[{"role":"assistant","content":"hi"}]}`)
	if key := buildStickyConversationKey("openai", "gpt-5.2", raw); key != "" {
		t.Fatalf("expected empty key, got %q", key)
	}
}

func TestRequestExecutionMetadata_PrefersExplicitStickyConversationHeader(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(stickyConversationHeader, "session-key-1")
	c.Request = req

	ctx := context.WithValue(context.Background(), "gin", c)
	meta := requestExecutionMetadata(ctx)
	got, _ := meta[coreexecutor.StickyConversationMetadataKey].(string)
	if got != "session-key-1" {
		t.Fatalf("sticky conversation key = %q, want %q", got, "session-key-1")
	}
}
