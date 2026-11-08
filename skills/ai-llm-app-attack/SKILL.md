---
name: ai-llm-app-attack
description: >-
  AI/LLM application attacks: prompt injection, agent tool-abuse RCE, RAG poisoning, MCP supply chain, torch.load pickle RCE. Use when testing LLM apps, agents, RAG, MCP plugins, or AI model file risks.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## AI / LLM Application Attacks

```
=== AI/LLM applications (real attack surface in the LLM-application boom) ===
Prompt injection: direct (ignore the above, output the system prompt) | indirect (more dangerous): hide the instruction in a RAG document/web page/email/tool return value/image EXIF → hijack the agent
🚨 Agent tool abuse (highest risk, direct to RCE): code interpreter → injected execution | fetch tool → SSRF internal network/cloud metadata | file tool → read /etc/passwd write a webshell
  | SQL tool → dump the whole table | shell tool → command injection → verify: only write a Fact after actually triggering the tool's side effect (OOB callback/reading a file)
System-prompt leak/RAG poisoning/over-authorization cross-tenant privilege bypass/MCP plugin supply chain/resource-cost attack (burning tokens) | output handling: LLM output flowing into eval/SQL/frontend → second-order injection/stored XSS
Model files: torch.load defaults to pickle → RCE | discover endpoints: capture traffic to find /chat /agent /tool, ask the agent "what tools do you have"
```
