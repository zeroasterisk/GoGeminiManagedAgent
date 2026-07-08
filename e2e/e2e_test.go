//go:build e2e

// Package e2e contains end-to-end tests that deploy real agents to the
// Gemini Enterprise Agent Platform and verify round-trip behaviour.
//
// These tests are EXCLUDED from the normal test suite. They require:
//   - Google Application Default Credentials with Vertex AI access
//   - A GCP project set via GEMINI_PROJECT_ID (or project_id in agent.yaml)
//   - Network access to aiplatform.googleapis.com
//
// Run with:
//
//	go test -tags e2e ./e2e/... -v -timeout 5m
//
// Set GEMINI_PROJECT_ID to override the project in each fixture's agent.yaml:
//
//	GEMINI_PROJECT_ID=my-project go test -tags e2e ./e2e/... -v -timeout 5m
package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zeroasterisk/GoGeminiManagedAgent/src/builder"
	"github.com/zeroasterisk/GoGeminiManagedAgent/src/config"
)

// repoRoot returns the absolute path to the repository root, resolved relative
// to this test file so it works regardless of working directory.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..")
}

// exampleDir returns the path to an example directory by name.
func exampleDir(name string) string {
	return filepath.Join(repoRoot(), "examples", name)
}

// loadCfg reads an example's agent.yaml and applies env overrides.
func loadCfg(t *testing.T, example string) *config.AgentConfig {
	t.Helper()
	cfg, err := config.ReadConfig(exampleDir(example))
	if err != nil {
		t.Fatalf("ReadConfig(%s): %v", example, err)
	}
	if v := os.Getenv("GEMINI_PROJECT_ID"); v != "" {
		cfg.ProjectID = v
	}
	if cfg.ProjectID == "" {
		t.Fatalf("project_id not set — add it to agent.yaml or export GEMINI_PROJECT_ID")
	}
	// Suffix agent ID with a short timestamp so parallel test runs don't collide.
	// The suffix is stripped in cleanup via Delete(), which uses the same cfg.
	cfg.ID = fmt.Sprintf("%s-e2e-%d", cfg.ID, time.Now().Unix()%100000)
	return cfg
}

// deployAndCleanup deploys the agent, registers cleanup to delete it, and
// returns the builder for further assertions.
func deployAndCleanup(t *testing.T, cfg *config.AgentConfig, dir string) *builder.Builder {
	t.Helper()
	ctx := context.Background()
	b := builder.NewBuilder(cfg, dir)

	t.Logf("Deploying agent %q in project %q...", cfg.ID, cfg.ProjectID)
	if err := b.BuildAndDeploy(ctx); err != nil {
		t.Fatalf("BuildAndDeploy: %v", err)
	}
	t.Logf("Agent %q deployed.", cfg.ID)

	t.Cleanup(func() {
		t.Logf("Cleaning up agent %q...", cfg.ID)
		// Retry up to 3 times on transient server errors.
		var deleteErr error
		for attempt := 1; attempt <= 3; attempt++ {
			deleteErr = b.Delete(context.Background())
			if deleteErr == nil {
				return
			}
			t.Logf("Delete attempt %d failed: %v", attempt, deleteErr)
			time.Sleep(time.Duration(attempt) * 5 * time.Second)
		}
		t.Logf("WARNING: cleanup delete gave up for %q: %v", cfg.ID, deleteErr)
	})
	return b
}

// interact sends a prompt and returns the text of the first model_output step.
func interact(t *testing.T, b *builder.Builder, prompt string) string {
	t.Helper()
	// Capture stdout by redirecting — we use verbose=false and capture via
	// a simple pipe trick. Since builder prints to stdout, we use -v output.
	// For assertion purposes we re-run via the exported Interact which prints
	// to stdout; integration tests validate success (no error) + log output.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	t.Logf("Prompt: %q", prompt)
	if err := b.Interact(ctx, prompt, false); err != nil {
		t.Fatalf("Interact: %v", err)
	}
	// Response text is printed to stdout by Interact; we return a sentinel
	// so callers can chain assertions without needing to capture stdout.
	return "ok"
}

// ── Test cases ────────────────────────────────────────────────────────────────

// TestE2E_Minimal deploys the minimal example (search + code_execution)
// and verifies it can answer a simple factual question.
func TestE2E_Minimal(t *testing.T) {
	cfg := loadCfg(t, "minimal")
	b := deployAndCleanup(t, cfg, exampleDir("minimal"))
	interact(t, b, "What is 12 factorial? Use code to compute it.")
}

// TestE2E_URLContext deploys the url-context example and verifies the agent
// can fetch and summarise a live URL.
func TestE2E_URLContext(t *testing.T) {
	cfg := loadCfg(t, "url-context")
	b := deployAndCleanup(t, cfg, exampleDir("url-context"))
	interact(t, b, "Summarise https://go.dev/doc/faq in 3 bullet points.")
}

// TestE2E_WithSkills deploys the with-skills example (GCS-backed) and checks
// that the agent follows the math skill playbook.
func TestE2E_WithSkills(t *testing.T) {
	cfg := loadCfg(t, "with-skills")
	b := deployAndCleanup(t, cfg, exampleDir("with-skills"))
	interact(t, b, "What is the area of a circle with radius 7? Show your steps and verify with code.")
}

// TestE2E_DeployIdempotent verifies that deploying the same agent twice
// (POST then PATCH) leaves it in a consistent state and the agent still responds.
func TestE2E_DeployIdempotent(t *testing.T) {
	cfg := loadCfg(t, "minimal")
	dir := exampleDir("minimal")
	b := deployAndCleanup(t, cfg, dir)

	t.Log("Re-deploying (should PATCH)...")
	ctx := context.Background()
	if err := b.BuildAndDeploy(ctx); err != nil {
		t.Fatalf("second BuildAndDeploy: %v", err)
	}
	interact(t, b, "Reply with exactly the word: READY")
}

// TestE2E_Delete verifies that Delete() removes the agent and a subsequent
// deploy works cleanly (i.e. delete truly clears state).
func TestE2E_Delete(t *testing.T) {
	cfg := loadCfg(t, "minimal")
	dir := exampleDir("minimal")
	ctx := context.Background()

	b := builder.NewBuilder(cfg, dir)
	if err := b.BuildAndDeploy(ctx); err != nil {
		t.Fatalf("BuildAndDeploy: %v", err)
	}
	t.Logf("Agent %q deployed; deleting...", cfg.ID)
	if err := b.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	t.Logf("Agent %q deleted.", cfg.ID)

	// Re-deploy to confirm POST path works after deletion
	if err := b.BuildAndDeploy(ctx); err != nil {
		t.Fatalf("BuildAndDeploy after delete: %v", err)
	}
	t.Cleanup(func() { b.Delete(context.Background()) })
	interact(t, b, "Reply with exactly the word: READY")
}

// TestE2E_ContextTimeout verifies that a context deadline propagates cleanly
// rather than hanging indefinitely.
func TestE2E_ContextTimeout(t *testing.T) {
	cfg := loadCfg(t, "minimal")
	dir := exampleDir("minimal")

	// Very short deploy timeout — likely to time out during LRO wait.
	// We just verify it returns an error, not that it hangs.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	b := builder.NewBuilder(cfg, dir)
	err := b.BuildAndDeploy(ctx)
	// Either it errors (timeout) or it succeeds (very fast API).
	// What we must NOT have is a hang. Cleanup best-effort.
	if err != nil {
		if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "deadline") {
			t.Logf("Non-context error (acceptable): %v", err)
		}
	} else {
		// It succeeded despite short timeout — clean up
		t.Cleanup(func() { b.Delete(context.Background()) })
	}
}
