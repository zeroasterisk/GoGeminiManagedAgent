package config

import (
	"os"
	"path/filepath"
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
`)

	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ID != "test-agent" {
		t.Errorf("ID: got %q, want %q", cfg.ID, "test-agent")
	}
	if cfg.Description != "A test agent" {
		t.Errorf("Description: got %q, want %q", cfg.Description, "A test agent")
	}
	// Defaults
	if cfg.BaseAgent != "antigravity-preview-05-2026" {
		t.Errorf("BaseAgent default: got %q", cfg.BaseAgent)
	}
	if cfg.Location != "global" {
		t.Errorf("Location default: got %q", cfg.Location)
	}
}

func TestReadConfig_Full(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `
id: "full-agent"
description: "Full config agent"
project_id: "my-project"
location: "us-central1"
gcs_bucket: "my-bucket"
base_agent: "custom-base-agent"
tools:
  - type: "code_execution"
  - type: "google_search"
  - type: "http"
    name: "my-api"
    url: "https://example.com/api"
    headers:
      Authorization: "Bearer token"
`)

	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ID != "full-agent" {
		t.Errorf("ID: got %q", cfg.ID)
	}
	if cfg.ProjectID != "my-project" {
		t.Errorf("ProjectID: got %q", cfg.ProjectID)
	}
	if cfg.Location != "us-central1" {
		t.Errorf("Location: got %q", cfg.Location)
	}
	if cfg.GCSBucket != "my-bucket" {
		t.Errorf("GCSBucket: got %q", cfg.GCSBucket)
	}
	if cfg.BaseAgent != "custom-base-agent" {
		t.Errorf("BaseAgent: got %q", cfg.BaseAgent)
	}
	if len(cfg.Tools) != 3 {
		t.Fatalf("Tools: got %d, want 3", len(cfg.Tools))
	}
	if cfg.Tools[2].Type != "http" {
		t.Errorf("Tools[2].Type: got %q", cfg.Tools[2].Type)
	}
	if cfg.Tools[2].URL != "https://example.com/api" {
		t.Errorf("Tools[2].URL: got %q", cfg.Tools[2].URL)
	}
	if cfg.Tools[2].Headers["Authorization"] != "Bearer token" {
		t.Errorf("Tools[2].Headers[Authorization]: got %q", cfg.Tools[2].Headers["Authorization"])
	}
}

func TestReadConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadConfig(dir)
	if err == nil {
		t.Fatal("expected error for missing agent.yaml, got nil")
	}
}

func TestReadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `{invalid yaml: [`)
	_, err := ReadConfig(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestReadConfig_LocationDefault_NotOverriddenWhenSet(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "agent.yaml", `
id: "agent"
location: "europe-west1"
`)
	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Location != "europe-west1" {
		t.Errorf("Location: got %q, want %q", cfg.Location, "europe-west1")
	}
}
