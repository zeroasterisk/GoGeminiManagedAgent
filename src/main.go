package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/zeroasterisk/GoGeminiManagedAgent/src/builder"
	"github.com/zeroasterisk/GoGeminiManagedAgent/src/config"
)

const usage = `gemini-managed-agents - deploy Agent Platform Managed Agents

Usage:
  gemini-managed-agents [command] [flags]

Commands:
  deploy   Create or update the agent from the config directory (default)
  verify   Send a test prompt and print the response
  delete   Remove the agent
  list     List all agents in the project

Flags:
  -dir string       Agent config directory (default "."); not required for list
  -prompt string    Prompt for the verify command (default "Hello")
  -protocol string  Transport for verify: "interactions" (default) or "a2a"
  -verbose          Print raw JSON response (verify only)

Environment:
  GEMINI_PROJECT_ID   GCP project ID (overrides project_id in agent.yaml)
  GEMINI_LOCATION     Location (must be "global"; overrides location in agent.yaml)

Examples:
  # Deploy an agent directory
  gemini-managed-agents deploy -dir ./examples/minimal

  # Send a test prompt (raw Interactions API)
  gemini-managed-agents verify -dir ./examples/minimal -prompt "What is 2+2?"

  # Send a test prompt over A2A protocol (SSE stream)
  gemini-managed-agents verify -dir ./examples/minimal -protocol a2a -prompt "What is 2+2?"

  # List all agents in a project
  GEMINI_PROJECT_ID=my-project gemini-managed-agents list

  # Delete an agent
  gemini-managed-agents delete -dir ./examples/minimal
`

func main() {
	fs := flag.NewFlagSet("gemini-managed-agents", flag.ExitOnError)
	dirFlag := fs.String("dir", ".", "Agent config directory")
	promptFlag := fs.String("prompt", "Hello", "Prompt for verify command")
	protocolFlag := fs.String("protocol", "interactions", "Transport for verify: interactions or a2a")
	verboseFlag := fs.Bool("verbose", false, "Print raw JSON response")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	// Subcommand comes first (like `go build`, `git push`); its flags follow it.
	// With no subcommand, default to deploy so `... -dir x` still works.
	args := os.Args[1:]
	cmd := "deploy"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	ctx := context.Background()

	// list is the only command that doesn't need an agent dir.
	if cmd == "list" {
		runList(ctx)
		return
	}

	cfg, err := config.ReadConfig(*dirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if v := os.Getenv("GEMINI_PROJECT_ID"); v != "" {
		cfg.ProjectID = v
	}
	if v := os.Getenv("GEMINI_LOCATION"); v != "" {
		cfg.Location = v
	}

	if cfg.ProjectID == "" {
		fmt.Fprintln(os.Stderr, "Error: project_id must be set in agent.yaml or via GEMINI_PROJECT_ID")
		os.Exit(1)
	}

	b := builder.NewBuilder(cfg, *dirFlag)

	switch cmd {
	case "deploy":
		fmt.Printf("Deploying agent %q from %s...\n", cfg.ID, *dirFlag)
		if err := b.BuildAndDeploy(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Deployment failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Deployment complete.")

	case "verify":
		switch strings.ToLower(*protocolFlag) {
		case "interactions":
			fmt.Printf("Verifying agent %q...\n", cfg.ID)
			if err := b.Interact(ctx, *promptFlag, *verboseFlag); err != nil {
				fmt.Fprintf(os.Stderr, "Verify failed: %v\n", err)
				os.Exit(1)
			}
		case "a2a":
			fmt.Printf("Verifying agent %q via A2A...\n", cfg.ID)
			if err := b.StreamMessage(ctx, *promptFlag, *verboseFlag); err != nil {
				fmt.Fprintf(os.Stderr, "Verify failed: %v\n", err)
				os.Exit(1)
			}
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown -protocol %q (want \"interactions\" or \"a2a\")\n", *protocolFlag)
			os.Exit(1)
		}

	case "delete":
		fmt.Printf("Deleting agent %q...\n", cfg.ID)
		if err := b.Delete(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Delete failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Done.")

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q\n\n%s", cmd, usage)
		os.Exit(1)
	}
}

func runList(ctx context.Context) {
	// For list, we need a project + location but no agent dir.
	// Build a minimal config from env vars + defaults.
	projectID := os.Getenv("GEMINI_PROJECT_ID")
	if projectID == "" {
		// Try to load from a dir if present (so `list -dir ./examples/minimal` also works)
		fmt.Fprintln(os.Stderr, "Error: GEMINI_PROJECT_ID is required for list\n  export GEMINI_PROJECT_ID=my-project")
		os.Exit(1)
	}
	location := os.Getenv("GEMINI_LOCATION")
	if location == "" {
		location = "global"
	}

	// Use a stub config — List() only needs ProjectID and Location.
	cfg := &config.AgentConfig{
		ID:        "list-stub", // not used by List()
		ProjectID: projectID,
		Location:  location,
	}

	b := builder.NewBuilder(cfg, ".")
	agents, err := b.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "List failed: %v\n", err)
		os.Exit(1)
	}

	if len(agents) == 0 {
		fmt.Printf("No agents found in project %q.\n", projectID)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tDESCRIPTION\tTOOLS\tUPDATED")
	fmt.Fprintln(w, "--\t-----------\t-----\t-------")
	for _, a := range agents {
		tools := ""
		for i, t := range a.Tools {
			if i > 0 {
				tools += ", "
			}
			tools += t.Type
		}
		desc := a.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		updated := a.Updated
		if len(updated) > 10 {
			updated = updated[:10] // date only
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.ID, desc, tools, updated)
	}
	w.Flush()
	fmt.Printf("\n%d agent(s) in project %q (location: %s)\n", len(agents), projectID, location)
}
