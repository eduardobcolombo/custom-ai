{
    "$schema": "https://opencode.ai/config.json",
    "provider": {
        "custom-ai": {
            "npm": "@ai-sdk/openai-compatible",
            "name": "Custom AI (local)",
            "options": {
                "baseURL": "http://localhost:8080/v1"
            },
            "models": {
                "Qwen3-0.6B-Q8_0": {
                    "name": "Qwen3-0.6B-Q8_0",
                    "limit": {
                        "context": 131072,
                        "output": 65536
                    }
                },
                "Qwen3-8B-UD-Q8_K_XL": {
                    "name": "Qwen3-8B-UD-Q8_K_XL",
                    "limit": {
                        "context": 131072,
                        "output": 65536
                    }
                }
            }
        }
    },
    "model": "custom-ai/Qwen3-0.6B-Q8_0",
    "formatter": true,
    "lsp": true,
    "compaction": {
        "auto": false,
        "prune": false
    },
    "autoupdate": false,
    "share": "disabled",
    "mcp": {
        "custom-ai": {
            "type": "remote",
            "url": "http://localhost:9000/mcp"
        }
    },
    "plugin": [
        "opencode-session-handoff"
    ],
    "instructions": [
        "AGENTS.md"
    ]
}