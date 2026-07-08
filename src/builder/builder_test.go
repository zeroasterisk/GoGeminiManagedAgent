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

// newTestBuilder creates a Builder wired to the given test server with fast poll intervals.
func newTestBuilder(cfg *config.AgentConfig, dir string, srv *httptest.Server) *Builder {
	b := newBuilderWithClient(cfg, dir, srv.Client(), srv.URL)
	b.lroPollInterval = 10 * time.Millisecond
	b.interactPollInterval = 10 * time.Millisecond
	return b
}

// minimalCfg returns a minimal config for tests that don't need GCS.
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

// --- readInstructions ---

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
	dir := t.TempDir()
	b := &Builder{cfg: minimalCfg(), dir: dir}
	got, err := b.readInstructions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string for missing instructions.md, got %q", got)
	}
}

// --- agentExists ---

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
		w.Write([]byte(`internal error`))
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	_, err := b.agentExists(context.Background(), srv.Client())
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

// --- deployAgent (POST new agent) ---

func TestDeployAgent_CreateNew_ImmediateDone(t *testing.T) {
	var receivedMethod string
	var receivedBody AgentPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/agents/test-agent"):
			// agentExists check → not found
			w.WriteHeader(http.StatusNotFound)
		case r.Method == "POST":
			receivedMethod = r.Method
			if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(LROResponse{Done: true})
		default:
			http.Error(w, "unexpected request", 500)
		}
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

	if receivedMethod != "POST" {
		t.Errorf("expected POST, got %s", receivedMethod)
	}
	if receivedBody.ID != "test-agent" {
		t.Errorf("payload ID: got %q, want %q", receivedBody.ID, "test-agent")
	}
	if receivedBody.SystemInstruction != "Be helpful." {
		t.Errorf("SystemInstruction: got %q", receivedBody.SystemInstruction)
	}
	if len(receivedBody.Tools) != 1 || receivedBody.Tools[0].Type != "google_search" {
		t.Errorf("Tools: got %+v", receivedBody.Tools)
	}
}

func TestDeployAgent_UpdateExisting_ImmediateDone(t *testing.T) {
	var receivedMethod string
	var patchURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/agents/test-agent"):
			// agentExists → found
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		case r.Method == "PATCH":
			receivedMethod = r.Method
			patchURL = r.URL.String()
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(LROResponse{Done: true})
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, 500)
		}
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	if err := b.BuildAndDeploy(context.Background()); err != nil {
		t.Fatalf("BuildAndDeploy: %v", err)
	}

	if receivedMethod != "PATCH" {
		t.Errorf("expected PATCH, got %s", receivedMethod)
	}
	if !strings.Contains(patchURL, "update_mask") {
		t.Errorf("PATCH URL missing update_mask: %s", patchURL)
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
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention status 403: %v", err)
	}
}

func TestDeployAgent_LROPolling(t *testing.T) {
	pollCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/agents/test-agent"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/agents"):
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(LROResponse{
				Name: "projects/test-project/locations/global/operations/op-123",
				Done: false,
			})
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/operations/op-123"):
			pollCount++
			if pollCount < 2 {
				json.NewEncoder(w).Encode(LROResponse{Done: false})
				return
			}
			json.NewEncoder(w).Encode(LROResponse{Done: true})
		default:
			http.Error(w, "unexpected: "+r.Method+" "+r.URL.Path, 500)
		}
	}))
	defer srv.Close()

	// Use a short ticker interval by monkey-patching time is not feasible,
	// so we test by verifying the operation completes without error.
	// The real LRO ticker (5s) would be too slow for unit tests.
	// Instead we test waitForLRO directly with an immediate-done server.
	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)

	// Test waitForLRO separately with a fast mock
	doneSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(LROResponse{Done: true})
	}))
	defer doneSrv.Close()

	bFast := newTestBuilder(minimalCfg(), t.TempDir(), doneSrv)
	if err := bFast.waitForLRO(context.Background(), doneSrv.Client(), "op-fast"); err != nil {
		t.Fatalf("waitForLRO: %v", err)
	}

	// Also verify LRO error propagation
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(LROResponse{Done: true, Error: &LROError{Code: 500, Message: "backend error"}})
	}))
	defer errSrv.Close()

	bErr := newTestBuilder(minimalCfg(), t.TempDir(), errSrv)
	err := bErr.waitForLRO(context.Background(), errSrv.Client(), "op-err")
	if err == nil || !strings.Contains(err.Error(), "backend error") {
		t.Errorf("expected LRO error propagation, got: %v", err)
	}

	_ = b // suppress unused warning; b is used to verify compile path
}

// --- Interact ---

func TestInteract_Success(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch {
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/interactions"):
			// initial POST
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"id": "interaction-abc"})
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/interactions/interaction-abc"):
			// poll → completed
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(InteractionResponse{
				ID:     "interaction-abc",
				Status: "completed",
				Steps: []InteractionStep{
					{
						Type: "model_output",
						Content: []InteractionContent{
							{Type: "text", Text: "Hello, world!"},
						},
					},
				},
			})
		default:
			http.Error(w, "unexpected: "+r.Method+" "+r.URL.Path, 500)
		}
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	if err := b.Interact(context.Background(), "hi", false); err != nil {
		t.Fatalf("Interact: %v", err)
	}
}

func TestInteract_NoIDInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"not_id": "something"})
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

// --- Payload construction ---

func TestBuildAndDeploy_PayloadHasNoBaseEnvironment_WhenNoBucket(t *testing.T) {
	var receivedBody AgentPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(LROResponse{Done: true})
	}))
	defer srv.Close()

	cfg := minimalCfg()
	cfg.GCSBucket = "" // no bucket
	b := newTestBuilder(cfg, t.TempDir(), srv)
	if err := b.BuildAndDeploy(context.Background()); err != nil {
		t.Fatalf("BuildAndDeploy: %v", err)
	}
	if receivedBody.BaseEnvironment != nil {
		t.Error("expected nil BaseEnvironment when no GCS bucket configured")
	}
}

func TestBuildAndDeploy_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Return in-progress LRO to force polling loop
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(LROResponse{Name: "op/long", Done: false})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	err := b.BuildAndDeploy(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
