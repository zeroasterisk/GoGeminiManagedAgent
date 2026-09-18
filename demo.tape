Output "docs/demo.gif"

Set FontSize 14
Set Width 950
Set Height 550
Set Padding 15
Set Theme "Catppuccin Mocha"

Type "cat examples/a2a-agent/agent.yaml"
Sleep 500ms
Enter
Sleep 2s

Type "export GEMINI_PROJECT_ID=my-gcp-project"
Sleep 500ms
Enter
Sleep 1s

Type "go test -tags e2e ./e2e/... -v -run TestE2E_MinimalA2A"
Sleep 500ms
Enter
Sleep 20s
