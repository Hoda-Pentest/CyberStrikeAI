# API / reference (example)

## HTTP `/api/skills/{id}`

- `resource_path`: an in-package relative path, e.g. `FORMS.md`, `scripts/payloads.txt`.
- `depth=summary`: only the summary and table of contents, suitable for first-round retrieval.

## Multi-agent runtime

- 使用 Pi **`skill`** 工具按技能包渐进加载；可选开启 `multi_agent.skills.filesystem_tools` 访问包内文件。

## Links

- [Agent Skills overview](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview)
