---
name: cyberstrike-eino-demo
description: A fully-featured example skill package: SKILL.md + optional directories such as scripts/, references/, assets/; it verifies the Eino skill and HTTP in-package paths (for authorized security testing and teaching only).
---

# CyberStrike x Eino fully-featured skill demo

This package follows [Agent Skills](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview): **`SKILL.md` is the manifest + main description** (no separate `SKILL.yaml`). The same directory may contain any subdirectories such as **`scripts/`**, **`references/`**, **`assets/`** (as long as the paths are safe and do not exceed the package depth/file-count limits), read by **`ListPackageFiles` / `resource_path`** and the Eino local tools. See `FORMS.md` and `REFERENCE.md` for more.

## Overview

Used to verify, in one pass:

- the HTTP `GET /api/skills` list (`script_count`, `file_count`, `progressive`, etc. are derived/scanned results)
- `GET /api/skills/cyberstrike-eino-demo?depth=summary|full`
- `section=` corresponds to the short id of an **`##` heading** or ASCII heading in `SKILL.md` (e.g. `## Payload samples` usually corresponds to `section=payload`)
- the ADK **`skill`** tool inside multi-agent (and optional local file tools) reading in-package relative-path resources
- Eino `FilesystemSkillsRetriever` retrieval over the package summary, `##` chunks, and script entries

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
