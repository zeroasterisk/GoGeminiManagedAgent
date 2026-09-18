package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/zeroasterisk/GoGeminiManagedAgent/src/config"
	"golang.org/x/oauth2/google"
)

const defaultAPIBase = "https://aiplatform.googleapis.com/v1beta1"

// ── API payload types ────────────────────────────────────────────────────────

// AgentPayload is the request body for Create/Update agent.
type AgentPayload struct {
	ID                string                  `json:"id,omitempty"`
	BaseAgent         string                  `json:"base_agent"`
	Description       string                  `json:"description,omitempty"`
	SystemInstruction string                  `json:"system_instruction,omitempty"`
	Tools             []ToolPayload           `json:"tools,omitempty"`
	BaseEnvironment   *BaseEnvironmentPayload `json:"base_environment,omitempty"`
}

// ToolPayload maps to a single tool in the API.
// Valid types: code_execution, filesystem, google_search, mcp_server, url_context.
type ToolPayload struct {
	Type    string            `json:"type"`
	Name    string            `json:"name,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// BaseEnvironmentPayload configures the remote sandbox environment.
type BaseEnvironmentPayload struct {
	Type    string                     `json:"type"` // always "remote"
	Sources []EnvironmentSourcePayload `json:"sources,omitempty"`
	Network *NetworkConfigPayload      `json:"network,omitempty"`
}

// EnvironmentSourcePayload mounts a GCS prefix into the sandbox.
type EnvironmentSourcePayload struct {
	Type   string `json:"type"` // "gcs"
	Source string `json:"source"`
	Target string `json:"target"`
}

// NetworkConfigPayload controls outbound network from the sandbox.
// Currently only Domain "*" is supported.
type NetworkConfigPayload struct {
	Allowlist []NetworkAllowlistEntry `json:"allowlist,omitempty"`
}

// NetworkAllowlistEntry is a single domain rule.
type NetworkAllowlistEntry struct {
	Domain string `json:"domain"`
}

// LROResponse is a long-running operation response.
type LROResponse struct {
	Name     string                 `json:"name"`
	Done     bool                   `json:"done"`
	Error    *LROError              `json:"error,omitempty"`
	Response map[string]interface{} `json:"response,omitempty"`
}

// LROError is the error field inside an LROResponse.
type LROError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// InteractionPayload is the request body for creating an interaction.
type InteractionPayload struct {
	Agent       string             `json:"agent"`
	Input       []InteractionInput `json:"input"`
	Environment *InteractionEnv    `json:"environment,omitempty"`
	Stream      bool               `json:"stream"`
	Background  bool               `json:"background"`
}

// InteractionInput is one turn of input.
type InteractionInput struct {
	Type    string               `json:"type"` // "user_input"
	Content []InteractionContent `json:"content"`
}

// InteractionContent is a single content block.
type InteractionContent struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// InteractionEnv selects the execution environment.
type InteractionEnv struct {
	Type string `json:"type"` // "remote"
}

// InteractionResponse is the polled result of an interaction.
type InteractionResponse struct {
	ID            string            `json:"id"`
	Status        string            `json:"status"`
	EnvironmentID string            `json:"environment_id"`
	Steps         []InteractionStep `json:"steps"`
}

// InteractionStep is one step in the agent's execution trace.
type InteractionStep struct {
	Type    string               `json:"type"`
	Name    string               `json:"name,omitempty"`
	Content []InteractionContent `json:"content,omitempty"`
}

// ── Builder ──────────────────────────────────────────────────────────────────

// Builder orchestrates deploying and interacting with a Gemini Managed Agent.
type Builder struct {
	cfg                  *config.AgentConfig
	dir                  string
	httpClient           *http.Client  // nil → use google.DefaultClient (ADC)
	apiBase              string        // override for tests
	lroPollInterval      time.Duration // override for tests; default 5s
	interactPollInterval time.Duration // override for tests; default 2s
}

// NewBuilder creates a Builder that authenticates via Application Default Credentials.
func NewBuilder(cfg *config.AgentConfig, dir string) *Builder {
	return &Builder{cfg: cfg, dir: dir, apiBase: defaultAPIBase}
}

// newBuilderWithClient creates a Builder with an injected HTTP client and base URL.
// Intended for tests using httptest.Server.
func newBuilderWithClient(cfg *config.AgentConfig, dir string, client *http.Client, apiBase string) *Builder {
	return &Builder{cfg: cfg, dir: dir, httpClient: client, apiBase: apiBase}
}

func (b *Builder) getLROPollInterval() time.Duration {
	if b.lroPollInterval > 0 {
		return b.lroPollInterval
	}
	return 5 * time.Second
}

func (b *Builder) getInteractPollInterval() time.Duration {
	if b.interactPollInterval > 0 {
		return b.interactPollInterval
	}
	return 2 * time.Second
}

// getClient returns the HTTP client, initialising from ADC on first call.
func (b *Builder) getClient(ctx context.Context) (*http.Client, error) {
	if b.httpClient != nil {
		return b.httpClient, nil
	}
	client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("google.DefaultClient: %w", err)
	}
	return client, nil
}

// agentsURL returns the base URL for the agents collection.
func (b *Builder) agentsURL() string {
	return fmt.Sprintf("%s/projects/%s/locations/%s/agents", b.apiBase, b.cfg.ProjectID, b.cfg.Location)
}

// ── List response types ───────────────────────────────────────────────────────

// AgentSummary is a single entry from the List agents response.
type AgentSummary struct {
	ID          string        `json:"id"`
	Description string        `json:"description"`
	BaseAgent   string        `json:"base_agent"`
	Created     string        `json:"created"`
	Updated     string        `json:"updated"`
	Tools       []ToolPayload `json:"tools"`
}

type listAgentsResponse struct {
	Agents        []AgentSummary `json:"agents"`
	NextPageToken string         `json:"nextPageToken"`
}

// ── Public operations ────────────────────────────────────────────────────────

// List returns all agents in the project, following pagination automatically.
func (b *Builder) List(ctx context.Context) ([]AgentSummary, error) {
	client, err := b.getClient(ctx)
	if err != nil {
		return nil, err
	}

	var all []AgentSummary
	pageToken := ""

	for {
		url := b.agentsURL()
		if pageToken != "" {
			url += "?pageToken=" + pageToken
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("list failed (%d): %s", resp.StatusCode, string(body))
		}

		var page listAgentsResponse
		if err = json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("parsing list response: %w", err)
		}
		all = append(all, page.Agents...)

		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}

	return all, nil
}

// BuildAndDeploy reads the agent directory, uploads assets to GCS if
// configured, then creates or updates the agent (idempotent).
func (b *Builder) BuildAndDeploy(ctx context.Context) error {
	instructions, err := b.readInstructions()
	if err != nil {
		return fmt.Errorf("reading instructions: %w", err)
	}

	var sources []EnvironmentSourcePayload
	if b.cfg.GCSBucket != "" {
		if err := b.ensureBucketExists(ctx); err != nil {
			return fmt.Errorf("ensuring GCS bucket: %w", err)
		}
		fmt.Printf("Uploading files to gs://%s/%s/ ...\n", b.cfg.GCSBucket, b.cfg.ID)
		if err := b.uploadToGCS(ctx); err != nil {
			return fmt.Errorf("uploading to GCS: %w", err)
		}
		sources = append(sources, EnvironmentSourcePayload{
			Type:   "gcs",
			Source: fmt.Sprintf("gs://%s/%s", b.cfg.GCSBucket, b.cfg.ID),
			Target: "/workspace",
		})
	}

	payload := AgentPayload{
		ID:                b.cfg.ID,
		BaseAgent:         b.cfg.BaseAgent,
		Description:       b.cfg.Description,
		SystemInstruction: instructions,
	}
	for _, t := range b.cfg.Tools {
		payload.Tools = append(payload.Tools, ToolPayload{
			Type:    t.Type,
			Name:    t.Name,
			URL:     t.URL,
			Headers: t.Headers,
		})
	}
	payload.BaseEnvironment = &BaseEnvironmentPayload{
		Type:    "remote",
		Sources: sources,
		Network: &NetworkConfigPayload{Allowlist: b.buildAllowlist()},
	}

	return b.deployAgent(ctx, payload)
}

// Delete removes the agent. Returns nil if the agent does not exist.
func (b *Builder) Delete(ctx context.Context) error {
	client, err := b.getClient(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/%s", b.agentsURL(), b.cfg.ID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		fmt.Printf("Agent %s does not exist, nothing to delete.\n", b.cfg.ID)
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("delete failed (%d): %s", resp.StatusCode, string(body))
	}

	// DELETE returns an LRO
	var lro LROResponse
	if err = json.Unmarshal(body, &lro); err != nil {
		// Some responses may be empty on success
		fmt.Printf("Agent %s deletion initiated.\n", b.cfg.ID)
		return nil
	}
	if lro.Done {
		fmt.Printf("Agent %s deleted.\n", b.cfg.ID)
		return nil
	}
	fmt.Printf("Waiting for deletion of %s...\n", b.cfg.ID)
	return b.waitForLRO(ctx, client, lro.Name)
}

// Interact sends prompt to the deployed agent and prints the response.
func (b *Builder) Interact(ctx context.Context, prompt string, verbose bool) error {
	_, err := b.InteractWithResult(ctx, prompt, verbose)
	return err
}

// InteractWithResult sends prompt to the deployed agent, prints the response, and returns the aggregated text.
func (b *Builder) InteractWithResult(ctx context.Context, prompt string, verbose bool) (string, error) {
	client, err := b.getClient(ctx)
	if err != nil {
		return "", err
	}

	agentRef := fmt.Sprintf("projects/%s/locations/%s/agents/%s", b.cfg.ProjectID, b.cfg.Location, b.cfg.ID)
	payload := InteractionPayload{
		Agent: agentRef,
		Input: []InteractionInput{
			{
				Type:    "user_input",
				Content: []InteractionContent{{Type: "text", Text: prompt}},
			},
		},
		Stream:      false,
		Background:  true,
		Environment: &InteractionEnv{Type: "remote"},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	interactURL := fmt.Sprintf("%s/projects/%s/locations/%s/interactions", b.apiBase, b.cfg.ProjectID, b.cfg.Location)

	fmt.Printf("Sending prompt to agent %s...\n", b.cfg.ID)
	req, err := http.NewRequestWithContext(ctx, "POST", interactURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Revision", "2026-05-20")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("interaction failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var initial map[string]interface{}
	if err = json.Unmarshal(respBody, &initial); err != nil {
		return "", fmt.Errorf("parsing initial response: %w", err)
	}

	interactionID, ok := initial["id"].(string)
	if !ok {
		return "", fmt.Errorf("response did not contain interaction ID: %s", string(respBody))
	}

	fmt.Printf("Interaction %s created. Polling...\n", interactionID)

	pollURL := fmt.Sprintf("%s/%s", interactURL, interactionID)
	ticker := time.NewTicker(b.getInteractPollInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			pollReq, err := http.NewRequestWithContext(ctx, "GET", pollURL, nil)
			if err != nil {
				return "", err
			}
			pollReq.Header.Set("Api-Revision", "2026-05-20")

			pollResp, err := client.Do(pollReq)
			if err != nil {
				return "", err
			}
			pollBody, _ := io.ReadAll(pollResp.Body)
			pollResp.Body.Close()

			if pollResp.StatusCode >= 400 {
				return "", fmt.Errorf("polling failed (%d): %s", pollResp.StatusCode, string(pollBody))
			}

			var result map[string]interface{}
			if err = json.Unmarshal(pollBody, &result); err != nil {
				return "", fmt.Errorf("parsing poll response: %w", err)
			}

			status, _ := result["status"].(string)
			if status == "in_progress" {
				fmt.Print(".")
				continue
			}

			fmt.Println()
			if verbose {
				prettyJSON, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(prettyJSON))
			} else {
				var response InteractionResponse
				if err = json.Unmarshal(pollBody, &response); err != nil {
					return "", fmt.Errorf("parsing final response: %w", err)
				}
				fmt.Println("\nAgent Response:")
				var responseText string
				for _, step := range response.Steps {
					if step.Type == "model_output" {
						for _, content := range step.Content {
							if content.Type == "text" {
								fmt.Print(content.Text)
								responseText += content.Text
							}
						}
					}
				}
				fmt.Println()
				return responseText, nil
			}
			return "", nil
		}
	}
}

// ── Internal helpers ─────────────────────────────────────────────────────────

func (b *Builder) buildAllowlist() []NetworkAllowlistEntry {
	if len(b.cfg.Network.Allowlist) == 0 {
		return []NetworkAllowlistEntry{{Domain: "*"}}
	}
	out := make([]NetworkAllowlistEntry, len(b.cfg.Network.Allowlist))
	for i, d := range b.cfg.Network.Allowlist {
		out[i] = NetworkAllowlistEntry{Domain: d}
	}
	return out
}

func (b *Builder) readInstructions() (string, error) {
	data, err := os.ReadFile(filepath.Join(b.dir, "instructions.md"))
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

func (b *Builder) ensureBucketExists(ctx context.Context) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	bucket := client.Bucket(b.cfg.GCSBucket)
	if _, err = bucket.Attrs(ctx); errors.Is(err, storage.ErrBucketNotExist) {
		location := b.cfg.Location
		if location == "global" || location == "" {
			location = "us-central1"
		}
		fmt.Printf("Creating GCS bucket %s in %s...\n", b.cfg.GCSBucket, location)
		if err = bucket.Create(ctx, b.cfg.ProjectID, &storage.BucketAttrs{Location: location}); err != nil {
			return fmt.Errorf("creating bucket %s: %w", b.cfg.GCSBucket, err)
		}
		fmt.Printf("Bucket %s created.\n", b.cfg.GCSBucket)
		return nil
	}
	return err
}

func (b *Builder) uploadToGCS(ctx context.Context) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	bucket := client.Bucket(b.cfg.GCSBucket)

	return filepath.Walk(b.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		relPath, err := filepath.Rel(b.dir, path)
		if err != nil {
			return err
		}
		// Skip deployment config itself and hidden files
		if relPath == "agent.yaml" || strings.HasPrefix(relPath, ".") {
			return nil
		}
		fmt.Printf("  Uploading %s ...\n", relPath)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		wc := bucket.Object(filepath.Join(b.cfg.ID, relPath)).NewWriter(ctx)
		if _, err = io.Copy(wc, f); err != nil {
			return err
		}
		return wc.Close()
	})
}

func (b *Builder) deployAgent(ctx context.Context, payload AgentPayload) error {
	client, err := b.getClient(ctx)
	if err != nil {
		return err
	}

	exists, err := b.agentExists(ctx, client)
	if err != nil {
		return fmt.Errorf("checking agent existence: %w", err)
	}

	var reqURL, method string
	if exists {
		method = "PATCH"
		// Build update_mask dynamically. Only include base_environment when
		// it is actually set — the API rejects a null value for that field.
		mask := "description,system_instruction,tools"
		if payload.BaseEnvironment != nil {
			mask += ",base_environment"
		}
		reqURL = fmt.Sprintf("%s/%s?update_mask=%s", b.agentsURL(), b.cfg.ID, mask)
		payload.ID = ""
	} else {
		method = "POST"
		reqURL = b.agentsURL()
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	fmt.Printf("%s agent %s...\n", method, b.cfg.ID)
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// 409 ALREADY_EXISTS: a concurrent deploy won the race on POST.
	// Re-run as PATCH so the caller's intent (create-or-update) is honoured.
	if resp.StatusCode == http.StatusConflict && method == "POST" {
		fmt.Printf("Agent %s already exists (race); switching to PATCH...\n", b.cfg.ID)
		payload.ID = ""
		patchURL := fmt.Sprintf("%s/%s?update_mask=description,system_instruction,tools,base_environment", b.agentsURL(), b.cfg.ID)
		if payload.BaseEnvironment == nil {
			patchURL = fmt.Sprintf("%s/%s?update_mask=description,system_instruction,tools", b.agentsURL(), b.cfg.ID)
		}
		payloadBytes, _ = json.Marshal(payload)
		req, err = http.NewRequestWithContext(ctx, "PATCH", patchURL, bytes.NewBuffer(payloadBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err = client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		respBody, _ = io.ReadAll(resp.Body)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API call failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var lro LROResponse
	if err = json.Unmarshal(respBody, &lro); err != nil {
		return fmt.Errorf("parsing LRO response: %w", err)
	}
	if lro.Done {
		if lro.Error != nil {
			return fmt.Errorf("operation failed: %s", lro.Error.Message)
		}
		fmt.Println("Agent deployed.")
		return nil
	}

	fmt.Printf("Operation %s in progress...\n", lro.Name)
	return b.waitForLRO(ctx, client, lro.Name)
}

func (b *Builder) agentExists(ctx context.Context, client *http.Client) (bool, error) {
	url := fmt.Sprintf("%s/%s", b.agentsURL(), b.cfg.ID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
}

func (b *Builder) waitForLRO(ctx context.Context, client *http.Client, opName string) error {
	url := fmt.Sprintf("%s/%s", b.apiBase, opName)
	ticker := time.NewTicker(b.getLROPollInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			// Retry on transient server errors (5xx) — the API occasionally
			// returns DEADLINE_EXCEEDED from internal dependencies during LRO poll.
			if resp.StatusCode >= 500 {
				fmt.Print("!")
				continue
			}
			if resp.StatusCode >= 400 {
				return fmt.Errorf("LRO poll failed (%d): %s", resp.StatusCode, string(body))
			}

			var lro LROResponse
			if err = json.Unmarshal(body, &lro); err != nil {
				return fmt.Errorf("parsing LRO: %w", err)
			}
			if lro.Done {
				if lro.Error != nil {
					return fmt.Errorf("operation failed: %s", lro.Error.Message)
				}
				fmt.Println("Done.")
				return nil
			}
			fmt.Print(".")
		}
	}
}
