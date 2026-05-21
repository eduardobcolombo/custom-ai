package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"eduardobcolombo/custom-ai/pkg/governance"
	"eduardobcolombo/custom-ai/pkg/pii"
	"eduardobcolombo/custom-ai/pkg/rag"
	"strings"
)

type ChatHandler struct {
	Evaluator  *governance.Evaluator
	Rag        rag.Retriever
	BifrostURL string
}

func NewChatHandler(evaluator *governance.Evaluator, ragService rag.Retriever, bifrostURL string) *ChatHandler {
	return &ChatHandler{
		Evaluator:  evaluator,
		Rag:        ragService,
		BifrostURL: bifrostURL,
	}
}

func (h *ChatHandler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read and parse the incoming request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Failed to parse JSON", http.StatusBadRequest)
		return
	}

	messagesRaw, ok := payload["messages"].([]interface{})
	if !ok || len(messagesRaw) == 0 {
		http.Error(w, "No messages provided", http.StatusBadRequest)
		return
	}

	lastMessageRaw, ok := messagesRaw[len(messagesRaw)-1].(map[string]interface{})
	if !ok {
		http.Error(w, "Invalid message format", http.StatusBadRequest)
		return
	}

	role, _ := lastMessageRaw["role"].(string)

	var userText string
	if role == "user" {
		userText, _ = lastMessageRaw["content"].(string)
		if userText == "" {
			http.Error(w, "User message cannot be empty", http.StatusBadRequest)
			return
		}
	}

	// Ensure the model string has 'custom-ai/' prefix for Bifrost routing
	if modelStr, ok := payload["model"].(string); ok {
		// OpenCode might send "Qwen3-0.6B-Q8_0/AGENT". Bifrost uses the first part before the slash as the provider.
		if !strings.HasPrefix(modelStr, "custom-ai/") {
			payload["model"] = "custom-ai/" + modelStr
		}
	}

	// --- PRE-EXTENSION PHASE ---

	if role == "user" {
		// 0. PII Detection (Log only)
		detectedPII := pii.DetectPII(userText)
		if len(detectedPII) > 0 {
			fmt.Printf("\n[Gateway] Pre-Extension found PII: %v. Allowing to pass through.\n", detectedPII)
		}

		// 1. Governance: OPA Evaluation
		// Later we can use OPA to decide if the user query is allowed to be processed or not.
		// For now, it just validates our P2 rules.
		// We can also check the access if it can use RAG or only the model
		// Validate it using x-ARDAN-key header
		// Add more headers or even a bearer token to include more context to be used in OPA policies
		allowed, reason, err := h.Evaluator.Evaluate(r.Context(), userText)
		if err != nil {
			http.Error(w, fmt.Sprintf("OPA evaluation failed: %v", err), http.StatusInternalServerError)
			return
		}

		// If request is denied, send an error response
		if !allowed {
			isStream := false
			if streamVal, ok := payload["stream"].(bool); ok && streamVal {
				isStream = true
			}
			msg := fmt.Sprintf("Request blocked by governance policy: %s", reason)
			sendErrorResponse(w, msg, isStream)
			return
		}

		// 2. Retrieval: RAG Service
		ragContext := h.Rag.RetrieveContext(r.Context(), userText)

		// 3. Augmentation via System Message
		if ragContext != "" {
			fmt.Printf("\n[Gateway] Injecting RAG Context as System Message: %s\n", ragContext)

			systemMsg := map[string]interface{}{
				"role":    "system",
				"content": fmt.Sprintf("You are an expert assistant. Use the following specialized context to inform your answer:\n\n%s", ragContext),
			}

			newMessages := []interface{}{systemMsg}
			newMessages = append(newMessages, messagesRaw...)
			payload["messages"] = newMessages
		}
	}

	// 4. Forward to Bifrost Docker Container
	forwardReqBody, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "Failed to marshal forwarded request", http.StatusInternalServerError)
		return
	}

	forwardResp, err := http.Post(h.BifrostURL, "application/json", bytes.NewBuffer(forwardReqBody))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to contact Bifrost Gateway: %v", err), http.StatusBadGateway)
		return
	}
	defer forwardResp.Body.Close()

	if forwardResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(forwardResp.Body)
		http.Error(w, fmt.Sprintf("Bifrost error: %s", string(body)), forwardResp.StatusCode)
		return
	}

	isStream := false
	if s, ok := payload["stream"].(bool); ok {
		isStream = s
	}

	// For better experience with OpenCode I added the stream instead of the default message response
	// The bifrost forwards the response from the AI provider as-is to the client.
	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		var textBuffer string
		var lastChunk map[string]interface{}
		var activeKey string // "content", "reasoning_content", or "tool_calls"

		flushBuffer := func(final bool) {
			if textBuffer == "" || activeKey == "" {
				return
			}

			splitIdx := len(textBuffer)
			if !final {
				lastDelim := strings.LastIndexAny(textBuffer, " \n\t,;!?")
				if lastDelim != -1 {
					splitIdx = lastDelim + 1
				} else {
					return
				}
			}

			toProcess := textBuffer[:splitIdx]
			textBuffer = textBuffer[splitIdx:]

			masked := pii.MaskPII(toProcess)

			// Refactore it later. It is a tricky way to handle PII in stream
			if lastChunk != nil {
				if choices, ok := lastChunk["choices"].([]interface{}); ok && len(choices) > 0 {
					if choice, ok := choices[0].(map[string]interface{}); ok {
						if delta, ok := choice["delta"].(map[string]interface{}); ok {
							if activeKey == "content" || activeKey == "reasoning_content" || activeKey == "reasoning" {
								delta[activeKey] = masked
							} else if activeKey == "tool_calls" {
								if toolCalls, ok := delta["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
									if tc, ok := toolCalls[0].(map[string]interface{}); ok {
										if fn, ok := tc["function"].(map[string]interface{}); ok {
											fn["arguments"] = masked
										}
									}
								}
							}
							// Strip reasoning_details to prevent unmasked PII leakage
							delete(delta, "reasoning_details")
						}
						// If Bifrost sends the accumulated 'message' in the stream, delete it
						// to force the IDE to rely entirely on the masked 'delta' stream.
						delete(choice, "message")
					}
				}
				modifiedChunk, _ := json.Marshal(lastChunk)
				// fmt.Printf("[DEBUG-SENT] %s\n", string(modifiedChunk))
				fmt.Fprintf(w, "data: %s\n\n", string(modifiedChunk))
				flusher.Flush()
			}
		}

		scanner := bufio.NewScanner(forwardResp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")
				if dataStr == "[DONE]" {
					flushBuffer(true)
					fmt.Fprintf(w, "data: [DONE]\n\n")
					flusher.Flush()
					continue
				}
				// fmt.Printf("[DEBUG] Chunk: %s\n", dataStr) // Un-comment to trace raw chunks

				var chunk map[string]interface{}
				if err := json.Unmarshal([]byte(dataStr), &chunk); err == nil {
					hasContent := false
					if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
						if choice, ok := choices[0].(map[string]interface{}); ok {
							if delta, ok := choice["delta"].(map[string]interface{}); ok {

								rContent, hasR := delta["reasoning_content"].(string)
								if !hasR {
									rContent, hasR = delta["reasoning"].(string)
								}
								content, hasC := delta["content"].(string)

								var toolArg string
								var hasToolArg bool
								if toolCalls, ok := delta["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
									if tc, ok := toolCalls[0].(map[string]interface{}); ok {
										if fn, ok := tc["function"].(map[string]interface{}); ok {
											if arg, ok := fn["arguments"].(string); ok && arg != "" {
												toolArg = arg
												hasToolArg = true
											}
										}
									}
								}

								if hasR && rContent != "" {
									if activeKey != "reasoning_content" && activeKey != "reasoning" {
										flushBuffer(true)
										// Set activeKey to whatever field the backend actually sent
										if _, ok := delta["reasoning"].(string); ok {
											activeKey = "reasoning"
										} else {
											activeKey = "reasoning_content"
										}
									}
									textBuffer += rContent
									lastChunk = chunk
									hasContent = true
								}

								if hasC && content != "" {
									if activeKey != "content" {
										flushBuffer(true)
										activeKey = "content"
									}
									textBuffer += content
									lastChunk = chunk
									hasContent = true
								}

								if hasToolArg {
									if activeKey != "tool_calls" {
										flushBuffer(true)
										activeKey = "tool_calls"
									}
									textBuffer += toolArg
									lastChunk = chunk
									hasContent = true
								}
							}
						}
					}

					if hasContent {
						flushBuffer(false)
					} else {
						// Ensure we flush any pending text before sending metadata chunks (like finish_reason="stop" or empty transitions)
						if textBuffer != "" {
							if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
								if choice, ok := choices[0].(map[string]interface{}); ok {
									if finishReason, _ := choice["finish_reason"].(string); finishReason != "" {
										flushBuffer(true)
									} else if delta, ok := choice["delta"].(map[string]interface{}); ok {
										if _, hasC := delta["content"]; hasC {
											flushBuffer(true)
										}
									}
								}
							}
						}

						// fmt.Printf("[DEBUG-RAW] %s\n", line)
						fmt.Fprintf(w, "%s\n\n", line)
						flusher.Flush()
					}
				} else {
					fmt.Fprintf(w, "%s\n\n", line)
					flusher.Flush()
				}
			} else if line != "" {
				fmt.Fprintf(w, "%s\n\n", line)
				flusher.Flush()
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Printf("[Gateway] Error reading stream: %v\n", err)
		}
		return
	}

	var bifrostResponse map[string]interface{}
	if err := json.NewDecoder(forwardResp.Body).Decode(&bifrostResponse); err != nil {
		http.Error(w, "Failed to parse Bifrost response", http.StatusInternalServerError)
		return
	}

	// --- POST-EXTENSION PHASE ---
	// ONLY FOR NON-STREAMING REQUESTS
	// 5. PII Redaction
	if choices, ok := bifrostResponse["choices"].([]interface{}); ok && len(choices) > 0 {
		for _, c := range choices {
			if choice, ok := c.(map[string]interface{}); ok {
				if msg, ok := choice["message"].(map[string]interface{}); ok {
					if content, ok := msg["content"].(string); ok && content != "" {
						redactedOutput := pii.MaskPII(content)
						if redactedOutput != content {
							msg["content"] = redactedOutput
						}
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bifrostResponse)
}

func sendErrorResponse(w http.ResponseWriter, message string, isStream bool) {
	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK) // 200 OK to prevent UI error banners

		// Stream the message as a chat chunk
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
		fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))

		// Send stop chunk
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
		fmt.Fprintf(w, "data: %s\n\n", string(stopBytes))

		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // 200 OK to prevent UI error banners

	resp := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": message,
				},
			},
		},
	}
	json.NewEncoder(w).Encode(resp)
}
