package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"

	"eduardobcolombo/custom-ai/pkg/governance"
	"eduardobcolombo/custom-ai/pkg/plugin"
	"eduardobcolombo/custom-ai/pkg/rag"
)

func main() {
	client, initErr := bifrost.Init(context.Background(), schemas.BifrostConfig{
		Account: NewKronkAccount(),
	})
	if initErr != nil {
		panic(initErr)
	}
	defer client.Shutdown()

	// Initialize Governance (OPA)
	evaluator, err := governance.NewEvaluator(context.Background(), "policy/chat.rego")
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize OPA: %v", err))
	}

	// Initialize RAG Service
	ragService := rag.NewService()

	// Wrap Bifrost client with our custom Middleware
	var chatClient plugin.ChatClient = plugin.NewMiddleware(client, evaluator, ragService)

	messages := []schemas.ChatMessage{}

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Chat started. Type 'exit' to quit.")
	for {
		fmt.Print("\nYou: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" {
			break
		}

		messages = append(messages, schemas.ChatMessage{
			Role: schemas.ChatMessageRoleUser,
			Content: &schemas.ChatMessageContent{
				ContentStr: schemas.Ptr(input),
			},
		})

		response, err := chatClient.ChatCompletionRequest(schemas.NewBifrostContext(context.Background(), schemas.NoDeadline), &schemas.BifrostChatRequest{
			Provider: schemas.OpenAI,
			Model:    "Qwen3-0.6B-Q8_0",
			Input:    messages,
		})

		if err != nil {
			fmt.Println("Error:", err)
			continue
		}

		reply := *response.Choices[0].Message.Content.ContentStr
		fmt.Println("\nAssistant:", reply)

		messages = append(messages, schemas.ChatMessage{
			Role: schemas.ChatMessageRoleAssistant,
			Content: &schemas.ChatMessageContent{
				ContentStr: schemas.Ptr(reply),
			},
		})
	}
}

type KronkAccount struct {
	cachedOpenAIKeys []schemas.Key // Pre-cached keys
}

func (a *KronkAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	return []schemas.ModelProvider{schemas.OpenAI}, nil
}

func (a *KronkAccount) GetKeysForProvider(ctx context.Context, provider schemas.ModelProvider) ([]schemas.Key, error) {

	switch provider {
	case schemas.OpenAI:
		return a.cachedOpenAIKeys, nil // Pre-cached keys
	}

	return nil, fmt.Errorf("provider %s not supported", provider)
}

func (a *KronkAccount) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	if provider == schemas.OpenAI {

		return &schemas.ProviderConfig{
			NetworkConfig: schemas.NetworkConfig{
				BaseURL:            "http://127.0.0.1:11435", // Kronk's local API endpoint
				InsecureSkipVerify: true,
			},
			ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
			// ProxyConfig: &schemas.ProxyConfig{
			// 	Type: schemas.HTTPProxy,
			// 	URL:  schemas.NewEnvVar("http://127.0.0.1:8080"), // Proxy URL (if needed)
			// },
			CustomProviderConfig: &schemas.CustomProviderConfig{
				IsKeyLess:        true,
				BaseProviderType: schemas.OpenAI, // Kronk uses OpenAI-compatible API
			},
		}, nil
	}
	return nil, fmt.Errorf("provider %s not supported", provider)
}

func NewKronkAccount() *KronkAccount {
	return &KronkAccount{
		cachedOpenAIKeys: []schemas.Key{{
			Models: schemas.WhiteList{"Qwen3-0.6B-Q8_0"}, // Keep Models ["*"] to use any model
			Weight: 1.0,
		}},
	}
}
