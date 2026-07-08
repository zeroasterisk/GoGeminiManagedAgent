# GoGeminiManagedAgent

A deploy CLI for [Gemini Enterprise Managed Agents](https://cloud.google.com/gemini/docs/managed-agents)
— describe your agent in a directory, run one command, it's live.

---

## What this is

[Gemini Enterprise Agent Platform (GEAP)](https://cloud.google.com/gemini/docs/managed-agents)
is Google's fully-managed runtime for agentic AI: it provisions sandboxes, executes tool
calls, and handles the infrastructure. The gap is the **deploy loop** — going from a local
directory to a running agent without hand-crafting JSON or clicking through a console.

This tool fills that gap. It is inspired by [Vercel's `eve` framework](https://vercel.com/blog/introducing-eve):
the idea that **an agent is a directory**, and a CLI should be all you need to go from
that directory to something running in production.

```
write files  →  geap-managed-agents-builder deploy  →  live agent
```

The analogy is to `vercel deploy`, not to the full `eve` framework. `eve` is a complete
agent runtime with durable sessions, channels, evals, and scheduling. GEAP provides the
equivalent runtime on Google Cloud — and this CLI is the `vercel deploy` step for it.

---

## Where your agent lives after deploy

This is the most important thing to understand before you start:

```
┌─────────────────────────────────────────────────────────────────────┐
│  YOUR MACHINE  (local, version-controlled)                           │
│                                                                      │
│  my-agent/                                                           │
│  ├── agent.yaml          ← tools, model, GCS bucket                 │
│  ├── instructions.md     ← system prompt                            │
│  └── skills/             ← playbooks the agent reads at runtime     │
│                                                                      │
│  geap-managed-agents-builder deploy   ◄── this tool                 │
└──────────────────┬──────────────────────────────────────────────────┘
                   │ uploads files to GCS, calls Vertex AI API
                   ▼
┌─────────────────────────────────────────────────────────────────────┐
│  GEMINI AGENT PLATFORM  (Google-managed)                             │
│                                                                      │
│  Resource name:                                                      │
│  projects/{project}/locations/global/agents/{id}                     │
│                                                                      │
│  ⚠ No public URL. Callable only through the Vertex AI REST API      │
│    with a Google OAuth token (service account or ADC).               │
│    An A2A bridge is on the roadmap — see "Using your agent" below.   │
└──────────────────┬──────────────────────────────────────────────────┘
                   │ per-interaction sandbox provisioned on demand
                   ▼
┌─────────────────────────────────────────────────────────────────────┐
│  EPHEMERAL SANDBOX  (per-interaction, torn down after)               │
│                                                                      │
│  /workspace/        ← GCS files mounted here at start               │
│  Python runtime     ← code_execution tool                           │
│  Outbound network   ← mcp_server, url_context, google_search        │
│                                                                      │
│  Nothing written here survives the interaction unless you push       │
│  it back via an MCP server.                                          │
└──────────────────┬──────────────────────────────────────────────────┘
                   │ MCP protocol  (over HTTPS)
                   ▼
┌─────────────────────────────────────────────────────────────────────┐
│  DURABLE EXTERNAL SYSTEMS  (persist across interactions)             │
│                                                                      │
│  Google Drive / Docs / Sheets   ← via Drive MCP server              │
│  BigQuery                        ← via BigQuery MCP server          │
│  Any database, API, or service   ← any MCP-compliant server         │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Quickstart

### 1. Install

**Build from source (Go 1.25+):**

```bash
git clone https://github.com/zeroasterisk/GoGeminiManagedAgent.git
cd GoGeminiManagedAgent
go build -o geap-managed-agents-builder ./src
mv geap-managed-agents-builder /usr/local/bin/   # optional
```

**One-liner (auto-installs Go if needed):**

```bash
curl -fsSL https://raw.githubusercontent.com/zeroasterisk/GoGeminiManagedAgent/main/install.sh | bash
```

### 2. Authenticate

```bash
gcloud auth application-default login
```

### 3. Deploy

```bash
GEMINI_PROJECT_ID=my-gcp-project \
  geap-managed-agents-builder deploy -dir ./examples/minimal
```

### 4. See what's deployed

```bash
GEMINI_PROJECT_ID=my-gcp-project geap-managed-agents-builder list
```

```
ID               DESCRIPTION                        TOOLS                          UPDATED
--               -----------                        -----                          -------
minimal-agent    Search the web and run Python.     google_search, code_execution  2026-07-08
research-agent   Research and data assistant.       url_context, mcp_server        2026-07-08

2 agent(s) in project "my-gcp-project" (location: global)
```

### 5. Talk to it (from the CLI)

```bash
GEMINI_PROJECT_ID=my-gcp-project \
  geap-managed-agents-builder verify -dir ./examples/minimal \
  -prompt "What is 12 factorial? Use code."
```

> **Coming soon:** an A2A bridge will expose each managed agent as a standard
> [A2A](https://github.com/a2aproject/A2A) endpoint so any A2A-capable client
> (ADK, LangGraph, etc.) can discover and call your deployed agents without
> touching the Vertex AI API directly. Until then, see "Using your agent from
> code" below.

---

## Comprehensive example

**`my-agent/agent.yaml`**

```yaml
id: "research-agent"
description: "Research and data assistant."
project_id: "my-gcp-project"

# Files here (except agent.yaml) are uploaded to GCS and mounted at
# /workspace in the sandbox on every interaction.
gcs_bucket: "my-gcp-project-agents"

tools:
  # Ephemeral Python sandbox — output lives only for this interaction
  - type: "code_execution"

  # R/W access to /workspace inside the sandbox
  - type: "filesystem"

  # Grounded web search
  - type: "google_search"

  # Fetch and inject URL content at inference time
  - type: "url_context"

  # External MCP server — the path to durable integrations
  # (Drive, BigQuery, databases, etc.)
  # ⚠ Keep tokens out of git: use env var substitution or a secret manager.
  - type: "mcp_server"
    name: "my-data-server"
    url: "https://your-mcp-server.example.com/mcp"
    headers:
      Authorization: "Bearer YOUR_MCP_TOKEN"
```

**`my-agent/instructions.md`**

```markdown
You are a research and data assistant.

Ephemeral (this interaction only):
- Run Python via code_execution.
- Read files in /workspace via filesystem.

Durable (persists after this interaction):
- Read and write through the MCP server. Confirm before writing.

Live information:
- Search via google_search.
- Fetch URLs via url_context.

Always say which tool you are using and why.
```

```bash
geap-managed-agents-builder deploy -dir ./my-agent
geap-managed-agents-builder verify -dir ./my-agent -prompt "Summarise https://go.dev in 3 bullets."
geap-managed-agents-builder list            # see all agents in the project
geap-managed-agents-builder delete -dir ./my-agent
```

---

## Using your agent from code

After deploy your agent is a **Google Cloud resource**, not a public URL. Here is how
to call it from an application.

### Resource name

```
projects/{PROJECT_ID}/locations/global/agents/{AGENT_ID}
```

### Auth

Your application needs a Google credential with the `cloud-platform` OAuth scope.

```bash
# Local development
gcloud auth application-default login

# Production (recommended)
# Create a service account, grant it roles/aiplatform.user,
# and use Workload Identity or a key file.
gcloud iam service-accounts create my-agent-caller
gcloud projects add-iam-policy-binding PROJECT_ID \
  --member="serviceAccount:my-agent-caller@PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/aiplatform.user"
```

### Calling the interactions API (Go example)

```go
package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "golang.org/x/oauth2/google"
)

const (
    projectID = "my-gcp-project"
    agentID   = "my-agent"
    apiBase   = "https://aiplatform.googleapis.com/v1beta1"
)

func callAgent(ctx context.Context, prompt string) error {
    client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
    if err != nil {
        return err
    }

    // 1. Create an interaction (background=true → async)
    body, _ := json.Marshal(map[string]any{
        "agent": fmt.Sprintf("projects/%s/locations/global/agents/%s", projectID, agentID),
        "input": []map[string]any{{
            "type":    "user_input",
            "content": []map[string]any{{"type": "text", "text": prompt}},
        }},
        "stream":      false,
        "background":  true,
        "environment": map[string]any{"type": "remote"},
    })

    interactURL := fmt.Sprintf("%s/projects/%s/locations/global/interactions", apiBase, projectID)
    req, _ := http.NewRequestWithContext(ctx, "POST", interactURL, bytes.NewBuffer(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Api-Revision", "2026-05-20")

    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    raw, _ := io.ReadAll(resp.Body)

    var initial map[string]any
    json.Unmarshal(raw, &initial)
    id := initial["id"].(string)

    // 2. Poll until complete
    pollURL := fmt.Sprintf("%s/%s", interactURL, id)
    for {
        time.Sleep(2 * time.Second)
        req, _ = http.NewRequestWithContext(ctx, "GET", pollURL, nil)
        req.Header.Set("Api-Revision", "2026-05-20")
        resp, _ = client.Do(req)
        raw, _ = io.ReadAll(resp.Body)
        resp.Body.Close()

        var result map[string]any
        json.Unmarshal(raw, &result)
        if result["status"] == "in_progress" {
            continue
        }

        // 3. Extract model output from steps
        for _, step := range result["steps"].([]any) {
            s := step.(map[string]any)
            if s["type"] == "model_output" {
                for _, c := range s["content"].([]any) {
                    fmt.Print(c.(map[string]any)["text"])
                }
            }
        }
        fmt.Println()
        return nil
    }
}
```

### Key facts for your application

| | |
|---|---|
| **Interaction URL** | `POST .../projects/{project}/locations/global/interactions` |
| **Interaction ID** | Returned in the initial response; use to poll |
| **Poll interval** | 2s is reasonable; the API is async with no push notification |
| **Auth header** | Bearer token from Google OAuth2 (`cloud-platform` scope) |
| **Session / multi-turn** | Each interaction is independent; there is no built-in conversation thread |
| **No public endpoint** | There is no `https://my-agent.run` — all calls go through the Vertex AI API |

### A2A integration (roadmap)

[A2A (Agent-to-Agent)](https://github.com/a2aproject/A2A) is an open standard
(Google-initiated, 50+ partners including Atlassian, Salesforce, LangChain, MongoDB)
that lets agents advertise capabilities via an **Agent Card** and accept tasks over HTTP
without the caller knowing the underlying platform.

> **Roadmap:** Google is building an A2A bridge for Gemini managed agents. Once it
> ships, every agent deployed by this CLI will automatically get a standard A2A endpoint
> and Agent Card — meaning any A2A-capable client (ADK, LangGraph, Autogen, custom)
> can discover and call your agents without Vertex AI API knowledge or Google OAuth.
>
> Until then, the Vertex AI REST API (shown above) is the integration path.

---

## Tool types

| Tool | What it does | Persists? |
|---|---|---|
| `code_execution` | Python sandbox in the remote environment | No — ephemeral |
| `filesystem` | R/W `/workspace` (seeded from GCS) | No — ephemeral |
| `google_search` | Grounded web search | N/A |
| `url_context` | Fetch and inject URL content at inference time | N/A |
| `mcp_server` | External [MCP](https://modelcontextprotocol.io/) server | **Yes — via the server** |

**Durable integrations go through `mcp_server`.** Drive, BigQuery, databases, GitHub —
anything with an MCP server. The sandbox is ephemeral; the MCP server is not.

**⚠ MCP tokens in `agent.yaml`:** headers like `Authorization: Bearer ...` are secrets.
Do not commit them. Use environment variable substitution in your CI/CD pipeline, or
store them in a secret manager and inject at deploy time.

---

## `agent.yaml` reference

```yaml
id: "my-agent"           # Required. 3–63 chars, lowercase letters/digits/hyphens,
                         # must start with a letter, end with a letter or digit.
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
    headers:             # Optional — keep secrets out of git.
      Authorization: "Bearer ..."
```

### Environment overrides

| Variable | Effect |
|---|---|
| `GEMINI_PROJECT_ID` | Overrides `project_id` in agent.yaml |
| `GEMINI_LOCATION` | Overrides `location` in agent.yaml |

---

## CLI reference

```
geap-managed-agents-builder [flags] <command>

Commands:
  deploy   Create or update the agent (default)
  verify   Send a test prompt and print the response
  delete   Remove the agent
  list     List all agents in the project

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
| [`examples/url-context/`](examples/url-context/) | `url_context` + `google_search`. Fetch and summarise live URLs. |
| [`examples/with-skills/`](examples/with-skills/) | `code_execution` + `filesystem` + GCS-backed skill playbooks. |
| [`examples/mcp-server/`](examples/mcp-server/) | `mcp_server` + `code_execution`. Template for Drive / BigQuery / database integrations. |
| [`examples/full-featured/`](examples/full-featured/) | All five tool types. Reference config. |

---

## Edge cases

| Situation | What happens |
|---|---|
| **Deploy on an existing agent** | Detects via GET, issues PATCH. Idempotent. |
| **Concurrent deploy race (same ID)** | First POST wins; second gets 409 and auto-retries as PATCH. |
| **Invalid agent ID** (uppercase, spaces, trailing hyphen, leading digit, <3 or >63 chars) | Caught locally before any API call. |
| **Wrong location** (anything other than `global`) | Caught locally — the API only accepts `global`. |
| **Unsupported tool type** (e.g. `http`, `drive`) | Caught locally with a list of supported types. |
| **`mcp_server` missing `url`** | Caught locally before deploy. |
| **GCS bucket doesn't exist** | Auto-created in `us-central1` before upload. |
| **Transient API 5xx during LRO poll** | Retried automatically; shown as `!` in output. |
| **`delete` on a non-existent agent** | Returns cleanly — no error. |
| **No `instructions.md`** | Allowed. Agent deployed with empty system prompt. |
| **Context timeout** | Propagated immediately from any in-flight operation. |

---

## Testing

### Unit tests (no GCP credentials needed)

```bash
go test ./src/...
go test -race ./src/...
```

All API calls are mocked with `net/http/httptest`. 43 tests.

### End-to-end tests (real GCP project required)

```bash
gcloud auth application-default login
export GEMINI_PROJECT_ID=my-gcp-project

# All scenarios
go test -tags e2e ./e2e/... -v -timeout 5m

# Specific scenarios
go test -tags e2e ./e2e/... -v -timeout 5m -run TestE2E_Minimal
go test -tags e2e ./e2e/... -v -timeout 5m -run TestE2E_WithSkills
go test -tags e2e ./e2e/... -v -timeout 5m -run TestE2E_DeployIdempotent
go test -tags e2e ./e2e/... -v -timeout 5m -run TestE2E_Delete
```

Each e2e test deploys a uniquely-named agent, runs a real interaction, and deletes the
agent in `t.Cleanup` — even on failure.

---

## Prerequisites

- Go 1.25+
- GCP project with [Vertex AI API](https://console.cloud.google.com/apis/library/aiplatform.googleapis.com) enabled
- `gcloud auth application-default login` (local dev) or a service account with `roles/aiplatform.user` (production)
- GCS bucket writeable by the deploying credential (only for `gcs_bucket` agents)

---

## License

Apache 2.0
