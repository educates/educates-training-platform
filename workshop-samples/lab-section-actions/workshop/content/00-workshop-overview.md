---
title: Workshop Overview
---

This workshop is a test suite for verifying all section-related clickable
actions supported by Educates. It is not intended as an end-user workshop but
rather as a functional test to confirm that each action type behaves correctly.

The following clickable actions are tested across the workshop pages:

**Section actions:**

- `section:heading` — Non-interactive heading styled as a clickable action
- `section:begin` — Marks the beginning of a collapsible section
- `section:end` — Marks the end of a collapsible section

**Properties tested:**

- `name` — For matching begin/end pairs and enabling nesting
- `prefix` — Override the default "Section" prefix on the action title
- `title` — Custom title text for the action
- `description` — Custom body text for the action
- `autostart` — Automatically trigger actions when a section is expanded
- `cascade` — Trigger the next clickable action after successful completion
- `hidden` — Hide a clickable action from view while still allowing programmatic triggering
- `open` — Section is expanded by default when the page loads
- `toggle` — Control whether `section:end` toggles the section closed during cascade
- `cooldown` — Control how long a triggered state persists (`-1` for permanent)
- `pause` — Control delay before cascade continues to the next action
