# Role Configuration Files

This directory contains all role configuration files. Each role defines the AI's behavior mode and available tools.

## Creating a New Role

To create a new role, create a YAML file under the `roles/` directory in the following format:

**Option 1: Explicitly specify the tool list (recommended)**
```yaml
name: Role name
description: Role description
user_prompt: User prompt (prepended to the user message to guide the AI's behavior)
icon: "Icon (optional)"
tools:
    # Add the tools you need...
    # ⚠️ Important: it is recommended to include the following core built-in MCP tools (vulnerabilities and knowledge base)
    - record_vulnerability
    - list_knowledge_risk_types
    - search_knowledge_base
enabled: true
```

**Option 2: Do not set the tools field (use all enabled tools)**
```yaml
name: Role name
description: Role description
user_prompt: User prompt (prepended to the user message to guide the AI's behavior)
icon: "Icon (optional)"
# Do not set the tools field; by default all tools enabled in MCP management will be used
enabled: true
```

## ⚠️ Important Reminder: Core Built-in MCP Tools

**If you set the `tools` field, be sure to include the following tools in the list (at least these three):**

1. **`record_vulnerability`** - Vulnerability management tool, used to record discovered vulnerabilities
2. **`list_knowledge_risk_types`** - Knowledge base tool, lists the available risk types
3. **`search_knowledge_base`** - Knowledge base tool, searches the knowledge base content

You may also add other built-in or external tools as needed, such as WebShell and batch tasks (subject to what is enabled in MCP management).

**Skills**: In **multi-agent / Eino** sessions, the packages under `skills_dir` are loaded on demand by the built-in **`skill`** tool, and have no binding relationship with the role YAML.

**Note**: If you do not set the `tools` field, the system will by default use all tools enabled in MCP management. To control a role's available tools explicitly, it is recommended to set the `tools` field explicitly.

## Role Configuration Field Reference

- **name**: Role name (required)
- **description**: Role description (required)
- **user_prompt**: User prompt; prepended to the user message to guide the AI toward a specific testing approach and focus (optional)
- **icon**: Role icon; supports Unicode emoji (optional)
- **tools**: Tool list; specifies the tools available to this role (optional)
  - **If the `tools` field is not set**: by default **all tools enabled in MCP management** are selected
  - **If the `tools` field is set**: only the tools specified in the list are used (it is recommended to include at least the core built-in tools above)
- **enabled**: Whether this role is enabled (required, true/false)

## Examples

Refer to the other role files in this directory, such as `CTF.yaml`.
