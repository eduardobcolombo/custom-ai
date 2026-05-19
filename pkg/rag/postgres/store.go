package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new Postgres-backed RAG store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		pool: pool,
	}
}

// RetrieveContext returns specialized context by executing an ILIKE query against the database.
func (s *Store) RetrieveContext(ctx context.Context, query string) string {
	// Search if any keyword from the DB is contained within the user's query
	rows, err := s.pool.Query(ctx, "SELECT content FROM knowledge_base WHERE $1 ILIKE '%' || keyword || '%'", query)
	if err != nil {
		fmt.Printf("Error querying knowledge base: %v\n", err)
		return ""
	}
	defer rows.Close()

	var contexts []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			fmt.Printf("Error scanning row: %v\n", err)
			continue
		}
		contexts = append(contexts, content)
	}

	if len(contexts) == 0 {
		return ""
	}

	return "Additional Context:\n- " + strings.Join(contexts, "\n- ")
}
