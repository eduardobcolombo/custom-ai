package plugin

import (
	"fmt"

	"github.com/maximhq/bifrost/core/schemas"
	"eduardobcolombo/custom-ai/pkg/governance"
	"eduardobcolombo/custom-ai/pkg/rag"
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
	
	// 3. Augmentation
	if ragContext != "" {
		augmentedText := fmt.Sprintf("Instructions: Use the provided context to answer the query if relevant.\n\n%s\n\nQuery: %s", ragContext, userText)
		
		newInput := make([]schemas.ChatMessage, len(req.Input))
		copy(newInput, req.Input)
		
		newInput[lastIdx].Content = &schemas.ChatMessageContent{
			ContentStr: schemas.Ptr(augmentedText),
		}
		
		req.Input = newInput
	}

	// 4. Pass to the next handler
	return m.next.ChatCompletionRequest(ctx, req)
}
