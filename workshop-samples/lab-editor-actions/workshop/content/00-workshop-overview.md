---
title: Workshop Overview
---

This workshop is a test suite for verifying the editor clickable actions
supported by Educates. It is not intended as an end-user workshop but rather as
a functional test to confirm that the editor actions behave correctly across all
their configuration options.

The following features are tested across the workshop pages:

**File operations:**

- `editor:open-file` — Open a file, optionally at a specific line
- `editor:create-file` — Create a new file or overwrite an existing file
- `editor:append-lines-to-file` — Append lines to the end of a file

**Line insertion:**

- `editor:insert-lines-before-line` — Insert lines before a specified line number
- `editor:insert-lines-after-line` — Insert lines after a specified line number
- `editor:append-lines-after-match` — Insert lines after a line matching a string

**Text matching and replacement:**

- `editor:select-matching-text` — Select text by exact match or regular expression
- `editor:replace-text-selection` — Replace the currently selected text
- `editor:replace-matching-text` — Find and replace text in a single step

**Other:**

- `editor:insert-value-into-yaml` — Insert a value into a YAML structure
- `editor:execute-command` — Execute a registered VS Code command
- Custom `prefix`, `title`, and `description` on editor actions
