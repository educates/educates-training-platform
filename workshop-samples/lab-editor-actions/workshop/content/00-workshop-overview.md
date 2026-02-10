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
- `editor:select-lines-in-range` — Select a range of lines by line number
- `editor:replace-text-selection` — Replace the currently selected text
- `editor:replace-matching-text` — Find and replace text in a single step

**Line deletion and replacement:**

- `editor:delete-lines-in-range` — Delete lines by line number range
- `editor:delete-matching-lines` — Delete lines matching a string or pattern
- `editor:replace-lines-in-range` — Replace a range of lines with new content

**File management:**

- `editor:copy-file` — Copy a file to a new location
- `editor:rename-file` — Rename or move a file
- `editor:close-file` — Close a file tab in the editor
- `editor:delete-file` — Delete a file from the file system

**YAML operations:**

- `editor:insert-value-into-yaml` — Insert a value into a YAML structure (deprecated)
- `editor:yaml-set-value` — Set or update a value at a YAML path
- `editor:yaml-add-item` — Append an item to a YAML sequence
- `editor:yaml-insert-item` — Insert an item at a specific position in a sequence
- `editor:yaml-replace-item` — Replace a sequence item by index or attribute match
- `editor:yaml-delete-value` — Delete a key or sequence item from a YAML file
- `editor:yaml-merge-values` — Merge key-value pairs into an existing mapping

**Other:**

- `editor:execute-command` — Execute a registered VS Code command
- Custom `prefix`, `title`, and `description` on editor actions
