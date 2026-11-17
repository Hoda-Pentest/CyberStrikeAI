# References and external links (example)

This file verifies whether the in-package **`references/`** directory is correctly recognized by the list API, HTTP `resource_path`, and the multi-agent local file tools.

## How to test (within an authorized environment)

1. The `package_files` in the `GET /api/skills/cyberstrike-pi-demo` response should include `references/citations.md`.
2. `GET /api/skills/cyberstrike-pi-demo?resource_path=references/citations.md` should return this content.
3. In multi-agent mode with `skills.filesystem_tools` enabled, this file can be read via a relative path.

## Placeholder references

- [OWASP Testing Guide](https://owasp.org/www-project-web-security-testing-guide/) (only as a link-format example)
