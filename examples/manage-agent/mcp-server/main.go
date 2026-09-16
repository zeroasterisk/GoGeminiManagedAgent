// Command mcp-server implements a Model Context Protocol server exposing
// read/administrative tools over a Google Cloud project: listing Vertex AI
// managed agents (built with this repo's builder), inspecting a specific
// agent, and listing Compute Engine instances. It is the backing service
// for the examples/manage-agent example agent.
//
// Auth: uses Application Default Credentials (ADC) on whatever host you run
// it on. Deploy it anywhere you can run a Go binary with a service account
// that has (at minimum) roles/aiplatform.viewer and roles/compute.viewer on
// the target project — Cloud Run, GCE, or your own infra.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/oauth2/google"
)

var (
	host    = flag.String("host", "0.0.0.0", "host to listen on")
	port    = flag.String("port", "8080", "port to listen on (plain HTTP; ignored if -tls-host is set)")
	tlsHost = flag.String("tls-host", "", "if set, serve HTTPS on :443 (and ACME HTTP-01 on :80) using a Let's Encrypt cert for this hostname (e.g. 1.2.3.4.nip.io). Requires the process to be reachable on ports 80 and 443.")
)

// tokenSource lazily resolves Application Default Credentials so a single
// bad/missing credential doesn't crash the whole server at startup.
func tokenSource(ctx context.Context, scopes ...string) (*http.Client, error) {
	creds, err := google.FindDefaultCredentials(ctx, scopes...)
	if err != nil {
		return nil, fmt.Errorf("resolving application default credentials: %w", err)
	}
	return oauth2Client(ctx, creds), nil
}

func oauth2Client(ctx context.Context, creds *google.Credentials) *http.Client {
	return &http.Client{Transport: &oauth2Transport{ctx: ctx, creds: creds}}
}

type oauth2Transport struct {
	ctx   context.Context
	creds *google.Credentials
}

func (t *oauth2Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.creds.TokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("fetching access token: %w", err)
	}
	req2 := req.Clone(req.Context())
	tok.SetAuthHeader(req2)
	return http.DefaultTransport.RoundTrip(req2)
}

func doGet(ctx context.Context, url string, scopes ...string) (map[string]any, error) {
	client, err := tokenSource(ctx, scopes...)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned HTTP %d: %s", url, resp.StatusCode, string(body))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding response from %s: %w", url, err)
	}
	return out, nil
}

const aiplatformScope = "https://www.googleapis.com/auth/cloud-platform"

type ListAgentsParams struct {
	ProjectID string `json:"project_id" jsonschema:"the GCP project ID to list managed agents in"`
	Location  string `json:"location,omitempty" jsonschema:"Vertex AI location, defaults to 'global'"`
}

func listAgents(ctx context.Context, _ *mcp.CallToolRequest, args ListAgentsParams) (*mcp.CallToolResult, any, error) {
	loc := args.Location
	if loc == "" {
		loc = "global"
	}
	url := fmt.Sprintf("https://aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/agents", args.ProjectID, loc)
	data, err := doGet(ctx, url, aiplatformScope)
	if err != nil {
		return errResult(err), nil, nil
	}
	return jsonResult(data), data, nil
}

type GetAgentParams struct {
	ProjectID string `json:"project_id" jsonschema:"the GCP project ID"`
	AgentID   string `json:"agent_id" jsonschema:"the managed agent's ID"`
	Location  string `json:"location,omitempty" jsonschema:"Vertex AI location, defaults to 'global'"`
}

func getAgent(ctx context.Context, _ *mcp.CallToolRequest, args GetAgentParams) (*mcp.CallToolResult, any, error) {
	loc := args.Location
	if loc == "" {
		loc = "global"
	}
	url := fmt.Sprintf("https://aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/agents/%s", args.ProjectID, loc, args.AgentID)
	data, err := doGet(ctx, url, aiplatformScope)
	if err != nil {
		return errResult(err), nil, nil
	}
	return jsonResult(data), data, nil
}

type ListInstancesParams struct {
	ProjectID string `json:"project_id" jsonschema:"the GCP project ID"`
	Zone      string `json:"zone" jsonschema:"Compute Engine zone, e.g. us-central1-a"`
}

func listInstances(ctx context.Context, _ *mcp.CallToolRequest, args ListInstancesParams) (*mcp.CallToolResult, any, error) {
	url := fmt.Sprintf("https://compute.googleapis.com/compute/v1/projects/%s/zones/%s/instances", args.ProjectID, args.Zone)
	data, err := doGet(ctx, url, "https://www.googleapis.com/auth/compute.readonly")
	if err != nil {
		return errResult(err), nil, nil
	}
	return jsonResult(data), data, nil
}

type ProjectInfoParams struct {
	ProjectID string `json:"project_id" jsonschema:"the GCP project ID"`
}

func projectInfo(ctx context.Context, _ *mcp.CallToolRequest, args ProjectInfoParams) (*mcp.CallToolResult, any, error) {
	url := fmt.Sprintf("https://cloudresourcemanager.googleapis.com/v1/projects/%s", args.ProjectID)
	data, err := doGet(ctx, url, "https://www.googleapis.com/auth/cloud-platform.read-only")
	if err != nil {
		return errResult(err), nil, nil
	}
	return jsonResult(data), data, nil
}

func jsonResult(v any) *mcp.CallToolResult {
	b, _ := json.MarshalIndent(v, "", "  ")
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

func errResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

// authMiddleware requires a valid "X-Mcp-Auth-Token" (or custom bearer) header
// when MCP_AUTH_TOKEN is set. We check X-Mcp-Auth-Token first, then fallback to
// Authorization: Bearer <token> if no Google ID token is present.
func authMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		log.Printf("WARNING: MCP_AUTH_TOKEN is not set; server is running with NO AUTH")
		return next
	}
	want := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		customHeader := r.Header.Get("X-Mcp-Auth-Token")
		authHeader := r.Header.Get("Authorization")
		if customHeader == token || authHeader == want {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func main() {
	flag.Parse()
	authToken := os.Getenv("MCP_AUTH_TOKEN")

	server := mcp.NewServer(&mcp.Implementation{Name: "gcp-manage-agent", Version: "v0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_agents",
		Description: "List Vertex AI managed agents (Gemini Enterprise Agent Platform) in a GCP project/location.",
	}, listAgents)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_agent",
		Description: "Get full config/details for one managed agent by ID.",
	}, getAgent)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_compute_instances",
		Description: "List Compute Engine VM instances in a project/zone (e.g. to see agent-hosting infra like GCE-based deployments).",
	}, listInstances)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_project_info",
		Description: "Get basic GCP project metadata (project number, lifecycle state, labels).",
	}, projectInfo)

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	authed := authMiddleware(authToken, handler)

	if *tlsHost != "" {
		// The Gemini Enterprise Agent Platform's mcp_server tool requires
		// https://. autocert gets a real Let's Encrypt cert via HTTP-01,
		// so any public IP + nip.io hostname works with no manual DNS setup.
		certManager := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(*tlsHost),
			Cache:      autocert.DirCache("/var/lib/mcp-server/autocert-cache"),
		}
		go func() {
			log.Printf("ACME HTTP-01 challenge server listening on :80 for host %s", *tlsHost)
			if err := http.ListenAndServe(":80", certManager.HTTPHandler(nil)); err != nil {
				log.Printf("ACME challenge server error: %v", err)
			}
		}()
		srv := &http.Server{
			Addr:      ":443",
			Handler:   authed,
			TLSConfig: certManager.TLSConfig(),
		}
		log.Printf("gcp-manage-agent MCP server listening at https://%s/ (streamable HTTP over TLS)", *tlsHost)
		if err := srv.ListenAndServeTLS("", ""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	addr := fmt.Sprintf("%s:%s", *host, *port)
	log.Printf("gcp-manage-agent MCP server listening at %s (streamable HTTP, POST /)", addr)
	if err := http.ListenAndServe(addr, authed); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
