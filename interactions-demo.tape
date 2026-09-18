Output "docs/interactions-demo.gif"

Set FontSize 14
Set Width 950
Set Height 580
Set Padding 15
Set Theme "Catppuccin Mocha"

# 1. Deploy the agent with CLI
Type "gemini-managed-agents deploy -dir ./examples/minimal"
Sleep 500ms
Enter
Sleep 4s

# 2. Verify with CLI (Interactions API)
Type "gemini-managed-agents verify -dir ./examples/minimal -prompt \"What is 15 factorial?\""
Sleep 500ms
Enter
Sleep 5s

# 3. Direct REST curl request to Interactions API
Type "TOKEN=$(gcloud auth print-access-token)"
Sleep 500ms
Enter

Type "curl -s -X POST https://aiplatform.googleapis.com/v1beta1/projects/my-project/locations/global/interactions -H \"Authorization: Bearer $TOKEN\" -H \"Api-Revision: 2026-05-20\" -d '{\"agent\":\".../minimal-agent\",\"input\":[{\"type\":\"user_input\",\"content\":[{\"type\":\"text\",\"text\":\"Ping\"}]}]}'"
Sleep 500ms
Enter
Sleep 4s

# 4. Invoke via Python Google GenAI SDK (Interactions API)
Type "python3 -c 'from google import genai; c = genai.Client(); print(c.interactions.create(agent=\".../minimal-agent\", input=\"Hi\").steps[-1].content[0].text)'"
Sleep 500ms
Enter
Sleep 4s
