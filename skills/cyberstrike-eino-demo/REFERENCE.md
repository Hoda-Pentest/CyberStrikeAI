# API / reference (example)

## HTTP `/api/skills/{id}`

- `resource_path`: an in-package relative path, e.g. `FORMS.md`, `scripts/payloads.txt`.
- `depth=summary`: only the summary and table of contents, suitable for first-round retrieval.

## Multi-agent runtime

- Use the Eino ADK **`skill`** tool to load per skill package progressively; optionally enable `multi_agent.eino_skills.filesystem_tools` to access in-package files.

## Links

- [Agent Skills overview](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview)
