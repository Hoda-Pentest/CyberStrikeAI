# Skills directory (Agent Skills / Eino)

- Each skill is a **subdirectory** that must have a **`SKILL.md`** at its root (YAML front matter: `name`, `description` + Markdown body); see [agentskills.io](https://agentskills.io/specification.md).
- **The directory name must match `name`.**
- **Runtime loading**: in an **Eino DeepAgent (multi-agent)** session, the ADK **`skill` middleware** discloses them progressively (the system prompt lists each skill's name/description, and the model then calls the **`skill`** tool to fetch the full `SKILL.md`). Optionally enable **`multi_agent.eino_skills.filesystem_tools`** to access in-package scripts and resources with the same `read_file` / `execute` etc. as on the local machine.
- **Web management**: HTTP `/api/skills/*` is still used to list, edit, and upload in-package files (implemented as `internal/skillpackage`, not MCP).
- **Runtime**: within a multi-agent (DeepAgent) session they are loaded progressively by the ADK **`skill`** tool; the single-agent MCP loop does not include Skills, so enable multi-agent or the later single-agent Eino path.
