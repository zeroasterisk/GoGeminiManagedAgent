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

// AgentPayload represents the JSON payload for the CreateAgent API
type AgentPayload struct {
	ID                string                  `json:"id,omitempty"`
	BaseAgent         string                  `json:"base_agent"`
	Description       string                  `json:"description,omitempty"`
	SystemInstruction string                  `json:"system_instruction,omitempty"`
	Tools             []ToolPayload           `json:"tools,omitempty"`
	BaseEnvironment   *BaseEnvironmentPayload `json:"base_environment,omitempty"`
}

type ToolPayload struct {
	Type    string            `json:"type"`
	Name    string            `json:"name,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type BaseEnvironmentPayload struct {
	Type    string                     `json:"type"` // "remote"
	Sources []EnvironmentSourcePayload `json:"sources,omitempty"`
	Network *NetworkConfigPayload      `json:"network,omitempty"`
}

type EnvironmentSourcePayload struct {
	Type   string `json:"type"` // "gcs"
	Source string `json:"source"`
	Target string `json:"target"`
}

type NetworkConfigPayload struct {
	Allowlist []NetworkAllowlistEntryPayload `json:"allowlist,omitempty"`
}

type NetworkAllowlistEntryPayload struct {
	Domain string `json:"domain"`
}

// LROResponse represents the response from a long-running operation
type LROResponse struct {
	Name     string                 `json:"name"`
	Done     bool                   `json:"done"`
	Error    *LROError              `json:"error,omitempty"`
	Response map[string]interface{} `json:"response,omitempty"`
}

type LROError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// InteractionPayload represents the payload for the Interact API
type InteractionPayload struct {
	Agent       string             `json:"agent"`
	Input       []InteractionInput `json:"input"`
	Environment *InteractionEnv    `json:"environment,omitempty"`
	Stream      bool               `json:"stream"`
	Background  bool               `json:"background"`
}

type InteractionInput struct {
	Type    string               `json:"type"` // "user_input"
	Content []InteractionContent `json:"content"`
}

type InteractionContent struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

type InteractionEnv struct {
	Type string `json:"type"` // "remote"
}

// InteractionResponse represents the response from the Interact API
type InteractionResponse struct {
	ID            string            `json:"id"`
	Status        string            `json:"status"`
	EnvironmentID string            `json:"environment_id"`
	Steps         []InteractionStep `json:"steps"`
}

type InteractionStep struct {
	Type    string               `json:"type"`           // "user_input", "model_output", "function_call", "function_response"
	Name    string               `json:"name,omitempty"` // for function_call
	Content []InteractionContent `json:"content,omitempty"`
}

// Builder handles the deployment of the agent
type Builder struct {
	cfg          *config.AgentConfig
	dir          string
	httpClient   *http.Client  // injectable for testing; nil means use google.DefaultClient
	apiBase      string        // injectable for testing; empty means use defaultAPIBase
	lroPollInterval time.Duration // injectable for testing; zero means 5s default
	interactPollInterval time.Duration // injectable for testing; zero means 2s default
}

// NewBuilder creates a new Builder using Google Application Default Credentials
func NewBuilder(cfg *config.AgentConfig, dir string) *Builder {
	return &Builder{cfg: cfg, dir: dir, apiBase: defaultAPIBase}
}

// newBuilderWithClient creates a Builder with an explicit HTTP client and API base URL.
// Used in tests to inject an httptest server.
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

// getClient returns the HTTP client, creating one from ADC if not already set.
func (b *Builder) getClient(ctx context.Context) (*http.Client, error) {
	if b.httpClient != nil {
		return b.httpClient, nil
	}
	client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to get default client: %v", err)
	}
	return client, nil
}

// BuildAndDeploy reads the agent config directory, uploads assets to GCS if
// configured, then creates or updates the agent via the Gemini API.
func (b *Builder) BuildAndDeploy(ctx context.Context) error {
	instructions, err := b.readInstructions()
	if err != nil {
		return fmt.Errorf("failed to read instructions: %v", err)
	}

	var sources []EnvironmentSourcePayload
	if b.cfg.GCSBucket != "" {
		if err := b.ensureBucketExists(ctx); err != nil {
			return fmt.Errorf("failed to ensure GCS bucket exists: %v", err)
		}
		fmt.Printf("Uploading files to gs://%s/%s/ ...\n", b.cfg.GCSBucket, b.cfg.ID)
		if err := b.uploadToGCS(ctx); err != nil {
			return fmt.Errorf("failed to upload to GCS: %v", err)
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
	if len(sources) > 0 {
		payload.BaseEnvironment = &BaseEnvironmentPayload{
			Type:    "remote",
			Sources: sources,
			Network: &NetworkConfigPayload{
				Allowlist: []NetworkAllowlistEntryPayload{{Domain: "*"}},
			},
		}
	}

	return b.deployAgent(ctx, payload)
}

func (b *Builder) readInstructions() (string, error) {
	filename := filepath.Join(b.dir, "instructions.md")
	data, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (b *Builder) ensureBucketExists(ctx context.Context) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	bucket := client.Bucket(b.cfg.GCSBucket)
	_, err = bucket.Attrs(ctx)
	if errors.Is(err, storage.ErrBucketNotExist) {
		location := b.cfg.Location
		if location == "global" || location == "" {
			location = "us-central1"
		}
		fmt.Printf("Bucket %s does not exist. Creating it in %s...\n", b.cfg.GCSBucket, location)
		if err = bucket.Create(ctx, b.cfg.ProjectID, &storage.BucketAttrs{Location: location}); err != nil {
			return fmt.Errorf("failed to create bucket %s: %v", b.cfg.GCSBucket, err)
		}
		fmt.Printf("Bucket %s created successfully.\n", b.cfg.GCSBucket)
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
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(b.dir, path)
		if err != nil {
			return err
		}

		// Skip agent.yaml (deployment config); upload everything else
		// including instructions.md and skills/ so the agent runtime can read them.
		if relPath == "agent.yaml" {
			return nil
		}
		// Skip hidden files/dirs (e.g. .git)
		if strings.HasPrefix(relPath, ".") {
			return nil
		}

		fmt.Printf("  Uploading %s ...\n", relPath)

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		objName := filepath.Join(b.cfg.ID, relPath)
		wc := bucket.Object(objName).NewWriter(ctx)
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

	agentsURL := fmt.Sprintf("%s/projects/%s/locations/%s/agents", b.apiBase, b.cfg.ProjectID, b.cfg.Location)

	exists, err := b.agentExists(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to check if agent exists: %v", err)
	}

	var reqURL, method string
	if exists {
		method = "PATCH"
		reqURL = fmt.Sprintf("%s/%s?update_mask=description,system_instruction,tools,base_environment", agentsURL, b.cfg.ID)
		payload.ID = "" // Omit ID from body for PATCH
	} else {
		method = "POST"
		reqURL = agentsURL
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	fmt.Printf("%s Agent %s ...\n", method, b.cfg.ID)
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
	if resp.StatusCode >= 400 {
		return fmt.Errorf("API call failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var lro LROResponse
	if err = json.Unmarshal(respBody, &lro); err != nil {
		return fmt.Errorf("failed to parse LRO response: %v", err)
	}

	if lro.Done {
		if lro.Error != nil {
			return fmt.Errorf("operation failed: %s", lro.Error.Message)
		}
		fmt.Println("Agent deployed immediately.")
		return nil
	}

	fmt.Printf("Waiting for operation %s to complete...\n", lro.Name)
	return b.waitForLRO(ctx, client, lro.Name)
}

func (b *Builder) agentExists(ctx context.Context, client *http.Client) (bool, error) {
	url := fmt.Sprintf("%s/projects/%s/locations/%s/agents/%s", b.apiBase, b.cfg.ProjectID, b.cfg.Location, b.cfg.ID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	return false, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(respBody))
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

			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close() // explicit close in loop; not deferred

			if resp.StatusCode >= 400 {
				return fmt.Errorf("failed to get operation status (%d): %s", resp.StatusCode, string(respBody))
			}

			var lro LROResponse
			if err = json.Unmarshal(respBody, &lro); err != nil {
				return err
			}

			if lro.Done {
				if lro.Error != nil {
					return fmt.Errorf("operation failed: %s", lro.Error.Message)
				}
				fmt.Println("Agent deployed successfully.")
				return nil
			}
			fmt.Print(".")
		}
	}
}

// Interact sends a prompt to the deployed agent and polls for the response.
func (b *Builder) Interact(ctx context.Context, prompt string, verbose bool) error {
	client, err := b.getClient(ctx)
	if err != nil {
		return err
	}

	agentResourceName := fmt.Sprintf("projects/%s/locations/%s/agents/%s", b.cfg.ProjectID, b.cfg.Location, b.cfg.ID)

	payload := InteractionPayload{
		Agent: agentResourceName,
		Input: []InteractionInput{
			{
				Type: "user_input",
				Content: []InteractionContent{
					{Type: "text", Text: prompt},
				},
			},
		},
		Stream:      false,
		Background:  true,
		Environment: &InteractionEnv{Type: "remote"},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	interactURL := fmt.Sprintf("%s/projects/%s/locations/%s/interactions", b.apiBase, b.cfg.ProjectID, b.cfg.Location)

	fmt.Printf("Sending prompt to agent %s...\n", b.cfg.ID)
	req, err := http.NewRequestWithContext(ctx, "POST", interactURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Revision", "2026-05-20")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("interaction failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var initialResponse map[string]interface{}
	if err = json.Unmarshal(respBody, &initialResponse); err != nil {
		return fmt.Errorf("failed to parse initial response: %v", err)
	}

	interactionID, ok := initialResponse["id"].(string)
	if !ok {
		return fmt.Errorf("response did not contain interaction ID: %s", string(respBody))
	}

	fmt.Printf("Interaction created with ID: %s. Polling for results...\n", interactionID)

	pollURL := fmt.Sprintf("%s/%s", interactURL, interactionID)
	ticker := time.NewTicker(b.getInteractPollInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pollReq, err := http.NewRequestWithContext(ctx, "GET", pollURL, nil)
			if err != nil {
				return err
			}
			pollReq.Header.Set("Api-Revision", "2026-05-20")

			pollResp, err := client.Do(pollReq)
			if err != nil {
				return err
			}

			pollBody, _ := io.ReadAll(pollResp.Body)
			pollResp.Body.Close() // explicit close in loop; not deferred

			if pollResp.StatusCode >= 400 {
				return fmt.Errorf("polling failed (%d): %s", pollResp.StatusCode, string(pollBody))
			}

			var result map[string]interface{}
			if err = json.Unmarshal(pollBody, &result); err != nil {
				return fmt.Errorf("failed to parse poll response: %v", err)
			}

			status, _ := result["status"].(string)
			fmt.Printf("Status: %s\n", status)

			if status != "in_progress" {
				if verbose {
					fmt.Println("Final Response (Raw):")
					prettyJSON, _ := json.MarshalIndent(result, "", "  ")
					fmt.Println(string(prettyJSON))
				} else {
					var response InteractionResponse
					if err = json.Unmarshal(pollBody, &response); err != nil {
						return fmt.Errorf("failed to parse final response: %v", err)
					}
					fmt.Println("Agent Response:")
					for _, step := range response.Steps {
						if step.Type == "model_output" {
							for _, content := range step.Content {
								if content.Type == "text" {
									fmt.Print(content.Text)
								}
							}
						}
					}
					fmt.Println()
				}
				return nil
			}
		}
	}
}
