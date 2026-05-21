# Agent Context & Rules

**Would an agent likely miss this without help?**
Yes, this file serves as the primary context for AI agents working on this Custom AI POC repository.

## Project Overview
This repository contains a Proof of Concept (POC) integrating several technologies:
- **Custom AI Gateway**: A Go HTTP server (`cmd/bifrost/main.go`) on port `8080` that acts as the primary entry point for AI IDEs like OpenCode.
- **Bifrost**: An AI router/proxy running as a standalone Docker container on port `8081`.
- **Kronk**: A local AI gateway and model server running on the host (`11435`).
- **OPA (Open Policy Agent)**: For governance and policy enforcement, running as a standalone Docker container on port `8181`.
- **RAG (Retrieval-Augmented Generation)**: A service to inject specialized context. Supports Postgres and In-Memory modes.

## Architectural Guidelines
- **HTTP Proxy Pattern**: We use a Custom Go Gateway (`cmd/bifrost/handlers/chat.go`) that intercepts HTTP `POST /v1/chat/completions` requests from clients, performs Governance/RAG, and forwards the augmented request to the Bifrost Docker container via HTTP.
- **OPA Integration**: OPA is NOT embedded. It runs as a Docker container. `pkg/governance/opa.go` is an HTTP client that communicates with the OPA REST API (`http://localhost:8181/v1/data/chat`).
- **Streaming PII Redaction**: PII masking (`pkg/pii/masker.go`) is applied dynamically to streaming HTTP response chunks in `chat.go` before flushing to the client.
- **Interface-Driven RAG**: `pkg/rag/rag.go` defines the `Retriever` interface. Implementations are in `pkg/rag/postgres` and `pkg/rag/memory`. The HTTP handlers depend only on this interface.

## Key Directories & Files
- `cmd/bifrost/main.go`: The main entry point for the Custom HTTP Gateway.
- `cmd/bifrost/handlers/chat.go`: Core logic for HTTP routing, OPA evaluation, RAG injection, and streaming PII redaction.
- `pkg/governance/opa.go`: HTTP client for the OPA Docker container.
- `pkg/rag/`: The central interface and specific implementations (memory, postgres).
- `policy/chat.rego`: The OPA Rego v1 policy definitions.
- `schema/`: SQL scripts for creating and seeding the Postgres DB.
- `data/config.json`: Configurations for the standalone Bifrost Docker container (mapping `custom-ai` to Kronk).

## Workflow & Commands
- **Run Everything**: `make up` (starts Docker Postgres, OPA, and Bifrost) and then `go run ./cmd/bifrost/main.go`.
- **Run In-Memory**: `USE_IN_MEMORY=true go run ./cmd/bifrost/main.go`
- **Dependencies**: Uses Go modules and `vendor/`. Run `go mod tidy` and `go mod vendor` when updating dependencies.

Always refer to this file when adding new features or making architectural changes to ensure consistency with the established patterns.