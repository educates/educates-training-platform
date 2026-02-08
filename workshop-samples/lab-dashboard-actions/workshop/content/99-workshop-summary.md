---
title: Workshop Summary
---

This workshop tested all dashboard-related clickable actions supported by
Educates.

**Preferred actions tested:**

- `dashboard:open-dashboard` — Open/focus an existing dashboard tab
- `dashboard:create-dashboard` — Create dashboards with URLs and terminal sessions, with and without focus
- `dashboard:reload-dashboard` — Reload dashboards with same or new URL, with and without focus
- `dashboard:delete-dashboard` — Delete custom dashboards, including terminal session dashboards
- `dashboard:open-url` — Open URLs in a new browser window

**Deprecated actions tested:**

- `dashboard:expose-dashboard` — Legacy equivalent of `dashboard:open-dashboard`

All deprecated actions remain functional for backward compatibility but should
not be used in new workshop content. Use the `dashboard:open-dashboard` action
instead.
