package plugin

import (
	"fmt"

	"eduardobcolombo/custom-ai/pkg/governance"
	"eduardobcolombo/custom-ai/pkg/pii"
	"eduardobcolombo/custom-ai/pkg/rag"
	"github.com/maximhq/bifrost/core/schemas"
)

type ChatClient interface {
	ChatCompletionRequest(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError)
}

type Middleware struct {
	next      ChatClient
	evaluator *governance.Evaluator
	rag       rag.Retriever
}

func NewMiddleware(next ChatClient, evaluator *governance.Evaluator, rag rag.Retriever) *Middleware {
	return &Middleware{
		next:      next,
		evaluator: evaluator,
		rag:       rag,
	}
}

func (m *Middleware) ChatCompletionRequest(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	if len(req.Input) == 0 {
		return m.next.ChatCompletionRequest(ctx, req)
	}

	lastIdx := len(req.Input) - 1
	lastMessage := req.Input[lastIdx]

	if lastMessage.Role != schemas.ChatMessageRoleUser || lastMessage.Content == nil || lastMessage.Content.ContentStr == nil {
		return m.next.ChatCompletionRequest(ctx, req)
	}

	userText := *lastMessage.Content.ContentStr

	// --- PRE-EXTENSION PHASE ---

	// 0. PII Detection (Log only)
	detectedPII := pii.DetectPII(userText)
	if len(detectedPII) > 0 {
		fmt.Printf("\n[Middleware] Pre-Extension found PII: %v. Allowing to pass through for now.\n", detectedPII)
	}

	// 1. Governance: OPA Evaluation
	allowed, reason, err := m.evaluator.Evaluate(ctx, userText)
	if err != nil {
		return nil, &schemas.BifrostError{
			StatusCode: schemas.Ptr(500),
			Error: &schemas.ErrorField{
				Message: fmt.Sprintf("Internal Error: OPA evaluation failed: %v", err),
			},
		}
	}

	if !allowed {
		msg := fmt.Sprintf("Request blocked by governance policy: %s", reason)
		return &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				{
					ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
						Message: &schemas.ChatMessage{
							Role: schemas.ChatMessageRoleAssistant,
							Content: &schemas.ChatMessageContent{
								ContentStr: schemas.Ptr(msg),
							},
						},
					},
				},
			},
		}, nil
	}

	// 2. Retrieval: RAG Service
	ragContext := m.rag.RetrieveContext(ctx, userText)
	
	// 3. Augmentation via System Message
	if ragContext != "" {
		fmt.Printf("\n[Middleware] Injecting RAG Context as System Message: %s\n", ragContext)

		systemMsg := schemas.ChatMessage{
			Role: schemas.ChatMessageRoleSystem,
			Content: &schemas.ChatMessageContent{
				ContentStr: schemas.Ptr(fmt.Sprintf("You are an expert assistant. Use the following specialized context to inform your answer:\n\n%s", ragContext)),
			},
		}

		newInput := make([]schemas.ChatMessage, 0, len(req.Input)+1)
		newInput = append(newInput, systemMsg)
		newInput = append(newInput, req.Input...)

		req.Input = newInput
	}

	// 4. Send the request to Kronk/Model
	resp, bifrostErr := m.next.ChatCompletionRequest(ctx, req)

	// --- POST-EXTENSION PHASE ---
	
	// 5. PII Redaction
	if resp != nil {
		for i := range resp.Choices {
			choice := resp.Choices[i].ChatNonStreamResponseChoice
			if choice != nil && choice.Message != nil && choice.Message.Content != nil {
				content := choice.Message.Content.ContentStr
				if content != nil {
					redactedOutput := pii.MaskPII(*content)
					if redactedOutput != *content {
						resp.Choices[i].ChatNonStreamResponseChoice.Message.Content.ContentStr = schemas.Ptr(redactedOutput)
					}
				}
			}
		}
	}

	return resp, bifrostErr
}
