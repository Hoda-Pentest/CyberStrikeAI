# Skills directory (Agent Skills / Pi)

- Each skill is a **subdirectory** that must have a **`SKILL.md`** at its root (YAML front matter: `name`, `description` + Markdown body); see [agentskills.io](https://agentskills.io/specification.md).
- **The directory name must match `name`**.
- **Runtime loading**: in **Pi single-agent or multi-agent** sessions, skills are progressively disclosed by the native **`skill` tool** (the system prompt lists each skill's name/description, and the model then calls the **`skill`** tool to fetch the full `SKILL.md`). Optionally enable **`multi_agent.skills.filesystem_tools`** to access scripts and resources in the package with the same local `read_file` / `execute` tools.
- **Web management**: HTTP `/api/skills/*` is still used for listing, editing and uploading files in a package (implemented as `internal/skillpackage`, not MCP).
- **Runtime**: both single-agent and multi-agent load skills on demand via the native **`skill`** tool.
