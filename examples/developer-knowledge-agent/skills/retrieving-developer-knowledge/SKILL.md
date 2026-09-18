---
name: retrieving-developer-knowledge
metadata:
  category: CloudInfrastructureAndServices
description: >-
  Searches, retrieves, and synthesizes official Google developer documentation across Google Cloud,
  AI/Gemini, Android, Chrome, Web, Flutter, Go, Firebase, and other Google developer platforms.
  Integrates with the Developer Knowledge MCP server (search_documents, get_documents, answer_query).
---

# Google Developer Knowledge

The Developer Knowledge skill provides access to official Google developer documentation across Google Cloud, AI/ML (ai.google.dev, ADK, TensorFlow), Android, Chrome, Web, Flutter, Go, Firebase, and other Google developer platforms via the Developer Knowledge MCP server.

## Workflow

1. **Direct Retrieval**: When answering a technical question, execute a documentation lookup directly:
   - Call `answer_query` for conceptual guides, architectural comparisons, product choice overviews, and multi-step workflows.
   - Call `search_documents` for granular CLI flags, exact syntax, parameter names, and IAM permissions (`service.resource.verb`). Use 2–5 focused keywords (e.g., `cloud run filestore nfs mount gcloud`) rather than conversational queries.
   - Call `get_documents` to fetch full documentation pages by resource URI name when specific in-depth reference is needed.
2. **Grounding in Official Documentation**: Ground all solutions directly in retrieved documentation. Official documentation conventions have absolute precedence over memorized defaults.
3. **Exact Parameter Formatting**: Format CLI flags, composite keys, and IAM permission strings according to official Google specifications.
4. **Complete Solutions**: Output full, self-contained, and executable technical solutions (commands with all required flags and placeholders, YAML/JSON configurations, or code snippets) directly in your response text.
