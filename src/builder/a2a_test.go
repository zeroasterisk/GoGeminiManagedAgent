package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe: %v", err)
	}
	os.Stdout = orig
	return <-done
}

func TestStreamMessage_Success(t *testing.T) {
	var gotPath string
	var gotReq a2a.SendMessageRequest
	events := []string{
		`{"statusUpdate":{"taskId":"t-1","contextId":"c-1","status":{"state":"TASK_STATE_WORKING"}}}`,
		// a0: a first chunk that the next append=false chunk must REPLACE.
		`{"artifactUpdate":{"taskId":"t-1","contextId":"c-1","artifact":{"artifactId":"a0","parts":[{"text":"OLDCHUNK"}]}}}`,
		`{"artifactUpdate":{"taskId":"t-1","contextId":"c-1","lastChunk":true,"artifact":{"artifactId":"a0","parts":[{"data":{"foo":"bar"}},{"text":"tooltext"}]}}}`,
		// a1: two append=true chunks that concatenate to "42".
		`{"artifactUpdate":{"taskId":"t-1","contextId":"c-1","artifact":{"artifactId":"a1","parts":[{"text":"4"}]}}}`,
		`{"artifactUpdate":{"taskId":"t-1","contextId":"c-1","append":true,"lastChunk":true,"artifact":{"artifactId":"a1","parts":[{"text":"2"}]}}}`,
		`{"statusUpdate":{"taskId":"t-1","contextId":"c-1","status":{"state":"TASK_STATE_COMPLETED"}}}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	out := captureStdout(t, func() {
		if err := b.StreamMessage(context.Background(), "what is the answer?", false); err != nil {
			t.Fatalf("StreamMessage: %v", err)
		}
	})

	if !strings.HasSuffix(gotPath, "/a2a/v1/message:stream") {
		t.Errorf("path: got %q, want suffix /a2a/v1/message:stream", gotPath)
	}
	if gotReq.Message == nil || len(gotReq.Message.Parts) == 0 || gotReq.Message.Parts[0].Text() != "what is the answer?" {
		t.Errorf("request message: got %+v", gotReq.Message)
	}
	if gotReq.Message.Role != a2a.MessageRoleUser {
		t.Errorf("request role: got %q, want %q", gotReq.Message.Role, a2a.MessageRoleUser)
	}
	// Events are named with A2A terminology; append concatenates ("42") and the
	// data/text Parts of the replaced a0 chunk are rendered.
	for _, want := range []string{
		"TaskStatusUpdateEvent (working)",
		"TaskArtifactUpdateEvent a0",
		"foo", "bar", // data Part
		"tooltext", // text Part
		"TaskArtifactUpdateEvent a1",
		"42", // append=true concatenation of "4" + "2"
		"TaskStatusUpdateEvent (completed)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("streamed output %q does not contain %q", out, want)
		}
	}
	// append=false replaced the first a0 chunk, so it must be gone.
	if strings.Contains(out, "OLDCHUNK") {
		t.Errorf("append=false chunk was not replaced: %q", out)
	}
}

func TestStreamMessage_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"nope"}}`))
	}))
	defer srv.Close()

	b := newTestBuilder(minimalCfg(), t.TempDir(), srv)
	err := b.StreamMessage(context.Background(), "hi", false)
	if err == nil {
		t.Fatal("expected error from 403 response")
	}
}
