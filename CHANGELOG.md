# Changelog

## Unreleased

### Added

- AI channels support custom HTTP request headers, environment variables, and session-ID placeholders; saved authentication headers can be used safely for connection tests, a separate audit identity isolates the primary channel's request headers, and custom headers do not follow cross-origin redirects.

- Added `agent.task_timeout_minutes` to configure execution timeouts for single-agent, multi-agent, workflow, and asynchronous tasks: unset defaults to 600 minutes, `0` means no limit, and negative values are refused at load time; resumed runs preserve the re-timing behavior, and per-tool timeouts still apply independently.

- The per-item download button in the vulnerability list gained a format-selection dialog supporting Markdown and Word (.docx); the Word download reuses vulnerability-read permissions and includes only the selected vulnerabilities (#365).

- Added asynchronous conversation APIs for single-agent and multi-agent runs: a task receipt is returned on acceptance, and a client disconnect does not stop execution; supports per-user isolated idempotent submission, persistent state and result queries, explicit cancellation, and interrupted state after a service restart. The synchronous APIs remain compatible.

- On startup, all project-management tables are automatically checked against a unified, up-to-date SQLite definition: missing tables are created and missing columns are added, with no need to maintain a column-by-column migration list; existing data and extra tables/columns are preserved, and if schema completion fails it rolls back and blocks startup. HITL and notification tables are included in the unified initialization, and the legacy vulnerability foreign-key migration preserves extra columns, indexes, triggers, and dependent records.

### Fixed

- Fixed an issue where WebShell batch liveness checks stayed stuck at "checking" due to a request-construction error; saved credentials are now read using the connection's own ID, and after a failed check the button is restored and other connections continue to be processed.

- Fixed a failure where the audit Agent's requests to compatibility gateways that do not support the `thinking` field would fail; added support for a separate compatibility profile and detection of default behavior by model and address.

- Fixed an issue where official OpenAI models known to support `none` still used the server-side default reasoning after reasoning was disabled (#366): the explicit disable parameter is now sent and preserved per provider, endpoint, and model, covering the Planner and forced tool selection; DeepSeek and third-party compatibility-gateway behavior is preserved, and the tool-call limitation that requires Responses is clearly surfaced.

- Prevent the built-in administrator from losing privileges: disabling the built-in `admin` or removing its system-administrator role is now refused, and on failure the original profile, permissions, and sessions are preserved; the UI disables the corresponding actions accordingly.

- Fixed an issue where asset-management information gathering failed because the default FOFA request asked for the unsupported `province` return field; `region` is now used consistently; legacy form/API field aliases remain compatible, imported assets' provinces are preserved, and the intelligence-field preset is corrected to `asn/org`.

- Fixed an issue where single-agent, multi-agent, role-workflow, and batch tasks misreported a lower-level call timeout as a whole-task timeout: a task is now marked timed out only when the task context's own deadline expires, and lower-level failures preserve their original diagnostics; the overall time limit and existing retry policy are unchanged.

- C2 Profile and WebShell deletions of nonexistent objects now consistently return 404, deletion database failures return a generic 500 message and no longer expose SQL errors; vulnerability and batch-task lists now consistently return `[]` when there are no matching records, with added HTTP contract regression tests.

- Fixed an issue where, when multiple people edited tool-call guard rules, a stale draft overwrote others' changes: only one save succeeds per version, a conflict preserves the draft and prompts a refresh; API saves must carry the ETag/If-Match from read time.

- Fixed an issue where updating/deleting a platform role logged out all users: only sessions of members affected by the role are now revoked; updates to the name, description, and equivalent permission sets keep sessions valid.

- Fixed overflow of dashboard status cards, risk badges, and queue labels caused by long text such as Russian; the sidebar and risk names now wrap fully, and the layout adapts to each card's available width.

- Self-update recovery now strictly validates the recovery record, state, commit, and binary pairing before rewriting files; it rejects missing, duplicate, or contradictory evidence and preserves a recoverable state.
- Editing a stopped external MCP no longer unexpectedly starts a connection; start/stop, config edits, and app restarts keep a consistent start/stop state.

- The login password show/hide button is now an eye icon inside the input field, with unified input width/height, keyboard focus support, a 44px touch target, accessibility labels in Chinese/English/Russian, and light/dark themes.
