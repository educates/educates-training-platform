---
title: Workshop Summary
---

This workshop tested all terminal-related clickable actions supported by
Educates.

**Preferred actions tested:**

- `terminal:execute` — Basic execution, with `clear`, multi-line, and `session` targeting
- `terminal:execute-all` — Execution across all terminals, with `clear`
- `terminal:input` — Text input with and without newline, with session targeting
- `terminal:interrupt` — Interrupt specific terminal session
- `terminal:interrupt-all` — Interrupt all terminal sessions
- `terminal:clear` — Clear specific terminal buffer
- `terminal:clear-all` — Clear all terminal buffers
- `terminal:select` — Switch focus to specific terminal session

**Workshop actions and shortcodes tested:**

- `workshop:copy` — Copy single and multi-line text to paste buffer
- `workshop:copy-and-edit` — Copy text to paste buffer with edit indicator
- `{{</* copy */>}}` — Inline copy shortcode for code spans

**Deprecated actions tested:**

- `execute` — Legacy command execution and `<ctrl+c>` interrupt
- `execute-1`, `execute-2`, `execute-3` — Legacy terminal-specific execution
- `execute-all` — Legacy execution across all terminals
- `copy` — Legacy copy text to paste buffer
- `copy-and-edit` — Legacy copy text to paste buffer with edit indicator

All deprecated actions remain functional for backward compatibility but should
not be used in new workshop content. Use the `terminal:*` namespaced actions
instead.
