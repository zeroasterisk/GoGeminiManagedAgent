package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// idPattern mirrors the API's ID constraint:
// lowercase letters, digits, hyphens; must start with a letter;
// must end with a letter or digit; 3–63 characters.
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,61}[a-z0-9]$`)

// validToolTypes is the exhaustive set accepted by the Gemini Managed Agent API.
// Reference: confirmed against API error messages on 2026-07-08.
var validToolTypes = map[string]bool{
	"code_execution": true,
	"filesystem":     true,
	"google_search":  true,
	"mcp_server":     true,
	"url_context":    true,
}

// AgentConfig represents the configuration in agent.yaml.
type AgentConfig struct {
	ID          string       `yaml:"id"`
	BaseAgent   string       `yaml:"base_agent"`
	Description string       `yaml:"description"`
	ProjectID   string       `yaml:"project_id"`
	Location    string       `yaml:"location"`
	GCSBucket   string       `yaml:"gcs_bucket"`
	Tools       []ToolConfig `yaml:"tools"`
	Network     NetworkConfig `yaml:"network"`
}

// ToolConfig represents a single tool entry in agent.yaml.
//
// Supported types (as of 2026-07-08):
//   - code_execution  — Python sandbox; no extra fields needed
//   - filesystem      — R/W access to /workspace in the remote sandbox
//   - google_search   — Grounded web search
//   - mcp_server      — External MCP endpoint; requires url; name and headers optional
//   - url_context     — Fetch and inject URL content at inference time
type ToolConfig struct {
	Type    string            `yaml:"type"`
	Name    string            `yaml:"name,omitempty"`
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

// NetworkConfig configures the network allowlist for the remote sandbox.
// Currently the API only supports Domain: "*" (allow all outbound).
type NetworkConfig struct {
	// Allowlist domains. Only "*" is currently supported by the API.
	// Defaults to ["*"] when a GCSBucket is configured.
	Allowlist []string `yaml:"allowlist,omitempty"`
}

// ReadConfig reads and validates the agent.yaml file from the given directory.
func ReadConfig(dir string) (*AgentConfig, error) {
	filename := filepath.Join(dir, "agent.yaml")
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("reading agent.yaml: %w", err)
	}

	var cfg AgentConfig
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing agent.yaml: %w", err)
	}

	// Apply defaults
	if cfg.BaseAgent == "" {
		cfg.BaseAgent = "antigravity-preview-05-2026"
	}
	if cfg.Location == "" {
		cfg.Location = "global"
	}

	if err = cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks semantic constraints on the config.
func (c *AgentConfig) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("agent.yaml: id is required")
	}
	if !idPattern.MatchString(c.ID) {
		return fmt.Errorf("agent.yaml: id %q is invalid — must be 3–63 characters, lowercase letters/digits/hyphens, start with a letter, end with a letter or digit", c.ID)
	}
	if c.Location != "global" {
		return fmt.Errorf("agent.yaml: location must be \"global\" (only location supported by the API); got %q", c.Location)
	}
	for i, t := range c.Tools {
		if t.Type == "" {
			return fmt.Errorf("agent.yaml: tools[%d] missing type", i)
		}
		if !validToolTypes[t.Type] {
			return fmt.Errorf("agent.yaml: tools[%d] unsupported type %q; supported types: code_execution, filesystem, google_search, mcp_server, url_context", i, t.Type)
		}
		if t.Type == "mcp_server" && t.URL == "" {
			return fmt.Errorf("agent.yaml: tools[%d] type=mcp_server requires a url", i)
		}
	}
	for _, d := range c.Network.Allowlist {
		if d != "*" {
			return fmt.Errorf("agent.yaml: network.allowlist: only \"*\" is currently supported by the API; got %q", d)
		}
	}
	return nil
}
