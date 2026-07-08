package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}

func TestReadConfig_Minimal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `
id: "test-agent"
description: "A test agent"
project_id: "my-project"
`)
	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ID != "test-agent" {
		t.Errorf("ID: got %q", cfg.ID)
	}
	if cfg.BaseAgent != "antigravity-preview-05-2026" {
		t.Errorf("BaseAgent default: got %q", cfg.BaseAgent)
	}
	if cfg.Location != "global" {
		t.Errorf("Location default: got %q", cfg.Location)
	}
}

func TestReadConfig_AllToolTypes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `
id: "full-agent"
project_id: "my-project"
gcs_bucket: "my-bucket"
tools:
  - type: "code_execution"
  - type: "filesystem"
  - type: "google_search"
  - type: "url_context"
  - type: "mcp_server"
    name: "my-mcp"
    url: "https://example.com/mcp"
    headers:
      Authorization: "Bearer tok"
`)
	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Tools) != 5 {
		t.Fatalf("Tools: got %d, want 5", len(cfg.Tools))
	}
	mcp := cfg.Tools[4]
	if mcp.Type != "mcp_server" || mcp.URL != "https://example.com/mcp" {
		t.Errorf("mcp_server tool: %+v", mcp)
	}
	if mcp.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("mcp headers: %+v", mcp.Headers)
	}
}

func TestReadConfig_MissingFile(t *testing.T) {
	_, err := ReadConfig(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing agent.yaml")
	}
}

func TestReadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `{invalid yaml: [`)
	_, err := ReadConfig(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidate_MissingID(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `description: "no id"`)
	_, err := ReadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Errorf("expected id-required error, got: %v", err)
	}
}

func TestValidate_BadLocation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `
id: "x"
location: "us-east5"
`)
	_, err := ReadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "location must be") {
		t.Errorf("expected location error, got: %v", err)
	}
}

func TestValidate_UnsupportedToolType(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `
id: "x"
tools:
  - type: "http"
`)
	_, err := ReadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("expected unsupported tool error, got: %v", err)
	}
}

func TestValidate_MCPServerMissingURL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `
id: "x"
tools:
  - type: "mcp_server"
    name: "no-url"
`)
	_, err := ReadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "requires a url") {
		t.Errorf("expected mcp url error, got: %v", err)
	}
}

func TestValidate_NetworkAllowlistOnlyStarSupported(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `
id: "x"
network:
  allowlist:
    - "*.googleapis.com"
`)
	_, err := ReadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "only \"*\" is currently supported") {
		t.Errorf("expected allowlist error, got: %v", err)
	}
}

func TestValidate_NetworkAllowlistStar(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `
id: "x"
network:
  allowlist:
    - "*"
`)
	_, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadConfig_LocationExplicitGlobal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `
id: "x"
location: "global"
`)
	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Location != "global" {
		t.Errorf("Location: got %q", cfg.Location)
	}
}
