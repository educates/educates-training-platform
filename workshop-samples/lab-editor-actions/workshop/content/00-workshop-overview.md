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
- `editor:create-directory` — Create a new directory
- `editor:append-lines-to-file` — Append lines to the end of a file
- `editor:prepend-lines-to-file` — Prepend lines to the beginning of a file

**Line insertion:**

- `editor:insert-lines-before-line` — Insert lines before a specified line number
- `editor:insert-lines-after-line` — Insert lines after a specified line number (deprecated)
- `editor:append-lines-after-line` — Append lines after a specified line number
- `editor:append-lines-after-match` — Insert lines after a line matching a string
- `editor:insert-lines-before-match` — Insert lines before a line matching a string

**Text matching and replacement:**

- `editor:select-matching-text` — Select text by exact match or regular expression
- `editor:select-lines-in-range` — Select a range of lines by line number
- `editor:replace-text-selection` — Replace the currently selected text
- `editor:delete-text-selection` — Delete the currently selected text
- `editor:insert-lines-before-selection` — Insert lines before the current selection
- `editor:append-lines-after-selection` — Append lines after the current selection
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
- `editor:set-yaml-value` — Set or update a value at a YAML path
- `editor:add-yaml-item` — Append an item to a YAML sequence
- `editor:insert-yaml-item` — Insert an item at a specific position in a sequence
- `editor:replace-yaml-item` — Replace a sequence item by index or attribute match
- `editor:delete-yaml-value` — Delete a key or sequence item from a YAML file
- `editor:merge-yaml-values` — Merge key-value pairs into an existing mapping
- `editor:select-yaml-path` - Select a key or sequence item from a YAML file.

**Terminal operations:**

- `editor:open-terminal` — Open or create a terminal in the VS Code editor
- `editor:close-terminal` — Close a terminal in the VS Code editor
- `editor:send-to-terminal` — Send text or commands to a terminal
- `editor:interrupt-terminal` — Interrupt a running command in a terminal
- `editor:clear-terminal` — Clear a terminal buffer

**Other:**

- `editor:execute-command` — Execute a registered VS Code command
- Custom `prefix`, `title`, and `description` on editor actions
