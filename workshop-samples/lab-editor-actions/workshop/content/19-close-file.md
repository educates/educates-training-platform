---
title: "Close File"
---

The `editor:close-file` clickable action closes a file tab in the editor.

First, open some files to work with:

```editor:create-file
file: ~/exercises/close-test-1.txt
text: |
  This file will be closed.
```

```editor:create-file
file: ~/exercises/close-test-2.txt
text: |
  This file will remain open.
```

## Close a File

The markdown for closing a file is:

~~~markdown
```editor:close-file
file: ~/exercises/close-test-1.txt
```
~~~

Click the action below to close the first file:

```editor:close-file
file: ~/exercises/close-test-1.txt
```

The first file tab should be closed while the second file remains open.

## Close a File That Is Not Open

Closing a file that is not currently open in the editor is a no-op and does not
produce an error.

```editor:close-file
file: ~/exercises/nonexistent-file.txt
```
