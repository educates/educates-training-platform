---
title: "Workshop Summary"
---

This workshop tested the following editor clickable action features:

**File operations:**

- Open a file (`editor:open-file`)
- Open a file at a specific line
- Open with path variations (`~/`, `$HOME/`, relative)
- Create a new file (`editor:create-file`)
- Create a directory (`editor:create-directory`)
- Create a file in a new directory
- Overwrite an existing file
- Append lines to a file (`editor:append-lines-to-file`)
- Append to a non-existent file (creates it)

**Line insertion:**

- Insert lines before a line number (`editor:insert-lines-before-line`)
- Insert lines after a line number (`editor:insert-lines-after-line`)
- Insert multiple lines at once
- Insert lines after a matching string (`editor:append-lines-after-match`)

**Text selection:**

- Select text by exact match (`editor:select-matching-text`)
- Select with before and after context lines
- Select entire line (before and after set to 0)
- Select all lines before or after match (using -1)
- Select by regular expression (`isRegex`)
- Select a regex subgroup (`group`)
- Limit search range (`start` and `stop`)
- Negative line offsets for search range
- Select a range of lines by number (`editor:select-lines-in-range`)
- Select a single line by number
- Select then replace lines

**Text replacement:**

- Replace selected text (`editor:replace-text-selection`)
- Replace with multi-line text
- Replace a regex group selection
- Find and replace in one step (`editor:replace-matching-text`)
- Replace using regular expression
- Replace within a search range

**Line deletion and replacement:**

- Delete a single line by number (`editor:delete-lines-in-range`)
- Delete a range of lines
- Delete lines at end of file
- Delete a matching line (`editor:delete-matching-lines`)
- Delete lines before and after match (using `before` and `after`)
- Delete lines matching a regex
- Replace a range of lines (`editor:replace-lines-in-range`)
- Replace a single line
- Replace lines at end of file
- Replace with more or fewer lines than original

**File management:**

- Copy a file (`editor:copy-file`)
- Copy without opening in editor
- Copy overwrites existing destination
- Rename a file (`editor:rename-file`)
- Rename without opening in editor
- Move a file to a different directory
- Close a file tab (`editor:close-file`)
- Close a file that is not open (no-op)
- Delete a file (`editor:delete-file`)
- Delete an open file (closes tab and deletes)
- Delete a closed file

**YAML operations:**

- Set a value at a YAML path (`editor:yaml-set-value`)
- Set nested values with automatic intermediate key creation
- Add an item to a sequence (`editor:yaml-add-item`)
- Insert an item at a specific index (`editor:yaml-insert-item`)
- Replace a sequence item by index or attribute match (`editor:yaml-replace-item`)
- Delete a mapping key or sequence item (`editor:yaml-delete-value`)
- Delete by attribute match (`[key=value]` syntax)
- Merge values into a mapping (`editor:yaml-merge-values`)
- Select a YAML path (`editor:yaml-select-path`)
- Select scalars, mappings, sequences, and nested values
- Select sequence items by index or attribute match
- Comment preservation across operations
- Flow-style (inline) YAML support

**Terminal operations:**

- Open a terminal with default session (`editor:open-terminal`)
- Open a terminal with a numeric session name (string coercion)
- Open a terminal with a named session
- Send a command to a terminal (`editor:send-to-terminal`)
- Send a command using a numeric session
- Send text without appending newline (`endl: false`)
- Interrupt a running command (`editor:interrupt-terminal`)
- Clear a terminal (`editor:clear-terminal`)
- Close a terminal (`editor:close-terminal`)
- Close the default terminal

**Other:**

- Execute VS Code commands (`editor:execute-command`)
- Execute commands with arguments
- Custom prefix, title, and description on editor actions
