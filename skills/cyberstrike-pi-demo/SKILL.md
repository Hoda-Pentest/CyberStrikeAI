---
name: cyberstrike-pi-demo
description: 满配示例技能包：SKILL.md + scripts/、references/、assets/ 等可选目录；验证 Pi skill 与 HTTP 包内路径（仅授权安全测试与教学）。
---

# CyberStrike × Pi 满配技能演示

本包与 [Agent Skills](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview) 一致：**`SKILL.md` 为清单 + 主说明**（无单独 `SKILL.yaml`）。同目录可有 **`scripts/`**、**`references/`**、**`assets/`** 等任意子目录（只要路径安全、未触达包深度/文件数上限），由 **`ListPackageFiles` / `resource_path`** 与 Pi 本机工具读取。补充说明见 `FORMS.md`、`REFERENCE.md`。

## Overview

Used to verify, in one pass:

- HTTP `GET /api/skills` 列表（`script_count`、`file_count`、`progressive` 等为推导/扫描结果）
- `GET /api/skills/cyberstrike-pi-demo?depth=summary|full`
- `section=` 对应 `SKILL.md` 中 **`##` 标题**或 ASCII 标题的短 id（例如 `## Payload 样例` 常对应 `section=payload`）
- 多代理内原生 **`skill`** 工具（及可选本机文件工具）读取包内相对路径资源
- 原生 `skill` 工具对技能正文、脚本与参考资料的读取

**Hard requirement**: any testing must obtain written authorization and be limited to the agreed scope and time window.

## Authorized testing workflow

1. **Scope confirmation**: domains / IPs, the list of endpoints, prohibited actions (DoS, data exfiltration, etc.).
2. **Baseline recording**: do read-only probing of the agreed assets, saving timestamps and a summary of the raw requests/responses.
3. **Categorized testing**: split tasks by vulnerability type; double-confirm the authorization boundary before high-risk operations.
4. **Evidence and reporting**: each finding includes reproduction steps, impact, and remediation advice; sensitive data is redacted.
5. **Wrap-up**: delete temporary accounts, clean up test data, and hand over the report.

## Payload samples

The following are **teaching placeholders**; for real testing they must be replaced with the target context and must not be used on unauthorized systems:

- SQLi probe (error-based): `"'` (observe whether it triggers a database error-message disclosure)
- Reflected XSS (harmless): `<script>alert(1)</script>` -> in a test range it should be encoded or blocked by CSP
- Path traversal (read-only verification): `....//....//etc/passwd` (only in an authorized file-read scenario)

See `scripts/payloads.txt` for the full list.

## references/ and assets/

Used to verify that subdirectories other than `scripts/` are treated equally:

| Path | Purpose |
|------|------|
| `references/citations.md` | References and HTTP `resource_path` test notes |
| `assets/README.txt` | Placeholder resource (can be swapped for a real binary to test the file-read cap) |

## Recommended toolchain

| Stage | Example tools |
|------|-----------|
| Proxy and replay | Burp Suite, mitmproxy |
| Scanning and directory | ffuf, nuclei (lower the concurrency to respect the authorization) |
| Vulnerability verification | self-written PoCs, official CLIs (sqlmap, etc.) only within the authorized scope |
| Recording | Markdown + a JSON snippet template (see `scripts/report-snippet.json`) |

## Checklist and verification

- [ ] Written authorization and the test window have been saved
- [ ] The files under `scripts/` match the references in the body
- [ ] The Web UI or `GET /api/skills?...` can verify the index; within a multi-agent session, use the **`skill`** tool to load per package to save tokens
- [ ] When details are needed, fetch the full text via **`skill`**, or HTTP `depth=full`, `section=<heading or short id>`
- [ ] When the raw script is needed, use a local file tool or HTTP `resource_path=scripts/check-env.sh`
- [ ] `resource_path=references/citations.md` and `resource_path=assets/README.txt` are readable
