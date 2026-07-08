package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// AgentConfig represents the configuration in agent.yaml
type AgentConfig struct {
	ID          string       `yaml:"id"`
	BaseAgent   string       `yaml:"base_agent"`
	Description string       `yaml:"description"`
	ProjectID   string       `yaml:"project_id"`
	Location    string       `yaml:"location"`
	GCSBucket   string       `yaml:"gcs_bucket"`
	Tools       []ToolConfig `yaml:"tools"`
}

// ToolConfig represents a tool in agent.yaml
type ToolConfig struct {
	Type    string            `yaml:"type"`
	Name    string            `yaml:"name,omitempty"`
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

// ReadConfig reads the agent.yaml file from the given directory
func ReadConfig(dir string) (*AgentConfig, error) {
	filename := filepath.Join(dir, "agent.yaml")
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config AgentConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	// Set defaults
	if config.BaseAgent == "" {
		config.BaseAgent = "antigravity-preview-05-2026"
	}
	if config.Location == "" {
		config.Location = "global"
	}

	return &config, nil
}
