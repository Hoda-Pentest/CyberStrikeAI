# CyberStrikeAI Documentation

[Chinese](#chinese-documentation) | [English](#english-documentation)

CyberStrikeAI documentation is organized by user journey. Start with deployment, then move to the topic that matches your task.

## Chinese Documentation

### Start by goal

- **Quick trial**: [Deployment Guide](zh-CN/deployment.md) → [Configuration Reference](zh-CN/configuration.md) → [Troubleshooting Guide](zh-CN/troubleshooting.md)
- **Production deployment**: [Configuration Profiles](zh-CN/configuration-profiles.md) → [Security Hardening](zh-CN/security-hardening.md) → [Ops Runbooks](zh-CN/runbooks.md) → [Audit and Monitoring](zh-CN/audit-and-monitoring.md)
- **Integration and automation**: [API Reference](zh-CN/api-reference.md) → [API Recipes](zh-CN/api-recipes.md) → [MCP Federation](zh-CN/mcp-federation.md)
- **Contribute to development**: [Developer Guide](zh-CN/developer-guide.md) → [Testing Guide](zh-CN/testing.md) → [Contributing Guidelines](zh-CN/contributing-guide.md)

### Core concepts and orchestration

- [Architecture](zh-CN/architecture.md)
- [Security Model](zh-CN/security-model.md)
- [Agents and Roles](zh-CN/agent-and-role-guide.md)
- [Skills Guide](zh-CN/skills-guide.md)
- [Eino Multi-Agent](zh-CN/MULTI_AGENT_EINO.md)
- [Blackboard and Context Memory](zh-CN/blackboard-context-memory.md)
- [Agent Finalization Governance](zh-CN/agent-finalization-best-practices.md)
- [Workflows](zh-CN/workflow-graph.md)
- [Tool Call Guard](zh-CN/tool-call-guard.md)
- [Tool Execution Governance](zh-CN/tool-execution-governance.md)
- [HITL Best Practices](zh-CN/hitl-best-practices.md)

### Feature guides

- [Asset Management](zh-CN/asset-management.md)
- [Resuming Project Tasks](zh-CN/project-resume.md)
- [Token Usage Analysis](zh-CN/token-usage-monitoring.md)
- [Knowledge Base](zh-CN/knowledge-base.md)
- [RBAC Administration](zh-CN/rbac.md)
- [Robot / Chatbot](zh-CN/robot.md)
- [Vision Analysis](zh-CN/VISION.md)
- [WebShell Management](zh-CN/webshell.md)
- [C2 Guide](zh-CN/c2.md)

### Development and release

- [Developer Guide](zh-CN/developer-guide.md)
- [Plugin Development](zh-CN/plugin-development.md)
- [Frontend i18n](zh-CN/frontend-i18n.md)
- [Testing Guide](zh-CN/testing.md)
- [Contributing Guidelines](zh-CN/contributing-guide.md)
- [Release Process](zh-CN/release-process.md)

## English Documentation

### Choose a path

- **Try locally**: [Deployment](en-US/deployment.md) → [Configuration](en-US/configuration.md) → [Troubleshooting](en-US/troubleshooting.md)
- **Run in production**: [Configuration Profiles](en-US/configuration-profiles.md) → [Security Hardening](en-US/security-hardening.md) → [Runbooks](en-US/runbooks.md) → [Audit and Monitoring](en-US/audit-and-monitoring.md)
- **Integrate and automate**: [API Reference](en-US/api-reference.md) → [API Recipes](en-US/api-recipes.md) → [MCP Federation](en-US/mcp-federation.md)
- **Contribute code**: [Developer Guide](en-US/developer-guide.md) → [Testing](en-US/testing.md) → [Contributing](en-US/contributing-guide.md)

### Concepts and orchestration

- [Architecture](en-US/architecture.md)
- [Security Model](en-US/security-model.md)
- [Agents and Roles](en-US/agent-and-role-guide.md)
- [Skills](en-US/skills-guide.md)
- [Eino Multi-Agent](en-US/MULTI_AGENT_EINO.md)
- [Blackboard and Context Memory](en-US/blackboard-context-memory.md)
- [Agent Finalization](en-US/agent-finalization-best-practices.md)
- [Workflows](en-US/workflow-graph.md)
- [Tool Call Guard](en-US/tool-call-guard.md)
- [Tool Execution Governance](en-US/tool-execution-governance.md)
- [HITL Best Practices](en-US/hitl-best-practices.md)

### Feature guides

- [Asset Management](en-US/asset-management.md)
- [Resuming Project Tasks](en-US/project-resume.md)
- [Token Usage Analysis](en-US/token-usage-monitoring.md)
- [Knowledge Base](en-US/knowledge-base.md)
- [RBAC Administration](en-US/rbac.md)
- [Robot / Chatbot](en-US/robot.md)
- [Vision Analysis](en-US/VISION.md)
- [WebShell Management](en-US/webshell.md)
- [C2 Guide](en-US/c2.md)

### Development and release

- [Developer Guide](en-US/developer-guide.md)
- [Plugin Development](en-US/plugin-development.md)
- [Frontend i18n](en-US/frontend-i18n.md)
- [Testing](en-US/testing.md)
- [Contributing](en-US/contributing-guide.md)
- [Release Process](en-US/release-process.md)

## Documentation conventions

- Commands assume the repository root unless stated otherwise.
- Examples use placeholders; never commit real credentials or target systems without explicit authorization.
- Runtime behavior and configuration defaults are authoritative in `config.example.yaml` and the source code. If a document differs, report it as documentation drift.

### Public documentation scope

- Public docs serve users and contributors: usage guides, stable API and architecture references, compatibility notes, and release records. Keep internal task journals, conversation approvals, one-off review sheets, and local verification logs outside the repository.
- Chinese and English guides use matching filenames and language-switch links; each locale index covers all guides in that language.
