package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

func (b *Builder) a2aEndpointURL() string {
	return fmt.Sprintf("%s/projects/%s/locations/%s/agents/%s/a2a/v1",
		b.apiBase, b.cfg.ProjectID, b.cfg.Location, b.cfg.ID)
}

// StreamMessage streams the reply from the agent over A2A (message:stream) and
// prints text as it arrives. It is the A2A equivalent of Interact.
func (b *Builder) StreamMessage(ctx context.Context, prompt string, verbose bool) error {
	_, err := b.StreamMessageWithResult(ctx, prompt, verbose)
	return err
}

// StreamMessageWithResult streams the reply from the agent over A2A (message:stream),
// prints text as it arrives, and returns the aggregated text response.
func (b *Builder) StreamMessageWithResult(ctx context.Context, prompt string, verbose bool) (string, error) {
	httpClient, err := b.getClient(ctx)
	if err != nil {
		return "", err
	}

	endpoint := a2a.NewAgentInterface(b.a2aEndpointURL(), a2a.TransportProtocolHTTPJSON)
	client, err := a2aclient.NewFromEndpoints(ctx,
		[]*a2a.AgentInterface{endpoint},
		a2aclient.WithRESTTransport(httpClient),
		a2aclient.WithConfig(a2aclient.Config{
			PreferredTransports: []a2a.TransportProtocol{a2a.TransportProtocolHTTPJSON},
		}),
	)
	if err != nil {
		return "", fmt.Errorf("creating A2A client: %w", err)
	}
	defer client.Destroy()

	req := &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(prompt)),
	}
	fmt.Printf("Sending A2A message:stream to agent %s...\n", b.cfg.ID)

	var textBuffer bytes.Buffer
	multiWriter := io.MultiWriter(os.Stdout, &textBuffer)
	printer := newA2AStreamPrinter(multiWriter, isTerminal(os.Stdout))
	for event, err := range client.SendStreamingMessage(ctx, req) {
		if err != nil {
			return "", fmt.Errorf("A2A message:stream: %w", err)
		}
		if verbose {
			if err := printA2AJSON(multiWriter, event); err != nil {
				return "", err
			}
			continue
		}
		printer.event(event)
	}
	if !verbose {
		printer.flush()
		fmt.Println()
	}
	return textBuffer.String(), nil
}

type a2aStreamPrinter struct {
	w       io.Writer
	color   bool
	started bool
	arts    map[a2a.ArtifactID]*a2a.Artifact
	order   []a2a.ArtifactID
	done    map[a2a.ArtifactID]bool
}

func newA2AStreamPrinter(w io.Writer, color bool) *a2aStreamPrinter {
	return &a2aStreamPrinter{
		w:     w,
		color: color,
		arts:  map[a2a.ArtifactID]*a2a.Artifact{},
		done:  map[a2a.ArtifactID]bool{},
	}
}

func (p *a2aStreamPrinter) event(event a2a.Event) {
	switch e := event.(type) {
	case *a2a.Message:
		p.header("Message")
		p.renderParts(e.Parts)
	case *a2a.Task:
		p.header("Task (" + state(e.Status.State) + ")")
		if e.Status.Message != nil {
			p.renderParts(e.Status.Message.Parts)
		}
		for _, art := range e.Artifacts {
			p.renderArtifact(art)
		}
	case *a2a.TaskStatusUpdateEvent:
		p.header("TaskStatusUpdateEvent (" + state(e.Status.State) + ")")
		if e.Status.Message != nil {
			p.renderParts(e.Status.Message.Parts)
		}
	case *a2a.TaskArtifactUpdateEvent:
		p.updateArtifact(e)
	}
}

func (p *a2aStreamPrinter) updateArtifact(e *a2a.TaskArtifactUpdateEvent) {
	if e.Artifact == nil {
		return
	}
	id := e.Artifact.ID
	acc, ok := p.arts[id]
	if !ok {
		acc = &a2a.Artifact{ID: id}
		p.arts[id] = acc
		p.order = append(p.order, id)
	}
	if e.Append {
		acc.Parts = append(acc.Parts, e.Artifact.Parts...)
	} else {
		acc.Parts = e.Artifact.Parts // append=false replaces the artifact's parts
	}
	if e.Artifact.Name != "" {
		acc.Name = e.Artifact.Name
	}
	if e.LastChunk {
		p.renderArtifact(acc)
		p.done[id] = true
	}
}

func (p *a2aStreamPrinter) flush() {
	for _, id := range p.order {
		if !p.done[id] {
			p.renderArtifact(p.arts[id])
			p.done[id] = true
		}
	}
}

func (p *a2aStreamPrinter) renderArtifact(art *a2a.Artifact) {
	label := "TaskArtifactUpdateEvent " + string(art.ID)
	if art.Name != "" {
		label += " (" + art.Name + ")"
	}
	p.header(label)
	p.renderParts(art.Parts)
}

func (p *a2aStreamPrinter) renderParts(parts a2a.ContentParts) {
	var text strings.Builder
	flushText := func() {
		if text.Len() > 0 {
			fmt.Fprintln(p.w, text.String())
			text.Reset()
		}
	}
	for _, part := range parts {
		if t := part.Text(); t != "" {
			text.WriteString(t)
			continue
		}
		flushText()
		if d := part.Data(); d != nil {
			p.data(d)
			continue
		}
		if u := part.URL(); u != "" {
			p.line(fmt.Sprintf("file %s %s", part.MediaType, u))
			continue
		}
		if raw := part.Raw(); raw != nil {
			p.line(fmt.Sprintf("file %s (%d bytes)", part.MediaType, len(raw)))
		}
	}
	flushText()
}

func (p *a2aStreamPrinter) data(v any) {
	out, err := json.MarshalIndent(v, "    ", "  ")
	if err != nil {
		return
	}
	fmt.Fprintf(p.w, "    %s\n", out)
}

func (p *a2aStreamPrinter) header(label string) {
	if p.started {
		fmt.Fprintln(p.w)
	}
	p.started = true
	fmt.Fprintln(p.w, p.bold("- "+label))
}

func (p *a2aStreamPrinter) line(s string) {
	fmt.Fprintf(p.w, "    %s\n", s)
}

func (p *a2aStreamPrinter) bold(s string) string {
	if !p.color {
		return s
	}
	return "\x1b[1m" + s + "\x1b[0m"
}

func state(s a2a.TaskState) string {
	return strings.ToLower(strings.TrimPrefix(string(s), "TASK_STATE_"))
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func printA2AJSON(w io.Writer, v any) error {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling A2A event: %w", err)
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}
