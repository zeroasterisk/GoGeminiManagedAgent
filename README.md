# GoGeminiManagedAgent

The fastest path to a running [Gemini Enterprise Managed Agent](https://cloud.google.com/gemini/docs/managed-agents):
two files, one command.

```
my-agent/agent.yaml + instructions.md  →  deploy  →  live agent
```

---

## What this is — and what it isn't

**[Gemini Enterprise Agent Platform (GEAP)](https://cloud.google.com/gemini/docs/managed-agents)**
is Google's fully-managed agent runtime. It provisions sandboxes on demand, executes
tool calls, and handles the infrastructure. You bring the definition; Google runs it.

There are two other tools in this space. This one occupies a different position than
either of them:

| Tool | What it does | Who it's for |
|---|---|---|
| **[`google-agents-cli`](https://github.com/google/agents-cli)** + ADK | Full agent development lifecycle — scaffold Python/TypeScript code, evals, deploy to Cloud Run / GKE, publish to Gemini Enterprise | Developers building agents *as code* |
| **[`gemini-agents-api` skill](https://github.com/google/skills/tree/main/skills/cloud/gemini-agents-api)** | Reference doc: the exact API calls to manage agents | AI coding agents that need the raw API shape |
| **This tool** | `terraform apply` for the managed-agents control plane — your agent definition lives in plain files, one command deploys or updates it | Anyone who wants a live agent without writing code or hand-crafting JSON |

The inspiration is [`vercel deploy`](https://vercel.com/blog/introducing-eve) — not the
full `eve` runtime (GEAP provides the equivalent), but the deploy step: a directory *is*
the deployment unit, a CLI is all you need, and re-running it is always safe.

**What this tool does that the alternatives don't:**

- `agent.yaml` + `instructions.md` = your agent in git, diffable, reviewable, portable
- One command handles POST-or-PATCH, LRO polling, and GCS file upload atomically
- Config validated locally before any API call (bad IDs, wrong location, unsupported tools)
- `list` shows everything deployed in your project
- e2e test harness: `go test -tags e2e` verifies real deployments in CI

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
│  ⚠ No public URL. Callable via the Interactions API or A2A —         │
│    both are the Vertex AI REST API with a Google OAuth token         │
│    (service account or ADC). See "Talk to it" below.                 │
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

`verify` speaks the raw Interactions API by default. To talk to the same agent
over **A2A** instead — the standard
[A2A](https://github.com/a2aproject/A2A) protocol any A2A client understands —
pass `-protocol a2a`:

```bash
GEMINI_PROJECT_ID=my-gcp-project \
  geap-managed-agents-builder verify -dir ./examples/minimal -protocol a2a \
  -prompt "What is 12 factorial? Use code."
```

Each managed agent is exposed at
`.../agents/{id}/a2a/v1` (A2A v1, HTTP+JSON), so any A2A-capable client
can call your deployed agents. This CLI uses the
[a2a-go](https://github.com/a2aproject/a2a-go) SDK under the hood.

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

### Official SDK (Python / TypeScript)

The official `google-genai >= 2.0.0` SDK has first-class support for the Interactions API:

```bash
pip install "google-genai>=2.0.0"
```

```python
import os
from google import genai

os.environ["GOOGLE_GENAI_USE_ENTERPRISE"] = "true"
os.environ["GOOGLE_CLOUD_PROJECT"] = "my-gcp-project"
os.environ["GOOGLE_CLOUD_LOCATION"] = "global"

client = genai.Client()

AGENT = "projects/my-gcp-project/locations/global/agents/my-agent"

# Single turn
turn1 = client.interactions.create(
    agent=AGENT,
    input="My name is Alan. What is 12 factorial?",
    store=True,          # persist this turn for multi-turn follow-up
)
print(turn1.steps[-1].content[0].text)

# Multi-turn: continue from the previous interaction
turn2 = client.interactions.create(
    agent=AGENT,
    input="What was my name again?",
    previous_interaction_id=turn1.id,
)
print(turn2.steps[-1].content[0].text)  # "Your name is Alan."
```

> The legacy SDKs (`google-cloud-aiplatform`, `google-generativeai`) do **not** support
> the Interactions API. Use `google-genai >= 2.0.0`.

### Auth

Your application needs a Google credential with the `cloud-platform` OAuth scope.

```bash
# Local development
gcloud auth application-default login

# Production (recommended): service account with roles/aiplatform.user
gcloud iam service-accounts create my-agent-caller
gcloud projects add-iam-policy-binding PROJECT_ID \
  --member="serviceAccount:my-agent-caller@PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/aiplatform.user"
```

### Calling the Interactions API directly (Go / any language)

For Go or any language without an official SDK, use the REST API directly:

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

// sendTurn sends one turn and returns the interaction ID and the agent's reply.
// Pass prevID="" for the first turn; pass the previous turn's ID for follow-ups.
func sendTurn(ctx context.Context, client *http.Client, prompt, prevID string) (id, reply string, err error) {
    payload := map[string]any{
        "agent": fmt.Sprintf("projects/%s/locations/global/agents/%s", projectID, agentID),
        "input": []map[string]any{{
            "type":    "user_input",
            "content": []map[string]any{{"type": "text", "text": prompt}},
        }},
        "store":       true,        // persist for multi-turn follow-up
        "background":  true,        // required for managed agents
        "stream":      false,
        "environment": map[string]any{"type": "remote"},
    }
    if prevID != "" {
        payload["previous_interaction_id"] = prevID
    }

    body, _ := json.Marshal(payload)
    interactURL := fmt.Sprintf("%s/projects/%s/locations/global/interactions", apiBase, projectID)
    req, _ := http.NewRequestWithContext(ctx, "POST", interactURL, bytes.NewBuffer(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Api-Revision", "2026-05-20")

    resp, err := client.Do(req)
    if err != nil {
        return "", "", err
    }
    defer resp.Body.Close()
    raw, _ := io.ReadAll(resp.Body)

    var initial map[string]any
    json.Unmarshal(raw, &initial)
    id = initial["id"].(string)

    // Poll until complete
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
        for _, step := range result["steps"].([]any) {
            s := step.(map[string]any)
            if s["type"] == "model_output" {
                for _, c := range s["content"].([]any) {
                    reply += c.(map[string]any)["text"].(string)
                }
            }
        }
        return id, reply, nil
    }
}

func main() {
    ctx := context.Background()
    client, _ := google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform")

    // Turn 1
    id1, reply1, _ := sendTurn(ctx, client, "My name is Alan. What is 12 factorial?", "")
    fmt.Println(reply1)

    // Turn 2 — agent remembers "Alan" from turn 1
    _, reply2, _ := sendTurn(ctx, client, "What was my name?", id1)
    fmt.Println(reply2)
}
```

### Key facts for your application

| | |
|---|---|
| **Interaction URL** | `POST .../projects/{project}/locations/global/interactions` |
| **Interaction ID** | Returned in the initial response; use to poll and for multi-turn |
| **Multi-turn** | Pass `store: true` on turn 1, then `previous_interaction_id: <id>` on follow-ups |
| **Streaming** | `verify -protocol a2a` streams via `message:stream` (SSE) |
| **Poll interval** | 2s is reasonable; managed agents require `background: true` |
| **Auth** | Bearer token from Google OAuth2 (`cloud-platform` scope) |
| **No public endpoint** | No `https://my-agent.run` — all calls go through the Vertex AI API |
| **Official SDK** | `google-genai >= 2.0.0` with `GOOGLE_GENAI_USE_ENTERPRISE=true` |

### Known gaps vs. the full API

This CLI covers the **control plane** (create/update/delete/list agents). The full
Interactions API has capabilities not yet exposed here:

| Feature | API support | This CLI |
|---|---|---|
| Single-turn interactions | ✓ | ✓ via `verify` (interactions or `-protocol a2a`) |
| Multi-turn (`previous_interaction_id`) | ✓ | Not in `verify` |
| Streaming (SSE) | ✓ | ✓ via `verify -protocol a2a` |
| A2A (`message:stream`) | ✓ | ✓ via `verify -protocol a2a` |
| `skill_registry` GCS source type | ✓ | GCS only |
| Function calling tools | ✓ | N/A (agent-side) |

### A2A integration

> **Note on Availability (as of September 2026):**
> The A2A endpoints (`/a2a/v1/...`) on managed agents are currently in **private preview** and not accessible to all Google Cloud projects. If a project has not been enrolled in the private preview, requests against these endpoints will fail. The Interactions API (`-protocol interactions`, default) is the standard method for general access.

[A2A (Agent-to-Agent)](https://github.com/a2aproject/A2A) is an open standard
(Google-initiated, 50+ partners including Atlassian, Salesforce, LangChain, MongoDB)
that lets agents advertise capabilities and accept tasks over HTTP without the caller
knowing the underlying platform.

Every agent deployed by this CLI is reachable as an A2A v1 (HTTP+JSON) endpoint at:

```
https://aiplatform.googleapis.com/v1beta1/projects/{project}/locations/global/agents/{id}/a2a/v1
```

Any A2A client can call it. This CLI uses the
[a2a-go](https://github.com/a2aproject/a2a-go) SDK — `verify -protocol a2a` sends
`message:stream` and prints the reply as it streams in (SSE). Auth is the same Google
OAuth `cloud-platform` token as every other call; the SDK's REST transport is handed a
credentialed `http.Client`.

Multi-turn over A2A (task/context continuation) is not yet wired into `verify`.

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
geap-managed-agents-builder [command] [flags]

Commands:
  deploy   Create or update the agent (default)
  verify   Send a test prompt and print the response
  delete   Remove the agent
  list     List all agents in the project

Flags:
  -dir string       Agent config directory (default ".")
  -prompt string    Prompt for the verify command (default "Hello")
  -protocol string  Transport for verify: "interactions" (default) or "a2a"
  -verbose          Print raw JSON response (verify only)
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

All API calls (Interactions and A2A) are mocked with `net/http/httptest`.

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

# Same as TestE2E_Minimal but over A2A (message:stream)
go test -tags e2e ./e2e/... -v -timeout 5m -run TestE2E_MinimalA2A
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
