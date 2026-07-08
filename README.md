# GoGeminiManagedAgent

Deploy [Gemini Enterprise Managed Agents](https://cloud.google.com/gemini/docs/managed-agents)
from a directory of plain files — no console clicks, no hand-crafted JSON.

---

## Why this exists

The [Gemini Enterprise Agent Platform (GEAP)](https://cloud.google.com/gemini/docs/managed-agents)
is a fully-managed runtime for agentic AI: it provisions sandboxes, routes tool calls,
and handles the infrastructure so you can focus on what the agent does.

The missing piece is the **deploy loop** — getting from "I have an idea for an agent"
to "it is live and I can iterate on it" without clicking through a console or writing
bespoke API scripts every time.

This tool is inspired by [Vercel](https://vercel.com/) and its `eve` CLI: a tight,
opinionated deploy workflow where a directory *is* the deployment unit. You describe
your agent in files, run one command, and it is live. Run it again and it updates.

```
write files → one command → live agent
```

### What part of the lifecycle this covers

```
┌─────────────────────────────────────────────────────────────────────────┐
│  YOUR MACHINE (local, version-controlled)                               │
│                                                                          │
│   my-agent/                                                              │
│   ├── agent.yaml          ← tools, model, GCS bucket                   │
│   ├── instructions.md     ← system prompt                               │
│   └── skills/             ← playbooks the agent reads at runtime        │
│                                                                          │
│  geap-managed-agents-builder deploy   ◄── this tool                     │
└────────────────────┬────────────────────────────────────────────────────┘
                     │ uploads files to GCS, calls Vertex AI API
                     ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  EPHEMERAL REMOTE SANDBOX (spun up per interaction, torn down after)    │
│                                                                          │
│   /workspace/              ← GCS files mounted here at start            │
│   Python runtime           ← code_execution tool                        │
│   Outbound network         ← mcp_server, url_context, google_search     │
│                                                                          │
│  Nothing written here survives the interaction unless you explicitly     │
│  push it back via an MCP server or GCS.                                 │
└────────────────────┬────────────────────────────────────────────────────┘
                     │ MCP protocol
                     ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  DURABLE EXTERNAL SYSTEMS (persist across interactions)                  │
│                                                                          │
│   Google Drive / Docs / Sheets   ← via Drive MCP server                │
│   BigQuery                        ← via BigQuery MCP server             │
│   Any database, API, or service   ← via any MCP-compliant server        │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Quickstart

### 1. Install

**Option A — build from source (Go 1.25+ required):**

```bash
git clone https://github.com/zeroasterisk/GoGeminiManagedAgent.git
cd GoGeminiManagedAgent
go build -o geap-managed-agents-builder ./src
# Optionally move to your PATH:
mv geap-managed-agents-builder /usr/local/bin/
```

**Option B — one-liner install script:**

```bash
curl -fsSL https://raw.githubusercontent.com/zeroasterisk/GoGeminiManagedAgent/main/install.sh | bash
```

> Don't have Go? The install script auto-detects and installs it via your
> package manager or [asdf](https://asdf-vm.com/).

### 2. Authenticate

```bash
gcloud auth application-default login
```

### 3. Deploy the minimal example

```bash
GEMINI_PROJECT_ID=my-gcp-project \
  geap-managed-agents-builder deploy -dir ./examples/minimal
```

### 4. Talk to it

```bash
GEMINI_PROJECT_ID=my-gcp-project \
  geap-managed-agents-builder verify -dir ./examples/minimal \
  -prompt "What is the population of Tokyo as of 2024?"
```

---

## Comprehensive example

This is a complete agent that uses every supported tool type.
Create a directory with these two files:

**`my-agent/agent.yaml`**

```yaml
id: "research-agent"
description: "Searches the web, reads URLs, runs Python, and connects to durable data via MCP."
project_id: "my-gcp-project"

# Files in this directory (except agent.yaml) are uploaded to GCS and mounted
# at /workspace in the remote sandbox on every interaction.
gcs_bucket: "my-gcp-project-agents"

# Supported tool types (all five, as of 2026-07-08):
tools:
  # Ephemeral Python sandbox — output lives only for this interaction
  - type: "code_execution"

  # R/W access to /workspace inside the sandbox (seeded from GCS above)
  - type: "filesystem"

  # Grounded web search
  - type: "google_search"

  # Fetch and inject the content of URLs mentioned in the conversation
  - type: "url_context"

  # External MCP server — the path to durable integrations (Drive, BigQuery, etc.)
  # Data written here persists after the interaction ends.
  - type: "mcp_server"
    name: "my-data-server"
    url: "https://your-mcp-server.example.com/mcp"
    headers:
      Authorization: "Bearer YOUR_MCP_TOKEN"
```

**`my-agent/instructions.md`**

```markdown
You are a research and data assistant.

**Ephemeral (this interaction only):**
- Run Python via code_execution to compute, parse, or transform data.
- Read files in /workspace via the filesystem tool.

**Durable (persists after this interaction):**
- Read and write through the MCP server. Confirm with the user before writing.

**Live information:**
- Search the web via google_search.
- Fetch specific pages via url_context.

Always say which tool you are using and why.
```

**Deploy:**

```bash
geap-managed-agents-builder deploy -dir ./my-agent
```

**Test:**

```bash
geap-managed-agents-builder verify -dir ./my-agent \
  -prompt "Fetch https://go.dev/doc/faq and summarise the top 3 FAQs."
```

**Update** (re-run deploy — it PATCHes if the agent already exists):

```bash
# Edit instructions.md or agent.yaml, then:
geap-managed-agents-builder deploy -dir ./my-agent
```

**Delete:**

```bash
geap-managed-agents-builder delete -dir ./my-agent
```

---

## Tool types explained

| Tool | What it does | Persists? |
|---|---|---|
| `code_execution` | Python sandbox in the remote environment | No — ephemeral |
| `filesystem` | R/W `/workspace` in the sandbox (seeded from GCS) | No — ephemeral |
| `google_search` | Grounded web search | N/A |
| `url_context` | Fetch and inject URL content at inference time | N/A |
| `mcp_server` | Connect to any [MCP](https://modelcontextprotocol.io/) server | **Yes** — via the server |

**Durable integrations via MCP** — Drive, BigQuery, databases, GitHub, Slack, and
anything else with an MCP server are all wired through `mcp_server`. The agent sandbox
is ephemeral; the MCP server is not.

---

## agent.yaml reference

```yaml
id: "my-agent"           # Required. Unique within the project.
description: "..."       # Optional.
project_id: "..."        # Optional if GEMINI_PROJECT_ID env var is set.
location: "global"       # Optional. Must be "global" (only value the API accepts).
base_agent: "antigravity-preview-05-2026"  # Optional. Default shown.
gcs_bucket: "..."        # Required only when using filesystem or skills.

tools:
  - type: "code_execution"
  - type: "filesystem"
  - type: "google_search"
  - type: "url_context"
  - type: "mcp_server"
    name: "my-server"    # Optional label.
    url: "https://..."   # Required for mcp_server.
    headers:             # Optional auth headers.
      Authorization: "Bearer ..."
```

### Environment variable overrides

| Variable | Effect |
|---|---|
| `GEMINI_PROJECT_ID` | Overrides `project_id` in agent.yaml |
| `GEMINI_LOCATION` | Overrides `location` in agent.yaml |

---

## CLI reference

```
geap-managed-agents-builder [flags] <command>

Commands:
  deploy   Create or update the agent (default when no command given)
  verify   Send a test prompt and print the response
  delete   Remove the agent

Flags:
  -dir string     Agent config directory (default ".")
  -prompt string  Prompt for the verify command (default "Hello")
  -verbose        Print raw JSON response (verify only)
```

---

## More examples

| Directory | What it demonstrates |
|---|---|
| [`examples/minimal/`](examples/minimal/) | `google_search` + `code_execution`. No GCS, no persistence. The smallest possible agent. |
| [`examples/url-context/`](examples/url-context/) | `url_context` + `google_search`. Agent fetches and summarises live URLs. |
| [`examples/with-skills/`](examples/with-skills/) | `code_execution` + `filesystem` + GCS-backed skill playbooks in `/workspace/skills/`. |
| [`examples/mcp-server/`](examples/mcp-server/) | `mcp_server` + `code_execution`. Template for durable Drive / BigQuery / database integrations. |
| [`examples/full-featured/`](examples/full-featured/) | All five tool types together. Reference config, not a production template. |

---

## Testing

### Unit tests (no GCP credentials required)

```bash
go test ./src/...

# With race detector
go test -race ./src/...
```

Tests use `net/http/httptest` to mock all API calls.

### End-to-end tests (real GCP project required)

E2E tests deploy actual agents, run real interactions, and clean up after themselves.
They are excluded from the normal test suite via a build tag.

**Prerequisites:**

```bash
gcloud auth application-default login
export GEMINI_PROJECT_ID=my-gcp-project
```

**Run all e2e tests:**

```bash
go test -tags e2e ./e2e/... -v -timeout 5m
```

**Run a specific scenario:**

```bash
# Minimal agent (search + code)
go test -tags e2e ./e2e/... -v -timeout 5m -run TestE2E_Minimal

# GCS-backed agent with skills
go test -tags e2e ./e2e/... -v -timeout 5m -run TestE2E_WithSkills

# Idempotency: deploy twice, verify PATCH path
go test -tags e2e ./e2e/... -v -timeout 5m -run TestE2E_DeployIdempotent

# Delete then re-deploy
go test -tags e2e ./e2e/... -v -timeout 5m -run TestE2E_Delete
```

Each test:
1. Deploys a uniquely-named agent (timestamped to avoid collisions)
2. Sends a real prompt and waits for the response
3. Deletes the agent in `t.Cleanup` — even on failure

---

## Prerequisites

- Go 1.25+
- A GCP project with the [Vertex AI API](https://console.cloud.google.com/apis/library/aiplatform.googleapis.com) enabled
- [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials): `gcloud auth application-default login`
- For GCS-backed agents: a GCS bucket the project's service account can write to

---

## License

Apache 2.0
