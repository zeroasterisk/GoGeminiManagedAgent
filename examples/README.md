# Agent Platform Examples

This directory contains standalone examples demonstrating different capabilities and tool configurations on Google Cloud's **Agent Platform**.

Each subdirectory is a complete agent deployment unit consisting of an `agent.yaml` specification and an `instructions.md` prompt.

---

## Example Catalog

| Directory | Core Purpose | Tools Configured |
|---|---|---|
| [`minimal/`](./minimal/) | Simplest possible deployment; fast factual lookup and computation | `google_search`, `code_execution` |
| [`a2a-agent/`](./a2a-agent/) | Inter-agent communication via the A2A (Agent-to-Agent) protocol | `google_search`, `code_execution`, `url_context` |
| [`developer-knowledge-agent/`](./developer-knowledge-agent/) | Grounded Google developer expert combining Google's official Developer Knowledge MCP server with the `retrieving-developer-knowledge` skill | `mcp_server`, `filesystem`, `code_execution`, `google_search` |
| [`url-context/`](./url-context/) | Grounded reading and summarization of arbitrary live URLs | `url_context`, `google_search` |
| [`with-skills/`](./with-skills/) | Mounting domain playbooks (`SKILL.md`) from GCS into the runtime filesystem | `filesystem`, `code_execution` |
| [`manage-agent/`](./manage-agent/) | Dogfooding: an agent backed by an MCP server to inspect and manage other GCP agents | `mcp_server`, `code_execution` |
| [`full-featured/`](./full-featured/) | Reference catalog showcasing every supported tool type in a single definition | `code_execution`, `filesystem`, `google_search`, `url_context`, `mcp_server` |

---

## Deploying Any Example

Set your GCP project ID and deploy directly with the CLI:

```bash
export GEMINI_PROJECT_ID="your-gcp-project-id"

# 1. Deploy the minimal agent
gemini-managed-agents deploy -dir ./examples/minimal

# 2. Verify with a prompt (Interactions API)
gemini-managed-agents verify -dir ./examples/minimal -prompt "What is 15 factorial? Verify with code."

# 3. Verify an A2A-capable agent over the A2A protocol (requires private preview access)
gemini-managed-agents verify -dir ./examples/a2a-agent -protocol a2a -prompt "Summarize Go 1.27 release highlights."
```
