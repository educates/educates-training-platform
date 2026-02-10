---
title: "Workshop Summary"
---

This workshop tested the following editor clickable action features:

**File operations:**

- Open a file (`editor:open-file`)
- Open a file at a specific line
- Open with path variations (`~/`, `$HOME/`, relative)
- Create a new file (`editor:create-file`)
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
- Delete multiple lines from match (using `count`)
- Delete lines before and after match (using `before` and `after`)
- Delete lines matching a regex
- Replace a range of lines (`editor:replace-lines-in-range`)
- Replace a single line
- Replace lines at end of file
- Replace with more or fewer lines than original

**YAML operations:**

- Insert a value at a YAML path (`editor:insert-value-into-yaml`)
- Insert complex structures (lists, maps)
- Insert simple scalar values

**Other:**

- Execute VS Code commands (`editor:execute-command`)
- Execute commands with arguments
- Custom prefix, title, and description on editor actions
