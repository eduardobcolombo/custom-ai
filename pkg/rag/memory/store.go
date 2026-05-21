package memory

import (
	"context"
	"strings"
)

type Store struct {
	knowledgeBase map[string]string
}

// NewStore initializes a new in-memory RAG store with mock data.
func NewStore() *Store {
	return &Store{
		knowledgeBase: map[string]string{
			"bifrost":     "Bifrost is an AI router and proxy that handles load balancing, retries, and plugin execution.",
			"kronk":       "Kronk is an AI gateway or model server that acts as a bridge to underlying local models.",
			"opa":         "Open Policy Agent (OPA) is an open-source, general-purpose policy engine that unifies policy enforcement.",
			"rag":         "Retrieval-Augmented Generation (RAG) provides contextual knowledge to an LLM before generating a response.",
			"123-45-6789": "123-45-6789 is the social security number of the user Bill Kennedy, a partner in Ardan Labs.",
		},
	}
}

// RetrieveContext returns specialized context based on keyword matching.
func (s *Store) RetrieveContext(ctx context.Context, query string) string {
	var contexts []string
	lowerQuery := strings.ToLower(query)

	for keyword, knowledge := range s.knowledgeBase {
		if strings.Contains(lowerQuery, keyword) {
			contexts = append(contexts, knowledge)
		}
	}

	if len(contexts) == 0 {
		return ""
	}

	return "Additional Context:\n- " + strings.Join(contexts, "\n- ")
}
