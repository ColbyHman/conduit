# ADR 0005: Worker Architecture and LLM Provider Abstraction

## Status
Accepted

## Context
Workers process two job types (`journal_analyze`, `mtg_analyze`) using LLMs. Need to support multiple providers (OpenAI, Anthropic, local Ollama/vLLM) without rewriting handlers.

## Decision
**Provider-agnostic LLM client interface** with a registry pattern. Handlers depend on interface, not concrete implementation.

## Interface
```go
type LLMClient interface {
    Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
    CountTokens(text string) (int, error)
    Model() string
}
```

## Configuration
```yaml
llm:
  provider: "openai"  # openai, anthropic, ollama, openai-compatible
  model: "gpt-4o-mini"
  api_key: "${LLM_API_KEY}"
  endpoint: "${LLM_ENDPOINT}"  # optional override
  temperature: 0.3
  max_tokens: 2000
  timeout: 60s
  # Provider-specific
  ollama:
    host: "http://host.docker.internal:11434"
```

## Provider Implementations
| Provider | Package | Notes |
|----------|---------|-------|
| OpenAI | `internal/llm/openai` | Official API |
| Anthropic | `internal/llm/anthropic` | Messages API |
| Ollama | `internal/llm/ollama` | Local, `/api/chat` |
| OpenAI-Compatible | `internal/llm/openai_compat` | vLLM, LM Studio, etc. |

## Prompt Templates
- Stored in `prompts/` as `.txt` files with `{{.Field}}` placeholders
- Loaded at startup, cached in memory
- Versioned via git — prompt changes are code changes

## Handler Pattern
```go
type JobHandler interface {
    JobType() string
    Handle(ctx context.Context, job *Job, llm LLMClient) (*Result, error)
}
```
- `journal_analyze` → `JournalAnalyzeHandler`
- `mtg_analyze` → `MTGAnalyzeHandler`
- Workers register handlers at startup, route by `job.job_type`

## Rationale
- **Testability** — Handlers accept interface; easy to mock in unit tests
- **Swap providers** — Change config, restart; no code changes
- **Local-first** — Ollama/vLLM support for privacy/cost
- **Prompt versioning** — Git history = prompt history

## Consequences
- Workers import all provider packages (register via init or explicit)
- `LLM_API_KEY` required for cloud providers; optional for local
- Token counting uses provider tokenizer (approximate for local)