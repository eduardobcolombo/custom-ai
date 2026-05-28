package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

// =============================================================================
// Configuration and State
// =============================================================================

// config holds the configuration options parsed from Bifrost.
type config struct {
	OpaURL string `json:"opa_url"`
}

// pluginState wraps the global configuration and runtime state of the plugin.
// It is protected by a read-write mutex to allow concurrent safe access.
type pluginState struct {
	mu     sync.RWMutex
	opaURL string
}

var state pluginState

// =============================================================================
// PII Masking Engine
// =============================================================================

var (
	// reSSN matches standard Social Security Numbers (e.g. 123-45-6789).
	reSSN = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)

	// reGenericSSN matches continuous digit structures that might represent SSNs.
	reGenericSSN = regexp.MustCompile(`\b\d{6,9}\b`)

	// reEmail matches standard email address formats.
	reEmail = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`)
)

const (
	maskedVisualToken = "************"
)

// maskPII replaces sensitive information with a visual mask (e.g. ************)
func maskPII(input string) string {
	input = reEmail.ReplaceAllString(input, maskedVisualToken)
	input = reSSN.ReplaceAllString(input, maskedVisualToken)
	input = reGenericSSN.ReplaceAllString(input, maskedVisualToken)
	return input
}

// =============================================================================
// Stream State Management
// =============================================================================

type piiContextKey string

const (
	streamStateKey piiContextKey = "pii-stream-state"
)

type bufferKey struct {
	choiceIdx int
	fieldType string
}

// streamState manages chunk accumulation buffers for streaming responses.
type streamState struct {
	mu      sync.Mutex
	buffers map[bufferKey]string
}

// isPIIChar returns true if the character can potentially be part of an SSN or Email.
func isPIIChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '-' || r == '@' || r == '.' || r == '_' || r == '%' || r == '+'
}

// processField accumulates chunks and returns the masked text up to the last delimiter.
// If isLast is true, it flushes the entire remaining buffer.
func (s *streamState) processField(ctx *schemas.BifrostContext, choiceIdx int, fieldType string, content string, isLast bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.buffers == nil {
		s.buffers = make(map[bufferKey]string)
	}

	key := bufferKey{choiceIdx: choiceIdx, fieldType: fieldType}
	buf := s.buffers[key] + content

	if isLast {
		masked := maskPII(buf)
		if masked != buf {
			logPIIInterception(ctx, fmt.Sprintf("PII detected and masked in stream choice %d field %s", choiceIdx, fieldType))
		}
		delete(s.buffers, key)
		return masked
	}

	// Scan from end to locate the last word delimiter
	lastDelimIdx := -1
	runes := []rune(buf)
	for i := len(runes) - 1; i >= 0; i-- {
		if !isPIIChar(runes[i]) {
			lastDelimIdx = i
			break
		}
	}

	if lastDelimIdx != -1 {
		byteIdx := len(string(runes[:lastDelimIdx+1]))
		toFlush := buf[:byteIdx]
		s.buffers[key] = buf[byteIdx:]

		masked := maskPII(toFlush)
		if masked != toFlush {
			logPIIInterception(ctx, fmt.Sprintf("PII detected and masked in stream choice %d field %s", choiceIdx, fieldType))
		}
		return masked
	}

	// Prevent buffer bloat from very long continuous words
	if len(buf) > 1024 {
		masked := maskPII(buf)
		if masked != buf {
			logPIIInterception(ctx, fmt.Sprintf("PII detected and masked in stream choice %d field %s (buffer limit reached)", choiceIdx, fieldType))
		}
		delete(s.buffers, key)
		return masked
	}

	s.buffers[key] = buf
	return ""
}

// =============================================================================
// Helper Logging
// =============================================================================

func logPIIInterception(ctx *schemas.BifrostContext, message string) {
	fmt.Printf("[pii-interceptor] %s\n", message)
	ctx.Log(schemas.LogLevelInfo, fmt.Sprintf("[pii-interceptor] %s", message))
}

// =============================================================================
// OPA Client
// =============================================================================

type opaResponse struct {
	Result struct {
		Allow bool     `json:"allow"`
		Deny  []string `json:"deny"`
	} `json:"result"`
}

func evaluateOPA(prompt string) (bool, string, error) {
	state.mu.RLock()
	opaURL := state.opaURL
	state.mu.RUnlock()

	payload := map[string]interface{}{
		"input": map[string]interface{}{
			"message": prompt,
		},
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return true, "", fmt.Errorf("failed to marshal OPA payload: %w", err)
	}

	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Post(opaURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return true, "", fmt.Errorf("OPA request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return true, "", fmt.Errorf("OPA returned status code %d", resp.StatusCode)
	}

	var opaResp opaResponse
	if err := json.NewDecoder(resp.Body).Decode(&opaResp); err != nil {
		return true, "", fmt.Errorf("failed to decode OPA response: %w", err)
	}

	if !opaResp.Result.Allow {
		reason := "Request blocked by governance policy"
		if len(opaResp.Result.Deny) > 0 {
			reason = opaResp.Result.Deny[0]
		}
		return false, reason, nil
	}

	return true, "", nil
}

// =============================================================================
// Plugin Lifecycle Hooks
// =============================================================================

// Init is called once when the plugin is loaded by Bifrost.
func Init(configVal any) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	// Default OPA URL
	state.opaURL = "http://opa:8181/v1/data/chat"

	if configVal != nil {
		data, err := json.Marshal(configVal)
		if err != nil {
			return fmt.Errorf("failed to marshal incoming config: %w", err)
		}

		var cfg config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("failed to parse config structure: %w", err)
		}

		if cfg.OpaURL != "" {
			state.opaURL = cfg.OpaURL
		}
	}

	fmt.Printf("[pii-interceptor] Plugin initialized successfully. OPA URL: %s\n", state.opaURL)
	return nil
}

// GetName returns the unique plugin name.
func GetName() string {
	return "pii-interceptor"
}

// Cleanup is called on Bifrost shutdown.
func Cleanup() error {
	fmt.Println("[pii-interceptor] Plugin cleanup called")
	return nil
}

// =============================================================================
// HTTP Transport Hooks
// =============================================================================

// HTTPTransportPreHook intercepts requests before they enter Bifrost core.
func HTTPTransportPreHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest) (*schemas.HTTPResponse, error) {
	if req == nil || len(req.Body) == 0 {
		return nil, nil
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return nil, nil
	}

	messagesRaw, ok := payload["messages"].([]interface{})
	if !ok || len(messagesRaw) == 0 {
		return nil, nil
	}

	var userText string
	for i := len(messagesRaw) - 1; i >= 0; i-- {
		if msg, ok := messagesRaw[i].(map[string]interface{}); ok {
			role, _ := msg["role"].(string)
			if role == "user" {
				userText, _ = msg["content"].(string)
				break
			}
		}
	}

	if userText == "" {
		return nil, nil
	}

	allowed, reason, err := evaluateOPA(userText)
	if err != nil {
		ctx.Log(schemas.LogLevelWarn, fmt.Sprintf("[pii-interceptor] OPA evaluation failed: %v", err))
		return nil, nil
	}

	if !allowed {
		fmt.Printf("[pii-interceptor] Request blocked by OPA: %s\n", reason)
		ctx.Log(schemas.LogLevelInfo, fmt.Sprintf("[pii-interceptor] Request blocked by OPA: %s", reason))

		isStream := false
		if streamVal, ok := payload["stream"].(bool); ok && streamVal {
			isStream = true
		}

		message := fmt.Sprintf("Request blocked by governance policy: %s", reason)

		if isStream {
			chunk := map[string]interface{}{
				"id":     "chatcmpl-blocked",
				"object": "chat.completion.chunk",
				"model":  "governance-policy",
				"choices": []interface{}{
					map[string]interface{}{
						"index": 0,
						"delta": map[string]interface{}{
							"role":    "assistant",
							"content": message,
						},
					},
				},
			}
			chunkBytes, _ := json.Marshal(chunk)

			stopChunk := map[string]interface{}{
				"id":     "chatcmpl-blocked",
				"object": "chat.completion.chunk",
				"model":  "governance-policy",
				"choices": []interface{}{
					map[string]interface{}{
						"index":         0,
						"finish_reason": "stop",
						"delta":         map[string]interface{}{},
					},
				},
			}
			stopBytes, _ := json.Marshal(stopChunk)

			sseData := fmt.Sprintf("data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", string(chunkBytes), string(stopBytes))

			return &schemas.HTTPResponse{
				StatusCode: 200,
				Headers: map[string]string{
					"Content-Type":  "text/event-stream",
					"Cache-Control": "no-cache",
					"Connection":    "keep-alive",
				},
				Body: []byte(sseData),
			}, nil
		}

		respBody := map[string]interface{}{
			"choices": []interface{}{
				map[string]interface{}{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": message,
					},
				},
			},
		}
		respBytes, _ := json.Marshal(respBody)

		return &schemas.HTTPResponse{
			StatusCode: 200,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: respBytes,
		}, nil
	}

	return nil, nil
}

// HTTPTransportPostHook intercepts responses after they exit Bifrost core (non-streaming only).
func HTTPTransportPostHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest, resp *schemas.HTTPResponse) error {
	if resp == nil || len(resp.Body) == 0 {
		return nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(resp.Body, &data); err != nil {
		ctx.Log(schemas.LogLevelWarn, fmt.Sprintf("[pii-interceptor] Failed to unmarshal response body: %v", err))
		return nil
	}

	modified := false
	if choices, ok := data["choices"].([]interface{}); ok {
		for _, c := range choices {
			if choice, ok := c.(map[string]interface{}); ok {
				if msg, ok := choice["message"].(map[string]interface{}); ok {
					if content, ok := msg["content"].(string); ok && content != "" {
						masked := maskPII(content)
						if masked != content {
							msg["content"] = masked
							modified = true
							logPIIInterception(ctx, "PII detected and masked in chat response content (non-streaming)")
						}
					}
					if reasoning, ok := msg["reasoning"].(string); ok && reasoning != "" {
						masked := maskPII(reasoning)
						if masked != reasoning {
							msg["reasoning"] = masked
							modified = true
							logPIIInterception(ctx, "PII detected and masked in chat response reasoning (non-streaming)")
						}
					}
					if rdetails, ok := msg["reasoning_details"].([]interface{}); ok {
						for _, rdVal := range rdetails {
							if rd, ok := rdVal.(map[string]interface{}); ok {
								if rdText, ok := rd["text"].(string); ok && rdText != "" {
									masked := maskPII(rdText)
									if masked != rdText {
										rd["text"] = masked
										modified = true
										logPIIInterception(ctx, "PII detected and masked in chat response reasoning_details text (non-streaming)")
									}
								}
								if rdSummary, ok := rd["summary"].(string); ok && rdSummary != "" {
									masked := maskPII(rdSummary)
									if masked != rdSummary {
										rd["summary"] = masked
										modified = true
										logPIIInterception(ctx, "PII detected and masked in chat response reasoning_details summary (non-streaming)")
									}
								}
							}
						}
					}
				}
				if text, ok := choice["text"].(string); ok && text != "" {
					masked := maskPII(text)
					if masked != text {
						choice["text"] = masked
						modified = true
						logPIIInterception(ctx, "PII detected and masked in text response content (non-streaming)")
					}
				}
			}
		}
	}

	if modified {
		newBody, err := json.Marshal(data)
		if err != nil {
			ctx.Log(schemas.LogLevelError, fmt.Sprintf("[pii-interceptor] Failed to marshal masked response: %v", err))
			return nil
		}
		resp.Body = newBody
	}

	return nil
}

// HTTPTransportStreamChunkHook intercepts streaming chunks before they are sent to the client.
func HTTPTransportStreamChunkHook(ctx *schemas.BifrostContext, req *schemas.HTTPRequest, chunk *schemas.BifrostStreamChunk) (*schemas.BifrostStreamChunk, error) {
	if chunk == nil {
		return nil, nil
	}

	var state *streamState
	val := ctx.Value(streamStateKey)
	if val == nil {
		state = &streamState{}
		ctx.SetValue(streamStateKey, state)
	} else {
		state = val.(*streamState)
	}

	isLast := false
	if val := ctx.Value(schemas.BifrostContextKeyStreamEndIndicator); val != nil {
		if b, ok := val.(bool); ok && b {
			isLast = true
		}
	}

	if chunk.BifrostChatResponse != nil && len(chunk.BifrostChatResponse.Choices) > 0 {
		for i := range chunk.BifrostChatResponse.Choices {
			choice := &chunk.BifrostChatResponse.Choices[i]
			if choice.ChatStreamResponseChoice != nil && choice.ChatStreamResponseChoice.Delta != nil {
				delta := choice.ChatStreamResponseChoice.Delta
				choiceLast := isLast || (choice.FinishReason != nil && *choice.FinishReason != "")

				if delta.Content != nil {
					masked := state.processField(ctx, choice.Index, "content", *delta.Content, choiceLast)
					delta.Content = &masked
				} else if choiceLast {
					masked := state.processField(ctx, choice.Index, "content", "", true)
					if masked != "" {
						delta.Content = &masked
					}
				}

				if delta.Reasoning != nil {
					masked := state.processField(ctx, choice.Index, "reasoning", *delta.Reasoning, choiceLast)
					delta.Reasoning = &masked
				} else if choiceLast {
					masked := state.processField(ctx, choice.Index, "reasoning", "", true)
					if masked != "" {
						delta.Reasoning = &masked
					}
				}

				for j := range delta.ReasoningDetails {
					rd := &delta.ReasoningDetails[j]
					fieldTypeText := fmt.Sprintf("reasoning_details_text_%d", j)
					fieldTypeSummary := fmt.Sprintf("reasoning_details_summary_%d", j)

					if rd.Text != nil {
						masked := state.processField(ctx, choice.Index, fieldTypeText, *rd.Text, choiceLast)
						rd.Text = &masked
					} else if choiceLast {
						masked := state.processField(ctx, choice.Index, fieldTypeText, "", true)
						if masked != "" {
							rd.Text = &masked
						}
					}

					if rd.Summary != nil {
						masked := state.processField(ctx, choice.Index, fieldTypeSummary, *rd.Summary, choiceLast)
						rd.Summary = &masked
					} else if choiceLast {
						masked := state.processField(ctx, choice.Index, fieldTypeSummary, "", true)
						if masked != "" {
							rd.Summary = &masked
						}
					}
				}
			}
		}
	}

	if chunk.BifrostTextCompletionResponse != nil && len(chunk.BifrostTextCompletionResponse.Choices) > 0 {
		for i := range chunk.BifrostTextCompletionResponse.Choices {
			choice := &chunk.BifrostTextCompletionResponse.Choices[i]
			if choice.TextCompletionResponseChoice != nil {
				choiceLast := isLast || (choice.FinishReason != nil && *choice.FinishReason != "")
				if choice.TextCompletionResponseChoice.Text != nil {
					masked := state.processField(ctx, choice.Index, "text", *choice.TextCompletionResponseChoice.Text, choiceLast)
					choice.TextCompletionResponseChoice.Text = &masked
				} else if choiceLast {
					masked := state.processField(ctx, choice.Index, "text", "", true)
					if masked != "" {
						choice.TextCompletionResponseChoice.Text = &masked
					}
				}
			}
		}
	}

	return chunk, nil
}
