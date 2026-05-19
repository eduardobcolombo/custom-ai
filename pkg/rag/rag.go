package rag

import "context"

// Retriever defines the contract for fetching specialized context for a query.
type Retriever interface {
	RetrieveContext(ctx context.Context, query string) string
}
