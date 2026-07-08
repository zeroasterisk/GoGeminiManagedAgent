package builder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeroasterisk/GoGeminiManagedAgent/src/config"
)

// newTestBuilder wires a Builder to the given test server with fast poll intervals.
func newTestBuilder(cfg *config.AgentConfig, dir string, srv *httptest.Server) *Builder {
	b := newBuilderWithClient(cfg, dir, srv.Client(), srv.URL)
	b.lroPollInterval = 10 * time.Millisecond
	b.interactPollInterval = 10 * time.Millisecond
	return b
}

func minimalCfg() *config.AgentConfig {
	return &config.AgentConfig{
		ID:        "test-agent",
		BaseAgent: "antigravity-preview-05-2026",
		ProjectID: "test-project",
		Location:  "global",
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}

// ── readInstructions ──────────────────────────────────────────────────────────

func TestReadInstructions_Present(t *testing.T) {
	dir := t.TempDir()
	want := "You are a helpful assistant."
	writeFile(t, dir, "instructions.md", want)

	b := &Builder{cfg: minimalCfg(), dir: dir}
	got, err := b.readInstructions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadInstructions_Missing(t *testing.T) {
	b := &Builder{cfg: minimalCfg(), dir: t.TempDir()}
	got, err := b.readInstructions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// ── agentExists ───────────────────────────────────────────────────────────────

func TestAgentExists_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	exists, err := b.agentExists(context.Background(), srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected exists=true")
	}
}

func TestAgentExists_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	exists, err := b.agentExists(context.Background(), srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected exists=false")
	}
}

func TestAgentExists_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	_, err := b.agentExists(context.Background(), srv.Client())
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

// ── deployAgent (POST new) ────────────────────────────────────────────────────

func TestDeployAgent_CreateNew_ImmediateDone(t *testing.T) {
	var gotMethod string
	var gotBody AgentPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(LROResponse{Done: true})
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, dir, "instructions.md", "Be helpful.")
	cfg := minimalCfg()
	cfg.Tools = []config.ToolConfig{{Type: "google_search"}}

	b := newTestBuilder(cfg, dir, srv)
	if err := b.BuildAndDeploy(context.Background()); err != nil {
		t.Fatalf("BuildAndDeploy: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotBody.ID != "test-agent" {
		t.Errorf("payload ID: got %q", gotBody.ID)
	}
	if gotBody.SystemInstruction != "Be helpful." {
		t.Errorf("SystemInstruction: got %q", gotBody.SystemInstruction)
	}
	if len(gotBody.Tools) != 1 || gotBody.Tools[0].Type != "google_search" {
		t.Errorf("Tools: %+v", gotBody.Tools)
	}
}

func TestDeployAgent_CreateNew_WithMCPTool(t *testing.T) {
	var gotBody AgentPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(LROResponse{Done: true})
	}))
	defer srv.Close()

	cfg := minimalCfg()
	cfg.Tools = []config.ToolConfig{
		{Type: "mcp_server", Name: "my-mcp", URL: "https://example.com/mcp",
			Headers: map[string]string{"Authorization": "Bearer tok"}},
	}
	b := newTestBuilder(cfg, t.TempDir(), srv)
	if err := b.BuildAndDeploy(context.Background()); err != nil {
		t.Fatalf("BuildAndDeploy: %v", err)
	}
	if len(gotBody.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(gotBody.Tools))
	}
	tool := gotBody.Tools[0]
	if tool.Type != "mcp_server" || tool.URL != "https://example.com/mcp" {
		t.Errorf("mcp tool: %+v", tool)
	}
	if tool.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("mcp headers: %+v", tool.Headers)
	}
}

// ── deployAgent (PATCH existing) ─────────────────────────────────────────────

func TestDeployAgent_UpdateExisting(t *testing.T) {
	var gotMethod, gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
			return
		}
		gotMethod = r.Method
		gotURL = r.URL.String()
		json.NewEncoder(w).Encode(LROResponse{Done: true})
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	if err := b.BuildAndDeploy(context.Background()); err != nil {
		t.Fatalf("BuildAndDeploy: %v", err)
	}
	if gotMethod != "PATCH" {
		t.Errorf("expected PATCH, got %s", gotMethod)
	}
	if !strings.Contains(gotURL, "update_mask") {
		t.Errorf("PATCH URL missing update_mask: %s", gotURL)
	}
}

func TestDeployAgent_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	err := b.BuildAndDeploy(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 error, got: %v", err)
	}
}

// ── waitForLRO ────────────────────────────────────────────────────────────────

func TestWaitForLRO_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(LROResponse{Done: true})
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	if err := b.waitForLRO(context.Background(), srv.Client(), "op-1"); err != nil {
		t.Fatalf("waitForLRO: %v", err)
	}
}

func TestWaitForLRO_ErrorPropagated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(LROResponse{Done: true, Error: &LROError{Code: 500, Message: "backend exploded"}})
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	err := b.waitForLRO(context.Background(), srv.Client(), "op-err")
	if err == nil || !strings.Contains(err.Error(), "backend exploded") {
		t.Errorf("expected error propagation, got: %v", err)
	}
}

func TestWaitForLRO_TransientServerError_Retried(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"transient"}`))
			return
		}
		json.NewEncoder(w).Encode(LROResponse{Done: true})
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	if err := b.waitForLRO(context.Background(), srv.Client(), "op-retry"); err != nil {
		t.Fatalf("waitForLRO should succeed after retries, got: %v", err)
	}
	if calls < 3 {
		t.Errorf("expected at least 3 calls (2 failures + 1 success), got %d", calls)
	}
}

func TestWaitForLRO_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(LROResponse{Done: false})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	err := b.waitForLRO(ctx, srv.Client(), "op-cancel")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDelete_ExistingAgent(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		json.NewEncoder(w).Encode(LROResponse{Done: true})
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	if err := b.Delete(context.Background()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if gotMethod != "DELETE" {
		t.Errorf("expected DELETE, got %s", gotMethod)
	}
}

func TestDelete_AgentNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	// Should return nil, not an error
	if err := b.Delete(context.Background()); err != nil {
		t.Errorf("expected nil for 404, got: %v", err)
	}
}

func TestDelete_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	err := b.Delete(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 error, got: %v", err)
	}
}

// ── Interact ──────────────────────────────────────────────────────────────────

func TestInteract_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			json.NewEncoder(w).Encode(map[string]interface{}{"id": "ia-1"})
			return
		}
		json.NewEncoder(w).Encode(InteractionResponse{
			ID:     "ia-1",
			Status: "completed",
			Steps: []InteractionStep{
				{Type: "model_output", Content: []InteractionContent{{Type: "text", Text: "4"}}},
			},
		})
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	if err := b.Interact(context.Background(), "2+2?", false); err != nil {
		t.Fatalf("Interact: %v", err)
	}
}

func TestInteract_NoIDInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"not_id": "x"})
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	err := b.Interact(context.Background(), "hi", false)
	if err == nil || !strings.Contains(err.Error(), "interaction ID") {
		t.Errorf("expected interaction ID error, got: %v", err)
	}
}

func TestInteract_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	err := b.Interact(context.Background(), "hi", false)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got: %v", err)
	}
}

// ── Payload shape assertions ──────────────────────────────────────────────────

func TestPayload_NoBaseEnvironment_WhenNoBucket(t *testing.T) {
	var gotBody AgentPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(LROResponse{Done: true})
	}))
	defer srv.Close()

	cfg := minimalCfg()
	cfg.GCSBucket = ""
	b := newTestBuilder(cfg, t.TempDir(), srv)
	if err := b.BuildAndDeploy(context.Background()); err != nil {
		t.Fatalf("BuildAndDeploy: %v", err)
	}
	if gotBody.BaseEnvironment != nil {
		t.Error("expected nil BaseEnvironment when no GCS bucket")
	}
}

func TestPayload_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(LROResponse{Name: "op/long", Done: false})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	if err := b.BuildAndDeploy(ctx); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
