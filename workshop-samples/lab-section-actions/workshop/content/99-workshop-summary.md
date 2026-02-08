---
title: Workshop Summary
---

This workshop tested all section-related clickable actions supported by
Educates.

**Section actions tested:**

- `section:heading` — Non-interactive heading with custom prefix, title, and description
- `section:begin` / `section:end` — Basic collapsible sections with toggle behavior

**Features tested:**

- Basic collapsible sections (unnamed)
- Named sections for independent expand/collapse on the same page
- Nested sections at multiple levels (2 and 3 deep)
- Multiple sibling inner sections within an outer section
- Custom `prefix`, `title`, and `description` properties
- `autostart` within sections — actions only trigger on section expand, not page load
- `autostart` in nested sections — inner autostart triggers on inner expand only
- Multiple autostart actions in a single section
- `cascade` to `section:end` — auto-closing sections after action completion
- Chained cascade through multiple actions before closing
- `autostart` combined with `cascade` for auto-run and auto-close
- Cascade close in nested sections — inner section closes, outer stays open
- Re-expanding sections after cascade close
- `hidden` sections revealed by cascade
- `hidden` sections with `autostart` for auto-expanded content on page load
- Hidden sections nested inside visible sections
