# GoGeminiManagedAgent

A Go CLI that deploys and manages [Gemini Enterprise Managed Agents](https://cloud.google.com/gemini/docs/managed-agents) using a filesystem-first, declarative approach.

Given a directory containing an `agent.yaml` and an `instructions.md`, the tool:

1. Reads the agent configuration
2. Uploads any supporting files (skills, docs) to GCS if a bucket is configured
3. Creates or updates the agent via the Vertex AI API (idempotent — POST on first run, PATCH on subsequent runs)

## Prerequisites

- Go 1.25+
- A GCP project with the Vertex AI API enabled
- [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials) configured (`gcloud auth application-default login`)

## Agent directory structure

```
my-agent/
├── agent.yaml          # Agent metadata, model, tools, and optional GCS config
├── instructions.md     # System prompt (uploaded to GCS if bucket is set)
└── skills/             # Optional — Markdown playbooks available to the agent at runtime
    └── my-skill/
        └── SKILL.md
```

### `agent.yaml` reference

```yaml
id: "my-agent-id"               # Required. Unique agent identifier within the project.
description: "What this does"   # Optional.
project_id: "my-gcp-project"    # Optional if GEMINI_PROJECT_ID env var is set.
location: "global"              # Optional. Defaults to "global".
base_agent: "antigravity-preview-05-2026"  # Optional. Defaults to the current preview model.
gcs_bucket: "my-gcs-bucket"    # Required only if you have skills or other files to mount.
tools:
  - type: "code_execution"
  - type: "google_search"
  - type: "http"
    name: "my-api"
    url: "https://example.com/api"
    headers:
      Authorization: "Bearer ${MY_TOKEN}"
```

`instructions.md`, `skills/`, and any other non-`agent.yaml` files are uploaded to
`gs://<gcs_bucket>/<agent-id>/` and mounted at `/workspace` in the agent runtime.

## Usage

### Build

```bash
go build -o geap-managed-agents-builder ./src
```

### Deploy

```bash
./geap-managed-agents-builder -dir ./examples/minimal
```

Project and location can be overridden via environment variables:

```bash
export GEMINI_PROJECT_ID="my-gcp-project"
export GEMINI_LOCATION="us-central1"
./geap-managed-agents-builder -dir ./examples/with-skills
```

### Verify (send a test prompt)

```bash
./geap-managed-agents-builder -dir ./examples/minimal -verify -prompt "What is 2+2?"
```

Pass `-verbose` to print the raw JSON response.

## Examples

| Directory | Description |
|---|---|
| `examples/minimal` | No GCS; uses `code_execution` and `google_search` tools |
| `examples/with-skills` | GCS-backed; uploads a math skill to `/workspace/skills/` |

## Development

```bash
# Run all tests (no GCP credentials required)
go test ./src/...

# Run with race detector
go test -race ./src/...
```

Tests use `net/http/httptest` to mock the Vertex AI API — no real GCP calls are made.

## License

Apache 2.0
