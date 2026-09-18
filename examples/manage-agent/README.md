# manage-agent example: a GCP fleet manager

A managed agent that helps you administer *other* managed agents (and
related GCP resources) in your project: "what agents do I have deployed?",
"show me the config for agent X", "what's running in us-central1-a?".

## Why this needs a companion MCP server

The Agent Platform's remote sandbox has **no ambient GCP
credentials** — it can't call `aiplatform.googleapis.com` on its own, and
`gcloud`/the metadata server are unreachable from inside it. The platform
does expose an undocumented `endpoint` tool type that looks like it's meant
for exactly this, but as of 2026-09 it doesn't work: the model has no way to
discover the real function-call name to invoke it, so every attempt times
out with `"Internal error encountered"` after ~4 minutes with zero
successful tool calls. Don't use it yet.

The documented, working path is `mcp_server`: run your own small MCP server
somewhere with real Google credentials (a service account with
`roles/aiplatform.viewer` + `roles/compute.viewer` is enough), and point the
agent at it. `mcp-server/main.go` in this directory is that server — it
uses Application Default Credentials and exposes:

- `list_agents(project_id, location?)`
- `get_agent(project_id, agent_id, location?)`
- `list_compute_instances(project_id, zone)`
- `get_project_info(project_id)`

**Requires a real `https://` URL with a valid cert.** Plain `http://`
silently fails the same way as the broken `endpoint` tool (no error, no
tool call, `"Internal error encountered"`). The binary has a built-in
`-tls-host` flag that gets a free Let's Encrypt cert via ACME HTTP-01 — no
manual DNS needed if you use a `<public-ip>.nip.io` hostname.

## Deploy the MCP server

### Option A: Cloud Run (recommended)

Needs `roles/run.admin` + `roles/artifactregistry.writer` (or
`roles/owner`/`roles/editor`) on the deploying identity — Cloud Run's HTTPS
ingress is managed by the platform, not a project firewall rule, so it's
not subject to any org firewall-sweeping policy.

```bash
cd examples/manage-agent/mcp-server
gcloud run deploy gcp-fleet-mcp \
  --source . \
  --region us-central1 \
  --no-allow-unauthenticated \
  --set-env-vars MCP_AUTH_TOKEN=$(openssl rand -base64 32)
```

Cloud Run gives you `https://...run.app` with a valid cert automatically —
skip `-tls-host` entirely and just run the binary with default flags
(`-port $PORT`, Cloud Run terminates TLS for you). Use `--no-allow-unauthenticated`
plus the `MCP_AUTH_TOKEN` bearer check as defense in depth, or swap in
Cloud Run's IAM-based auth if you prefer.

### Option B: your own VM / host

```bash
GOOS=linux GOARCH=amd64 go build -o mcp-server ./examples/manage-agent/mcp-server
# copy the binary to your host, then:
MCP_AUTH_TOKEN=$(openssl rand -base64 32) \
  ./mcp-server -tls-host YOUR_PUBLIC_IP.nip.io
```

This opens `:80` for the ACME HTTP-01 challenge and `:443` for the actual
server — both need to be reachable from the internet (Let's Encrypt needs
to reach `:80`).

**Caveat:** if your project has an org policy that auto-deletes
`0.0.0.0/0` ingress firewall rules (some sandboxes/security-hardened
projects do this silently, with no log visibility from a limited service
account), a raw VM will go dark ~15 minutes after you open the port. If
your `list`/`verify` calls start timing out after initially working, check
`gcloud compute firewall-rules list` for your rule still being present. If
it's gone and you don't have permission to fix the policy, use Option A.

## Deploy the agent

Fill in `agent.yaml`'s `url` and `Authorization` header with your server's
real address and token, then:

```bash
go run ./src deploy -dir examples/manage-agent
go run ./src verify -dir examples/manage-agent -prompt "List all managed agents in this project."
```

## Console

Manage all your Vertex AI managed agents (this one included) at:
https://console.cloud.google.com/agent-platform/studio
