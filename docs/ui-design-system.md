# UI style conventions

The main app's shared styles live in `web/static/css/design-system.css`, loaded after the module and mobile CSS.
Colors reuse the existing theme variables; drawer outer frames are uniformly square, cards use a 16px radius, controls a 10px radius, desktop body padding 24px, and mobile 16px.
Status colors, selected states, charts, the terminal and the workflow canvas keep their own business semantics.

## Container choice

- Details, logs, long forms: right-side drawer, 720px by default; monospace editing panels for code and long text at 960px; the title bar can expand to 1440px.
- Phones (below 768px): the drawer is full-screen, adapts to the safe area, the body scrolls on its own, and the bottom actions stay visible.
- Confirmations, renames, short bindings: a centered short dialog, kept compact on phones too.
- Data comparison and bulk operations continue to use tables; the terminal, workflow and session workspace are not forced into card lists.

`DRAWER_IDS` and `WIDE_IDS` in `web/static/js/modal.js` manage the migration list of existing modules.
New modules should open/close their container via `openAppModal` / `closeAppModal`, keeping the existing cleanup and unsaved-changes prompt logic on close.
The shared lifecycle handles nesting levels, background scroll locking, focus return, Tab focus trapping, and Escape triggering the original close button.
A title bar that updates asynchronously automatically gets its expand button added. The short C2 sleep setting remains a dialog.

Do not add per-drawer positioning, backdrop, corner radius or theme-color overrides anymore.
At this stage a few `!important` rules in the shared layout override legacy inline widths/heights and old mobile rules; these legacy declarations can be removed gradually as those modules are revised.

## Verification

Frontend regression:

```sh
node --test web/static/js/*.test.cjs
```

Browser verification requires an externally installed Playwright and Chromium, for example:

```sh
npm install --prefix /tmp/csai-ui-tools playwright
/tmp/csai-ui-tools/node_modules/.bin/playwright install chromium
NODE_PATH=/tmp/csai-ui-tools/node_modules node scripts/check-ui-design-system.cjs
```

The script uses the real templates, styles and the real render functions of the selected modules with synthetic data; it does not start the backend or access any test target.
It checks the four screen widths 1440, 1024, 390, 360px and the light/dark themes, verifying drawer boundaries, internal horizontal overflow, the bottom bar, nesting levels, expansion, keyboard close, focus return, and horizontal overflow on the main page.
Screenshots and results are written to the system temp directory by default, or to a directory set via `CSAI_UI_ARTIFACTS`.
This covers the UI layout and shared interactions; it is not equivalent to full acceptance of real backend data, save operations, all dropdown states, or a physical phone's on-screen keyboard.

### Realistic data-shape regression

`check-ui-design-system.cjs` now calls `ui-realistic-scenarios.cjs` and loads the real i18next/translation implementation.
The regression data is built anonymously from the real pages' counts, text lengths and button states; it does not copy target addresses, credentials or business evidence.

- 365 queue tasks, rendered in paused, running and completed states; verifies that the real action buttons do not squeeze the title or overflow.
- 626 tools, 614 enabled tools and long descriptions; uses the real role editing entry to check statistics wrapping and body scrolling.
- Create/edit switching for roles and multi-file Skills, and crossing the desktop/phone breakpoint after opening the editor; verifies that a translation refresh does not change the editing title.
- 50 long vulnerability records and three pages of pagination: verifies full-page scrolling on phones, and that the title and pagination do not permanently occupy the record area.

All scenarios run at the four screen widths and two themes; the fixtures reject non-GET operations.
To target a single breakpoint, set `CSAI_UI_WIDTHS=390`; omit this variable before committing to run the full matrix.

### Queue dynamic text and button contrast

The queue title renders the translatable prefix and the as-is queue name separately; the execute button's `data-i18n` updates with the paused, pending and Cron states.
Small buttons for primary actions explicitly use an accent background and matching text color, avoiding old size styles that turned the background gray while keeping dark text.

The regression adds: paused/pending/running/completed/cancelled, the Cron toggle, Chinese/English switching, desktop and phone breakpoint switching, empty titles and titles with special characters.
For the visible start/continue/rerun buttons it checks the computed colors in the normal, hover and keyboard-focus states, requiring a text-to-opaque-background contrast of at least 4.5:1; the computed results per size and theme are saved as `queue-contrast-*.json`.
The browser regression uses the real i18next translation function and no longer overrides it with a fixed Chinese substitute function.

The Skill "view" entry dynamically creates `skill-view-modal`, sharing the wide drawer with the edit entry. The regression also verifies summary display, on-demand full-text loading, expansion, Escape cleanup and the "view → edit" switch, so that the read-only detail entry is not missed by only testing the edit form.

The full field text of info collection, the AI query parse result, and the WebShell database connection config also use the shared drawer. Info-collection dynamic nodes use the standard header/body/footer classes and the shared close logic; the regression uses fixtures with long text, warning notes and an offline database config, checking scrolling, repeated opening, copying, applying only the query syntax, and close cleanup, without running external queries or database connections.
