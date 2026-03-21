package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	"github.com/tidwall/gjson"
)

func buildStickyConversationKey(handlerType, modelName string, rawJSON []byte) string {
	firstUserText := strings.TrimSpace(extractFirstUserText(handlerType, rawJSON))
	if firstUserText == "" {
		return ""
	}
	normalizedText := normalizeConversationText(firstUserText)
	if normalizedText == "" {
		return ""
	}

	modelKey := strings.TrimSpace(thinking.ParseSuffix(strings.TrimSpace(modelName)).ModelName)
	if modelKey == "" {
		modelKey = strings.TrimSpace(modelName)
	}
	sum := sha256.Sum256([]byte(normalizedText))
	return strings.TrimSpace(handlerType) + ":" + modelKey + ":" + hex.EncodeToString(sum[:])
}

func extractFirstUserText(handlerType string, rawJSON []byte) string {
	switch strings.TrimSpace(handlerType) {
	case "openai-response":
		if text := extractFirstUserTextFromResponses(rawJSON); text != "" {
			return text
		}
		return extractFirstUserTextFromMessages(rawJSON)
	case "openai", "claude":
		return extractFirstUserTextFromMessages(rawJSON)
	case "gemini", "gemini-cli":
		return extractFirstUserTextFromGeminiContents(rawJSON)
	default:
		if text := extractFirstUserTextFromMessages(rawJSON); text != "" {
			return text
		}
		if text := extractFirstUserTextFromResponses(rawJSON); text != "" {
			return text
		}
		return extractFirstUserTextFromGeminiContents(rawJSON)
	}
}

func extractFirstUserTextFromMessages(rawJSON []byte) string {
	messages := gjson.GetBytes(rawJSON, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return ""
	}
	var first string
	messages.ForEach(func(_, message gjson.Result) bool {
		role := strings.TrimSpace(strings.ToLower(message.Get("role").String()))
		if role != "user" {
			return true
		}
		first = extractTextContent(message.Get("content"))
		return strings.TrimSpace(first) == ""
	})
	return first
}

func extractFirstUserTextFromResponses(rawJSON []byte) string {
	input := gjson.GetBytes(rawJSON, "input")
	if input.Type == gjson.String {
		return input.String()
	}
	if !input.Exists() || !input.IsArray() {
		return ""
	}
	var first string
	input.ForEach(func(_, item gjson.Result) bool {
		itemRole := strings.TrimSpace(strings.ToLower(item.Get("role").String()))
		itemType := strings.TrimSpace(strings.ToLower(item.Get("type").String()))
		if itemRole != "user" && !(itemType == "message" && itemRole == "user") {
			return true
		}
		first = extractTextContent(item.Get("content"))
		return strings.TrimSpace(first) == ""
	})
	return first
}

func extractFirstUserTextFromGeminiContents(rawJSON []byte) string {
	contents := gjson.GetBytes(rawJSON, "contents")
	if !contents.Exists() || !contents.IsArray() {
		return ""
	}
	var first string
	contents.ForEach(func(_, content gjson.Result) bool {
		role := strings.TrimSpace(strings.ToLower(content.Get("role").String()))
		if role != "user" {
			return true
		}
		first = extractPartsText(content.Get("parts"))
		return strings.TrimSpace(first) == ""
	})
	return first
}

func extractTextContent(content gjson.Result) string {
	if !content.Exists() {
		return ""
	}
	if content.Type == gjson.String {
		return content.String()
	}
	if content.IsArray() {
		parts := make([]string, 0, len(content.Array()))
		content.ForEach(func(_, item gjson.Result) bool {
			if text := extractTextLikeValue(item); text != "" {
				parts = append(parts, text)
			}
			return true
		})
		return strings.Join(parts, "\n")
	}
	if content.IsObject() {
		return extractTextLikeValue(content)
	}
	return ""
}

func extractPartsText(parts gjson.Result) string {
	if !parts.Exists() || !parts.IsArray() {
		return ""
	}
	values := make([]string, 0, len(parts.Array()))
	parts.ForEach(func(_, part gjson.Result) bool {
		if text := extractTextLikeValue(part); text != "" {
			values = append(values, text)
		}
		return true
	})
	return strings.Join(values, "\n")
}

func extractTextLikeValue(value gjson.Result) string {
	if !value.Exists() {
		return ""
	}
	if value.Type == gjson.String {
		return value.String()
	}
	for _, path := range []string{"text", "content", "input_text", "output_text"} {
		if text := strings.TrimSpace(value.Get(path).String()); text != "" {
			return text
		}
	}
	return ""
}

func normalizeConversationText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return strings.Join(strings.Fields(text), " ")
}
