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
id: "my-agent"
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
id: "my-agent"
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
id: "my-agent"
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
id: "my-agent"
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
id: "my-agent"
network:
  allowlist:
    - "*"
`)
	_, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── ID format validation ──────────────────────────────────────────────────────

func TestValidate_IDTooShort(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `id: "ab"`) // 2 chars, min is 3
	_, err := ReadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected invalid ID error, got: %v", err)
	}
}

func TestValidate_IDWithUppercase(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `id: "MyAgent"`)
	_, err := ReadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected invalid ID error, got: %v", err)
	}
}

func TestValidate_IDWithSpaces(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `id: "my agent"`)
	_, err := ReadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected invalid ID error, got: %v", err)
	}
}

func TestValidate_IDTrailingHyphen(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `id: "my-agent-"`)
	_, err := ReadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected invalid ID error for trailing hyphen, got: %v", err)
	}
}

func TestValidate_IDLeadingDigit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `id: "1agent"`)
	_, err := ReadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected invalid ID error for leading digit, got: %v", err)
	}
}

func TestValidate_IDTooLong(t *testing.T) {
	dir := t.TempDir()
	// 64 chars — one over the limit
	writeFile(t, dir, "agent.yaml", `id: "a123456789012345678901234567890123456789012345678901234567890123"`)
	_, err := ReadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected invalid ID error for too-long ID, got: %v", err)
	}
}

func TestValidate_IDValid_MinLength(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `id: "abc"`) // 3 chars — minimum valid
	_, err := ReadConfig(dir)
	if err != nil {
		t.Errorf("expected valid 3-char ID to pass, got: %v", err)
	}
}

func TestValidate_IDValid_WithHyphens(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `id: "my-great-agent-v2"`)
	_, err := ReadConfig(dir)
	if err != nil {
		t.Errorf("expected valid hyphenated ID to pass, got: %v", err)
	}
}

func TestReadConfig_LocationExplicitGlobal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `
id: "my-agent"
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
