package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/zeroasterisk/GoGeminiManagedAgent/src/builder"
	"github.com/zeroasterisk/GoGeminiManagedAgent/src/config"
)

const usage = `geap-managed-agents-builder - deploy Gemini Enterprise Managed Agents

Usage:
  geap-managed-agents-builder [flags] <command>

Commands:
  deploy   (default) Create or update the agent from the config directory
  verify   Send a test prompt and print the response
  delete   Remove the agent

Flags:
  -dir string     Agent config directory (default ".")
  -prompt string  Prompt for the verify command (default "Hello")
  -verbose        Print raw JSON response (verify only)

Environment:
  GEMINI_PROJECT_ID   Override project_id from agent.yaml
  GEMINI_LOCATION     Override location from agent.yaml (must be "global")

Examples:
  geap-managed-agents-builder -dir ./examples/minimal
  geap-managed-agents-builder -dir ./examples/minimal verify -prompt "What is 2+2?"
  geap-managed-agents-builder -dir ./examples/minimal delete
`

func main() {
	fs := flag.NewFlagSet("geap", flag.ExitOnError)
	dirFlag := fs.String("dir", ".", "Agent config directory")
	promptFlag := fs.String("prompt", "Hello", "Prompt for verify command")
	verboseFlag := fs.Bool("verbose", false, "Print raw JSON response")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	// Remaining args after flags are the subcommand
	cmd := "deploy"
	if args := fs.Args(); len(args) > 0 {
		cmd = args[0]
	}

	ctx := context.Background()

	cfg, err := config.ReadConfig(*dirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Environment variable overrides
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
		fmt.Printf("Verifying agent %q...\n", cfg.ID)
		if err := b.Interact(ctx, *promptFlag, *verboseFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Verify failed: %v\n", err)
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
