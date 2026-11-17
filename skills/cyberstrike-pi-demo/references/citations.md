# References and external links (example)

This file verifies whether the in-package **`references/`** directory is correctly recognized by the list API, HTTP `resource_path`, and the multi-agent local file tools.

## How to test (within an authorized environment)

1. `GET /api/skills/cyberstrike-pi-demo` 响应中的 `package_files` 应包含 `references/citations.md`。
2. `GET /api/skills/cyberstrike-pi-demo?resource_path=references/citations.md` 应返回本文内容。
3. 多代理且开启 `skills.filesystem_tools` 时，可通过相对路径读取本文件。

## Placeholder references

- [OWASP Testing Guide](https://owasp.org/www-project-web-security-testing-guide/) (only as a link-format example)
