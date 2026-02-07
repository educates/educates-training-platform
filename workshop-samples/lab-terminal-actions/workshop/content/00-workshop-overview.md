---
title: Workshop Overview
---

This workshop is a test suite for verifying all terminal-related clickable
actions supported by Educates. It is not intended as an end-user workshop but
rather as a functional test to confirm that each action type behaves correctly.

The workshop is configured with a `split/2` terminal layout providing three
terminal sessions in the Terminal tab, plus an additional dashboard tab named
"Terminal#4" providing access to a fourth terminal session.

The following categories of clickable actions are tested across the workshop
pages:

**Preferred clickable actions (current format):**

- `terminal:execute` — Execute a command in a specific terminal session
- `terminal:execute-all` — Execute a command in all terminal sessions
- `terminal:input` — Send text to a terminal without automatic execution
- `terminal:interrupt` — Send Ctrl-C to a specific terminal session
- `terminal:interrupt-all` — Send Ctrl-C to all terminal sessions
- `terminal:clear` — Clear a specific terminal session
- `terminal:clear-all` — Clear all terminal sessions

**Deprecated clickable actions (legacy format):**

- `execute` — Legacy equivalent of `terminal:execute`
- `execute-1`, `execute-2`, `execute-3` — Legacy terminal-specific execution
- `execute-all` — Legacy equivalent of `terminal:execute-all`
