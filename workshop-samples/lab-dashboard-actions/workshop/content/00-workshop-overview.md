---
title: Workshop Overview
---

This workshop is a test suite for verifying all dashboard-related clickable
actions supported by Educates. It is not intended as an end-user workshop but
rather as a functional test to confirm that each action type behaves correctly.

The workshop is configured with a pre-existing dashboard tab named "Example"
pointing to `https://www.example.com` which is used by various actions for
testing.

The following categories of clickable actions are tested across the workshop
pages:

**Preferred clickable actions (current format):**

- `dashboard:open-dashboard` — Open/focus an existing dashboard tab
- `dashboard:create-dashboard` — Create a new dashboard tab with a URL or terminal session
- `dashboard:reload-dashboard` — Reload an existing dashboard, optionally with a new URL
- `dashboard:delete-dashboard` — Delete a dashboard tab
- `dashboard:open-url` — Open a URL in a new browser window

**Deprecated clickable actions (legacy format):**

- `dashboard:expose-dashboard` — Legacy equivalent of `dashboard:open-dashboard`
