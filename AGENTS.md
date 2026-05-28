# Agent Context & Rules

**Would an agent likely miss this without help?**
Yes, this file serves as the primary context for AI agents working on this Custom AI POC repository.

## Project Overview
This repository contains a Proof of Concept (POC) integrating several technologies:
- **Bifrost**: An AI router/proxy running as a Docker container on port `8081` which acts as the primary entry point for AI IDEs like OpenCode.
- **PII Interceptor Plugin**: A self-contained, native Go plugin (`external/plugin/interception/main.go`) loaded by Bifrost to dynamically detect and mask sensitive data (emails and SSN) in responses.
- **Kronk**: A local AI gateway and model server running on the host (`11435`).
- **OPA (Open Policy Agent)**: For governance and policy enforcement, running as a standalone Docker container on port `8181`.

## Architectural Guidelines
- **Bifrost-Centric Pattern**: OpenCode communicates directly with Bifrost (`http://localhost:8081/v1`). The legacy custom Go gateway (`cmd/bifrost/main.go`) is retired.
- **Self-Contained PII Interceptor**:
  - The plugin is fully implemented in [main.go](file:///Users/ecolombo/code/ardan/custom-ai/external/plugin/interception/main.go). It has no external subpackage dependencies.
  - **Dynamic Configuration**: Supports configuration parameters (like `opa_url` to customize the governance evaluation endpoint) passed dynamically via `data/config.json`.
  - **Stateful Stream Buffering**: Because streaming responses arrive in small token fragments, the plugin stores a request-scoped `streamState` in `BifrostContext`. It maps a `(choiceIndex, fieldType)` key to a text buffer, allowing concurrent buffering of content, reasoning, reasoning details, and text completions.
  - **Delimiter-based Flushing**: Buffered content is split at the last delimiter (characters that cannot be part of an SSN or Email) to flush and mask only complete words, ensuring near-zero latency. Final tokens are flushed when the stream ends.
  - **Interception Logging**: Generates console logs (`fmt.Println`) and structured logs (`ctx.Log`) to indicate when PII is detected and replaced.

## Key Directories & Files
- `external/plugin/interception/main.go`: The complete source for the native Go PII masking plugin.
- `external/bifrost/`: The Bifrost repository submodule.
- `external/bifrost/transports/Dockerfile.local`: Aligned with dynamic CGO compilation to allow Bifrost to load plugins at runtime.
- `data/config.json`: Configurations for the standalone Bifrost Docker container (mapping `custom-ai` to Kronk and registering `pii-interceptor`).
- `opencode.json`: IDE configuration pointing to Bifrost on port `8081` with prefixed model names (e.g. `custom-ai/Qwen3-0.6B-Q8_0`).

## Workflow & Commands
- **Run Everything**: `make up` (starts Docker OPA and Bifrost on port `8081`).
- **Rebuild PII Plugin & Bifrost Image**: `make build-bifrost-image` compiles the plugin inside a matching Go container and builds the local image `custom-bifrost:latest`.
- **Stop Containers**: `make down`.

Always refer to this file when adding new features or making architectural changes to ensure consistency with the established patterns.