package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/zeroasterisk/GoGeminiManagedAgent/src/builder"
	"github.com/zeroasterisk/GoGeminiManagedAgent/src/config"
)

func main() {
	dirFlag := flag.String("dir", ".", "Directory containing the agent configuration")
	verifyFlag := flag.Bool("verify", false, "Verify the agent by sending a test prompt")
	promptFlag := flag.String("prompt", "Hello", "Prompt to send to the agent for verification")
	verboseFlag := flag.Bool("verbose", false, "Print verbose output (raw JSON)")
	flag.Parse()

	ctx := context.Background()

	cfg, err := config.ReadConfig(*dirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}

	// Override with env vars if present
	if envProject := os.Getenv("GEMINI_PROJECT_ID"); envProject != "" {
		cfg.ProjectID = envProject
	}
	if envLocation := os.Getenv("GEMINI_LOCATION"); envLocation != "" {
		cfg.Location = envLocation
	}

	if cfg.ProjectID == "" {
		fmt.Fprintln(os.Stderr, "Error: project_id must be specified in agent.yaml or via GEMINI_PROJECT_ID env var")
		os.Exit(1)
	}

	b := builder.NewBuilder(cfg, *dirFlag)

	if *verifyFlag {
		fmt.Printf("Verifying agent %s...\n", cfg.ID)
		err = b.Interact(ctx, *promptFlag, *verboseFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Verification failed: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Printf("Deploying agent %s from %s...\n", cfg.ID, *dirFlag)
		err = b.BuildAndDeploy(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Deployment failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Deployment complete.")
	}
}
