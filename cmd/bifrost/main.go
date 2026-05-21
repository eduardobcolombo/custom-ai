package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"eduardobcolombo/custom-ai/cmd/bifrost/handlers"
	"eduardobcolombo/custom-ai/pkg/governance"
	"eduardobcolombo/custom-ai/pkg/rag"
	"eduardobcolombo/custom-ai/pkg/rag/memory"
	"eduardobcolombo/custom-ai/pkg/rag/postgres"

	"github.com/ardanlabs/conf/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := struct {
		conf.Version
		Web struct {
			APIHost string `conf:"default:0.0.0.0:8080"`
		}
		Gateways struct {
			OPAURL     string `conf:"default:http://localhost:8181/v1/data/chat"`
			BifrostURL string `conf:"default:http://localhost:8081/v1/chat/completions"`
		}
		RAG struct {
			Mode  string `conf:"default:memory"` // "memory" or "postgres"
			DBURL string `conf:"default:postgres://postgres:password@localhost:5432/custom_ai?sslmode=disable,mask"`
		}
	}{
		Version: conf.Version{
			Build: "1.0.0",
			Desc:  "Custom AI Gateway",
		},
	}

	const prefix = "BIFROST"
	help, err := conf.Parse(prefix, &cfg)
	if err != nil {
		if errors.Is(err, conf.ErrHelpWanted) {
			fmt.Println(help)
			return
		}
		log.Fatalf("parsing config: %v", err)
	}

	out, err := conf.String(&cfg)
	if err != nil {
		log.Fatalf("generating config for output: %v", err)
	}
	fmt.Printf("startup config:\n%s\n", out)

	// Initialize Governance (OPA)
	evaluator, err := governance.NewEvaluator(context.Background(), cfg.Gateways.OPAURL)
	if err != nil {
		log.Fatalf("Failed to initialize OPA evaluator: %v", err)
	}

	// Initialize RAG Service
	var ragService rag.Retriever
	if cfg.RAG.Mode == "memory" {
		ragService = memory.NewStore()
		fmt.Println("Using In-Memory RAG store")
	} else {
		pool, err := pgxpool.New(context.Background(), cfg.RAG.DBURL)
		if err != nil {
			log.Fatalf("Unable to connect to database: %v", err)
		}
		defer pool.Close()

		ragService = postgres.NewStore(pool)
		fmt.Println("Using Postgres RAG store")
	}

	chatHandler := handlers.NewChatHandler(evaluator, ragService, cfg.Gateways.BifrostURL)
	http.HandleFunc("/v1/chat/completions", chatHandler.HandleChatCompletions)

	fmt.Printf("Custom AI Gateway running on host %s...\n", cfg.Web.APIHost)
	log.Fatal(http.ListenAndServe(cfg.Web.APIHost, nil))
}
