# Agent Context & Rules

**Would an agent likely miss this without help?**
Yes, this file serves as the primary context for AI agents working on this Custom AI POC repository.

## Project Overview
This repository contains a Proof of Concept (POC) integrating several technologies:
- **Bifrost**: An AI router/proxy.
- **Kronk**: A local AI gateway and model server.
- **OPA (Open Policy Agent)**: For governance and policy enforcement.
- **RAG (Retrieval-Augmented Generation)**: A service to inject specialized context. Supports Postgres and In-Memory modes.

## Architectural Guidelines
- **Bifrost Plugins**: We use the **Middleware pattern** (wrapping `ChatCompletionRequest`) instead of native Bifrost hooks. See `pkg/plugin/middleware.go`.
- **OPA Integration**: OPA is embedded in Go using `github.com/open-policy-agent/opa/v1/rego`.
- **Interface-Driven RAG**: `pkg/rag/rag.go` defines the `Retriever` interface. The implementations are cleanly separated into `pkg/rag/postgres` and `pkg/rag/memory`. The middleware must remain agnostic and only depend on the interface.

## Key Directories & Files
- `cmd/bifrost/main.go`: The main entry point. Toggles between Postgres/Memory based on `USE_IN_MEMORY`.
- `pkg/plugin/middleware.go`: Custom middleware handling OPA evaluation and RAG injection.
- `pkg/governance/opa.go`: Embedded OPA evaluator.
- `pkg/rag/`: The central interface and specific implementations (memory, postgres).
- `policy/chat.rego`: The OPA Rego v1 policy definitions.
- `schema/`: SQL scripts for creating and seeding the Postgres DB automatically via Docker.

## Workflow & Commands
- **Run with Database**: `make up` (starts Docker Postgres and runs the app)
- **Run In-Memory (No Docker)**: `USE_IN_MEMORY=true make run`
- **Dependencies**: The project uses Go modules and a `vendor/` directory. Run `go mod tidy` and `go mod vendor` when updating dependencies.

Always refer to this file when adding new features or making architectural changes to ensure consistency with the established patterns.