---
title: "Delete File"
---

The `editor:delete-file` clickable action deletes a file from the file system
and closes it in the editor if it is open.

First, create some files to work with:

```editor:create-file
file: ~/exercises/delete-test-1.txt
text: |
  This file will be deleted.
```

```editor:create-file
file: ~/exercises/delete-test-2.txt
text: |
  This file will also be deleted.
```

## Delete an Open File

The markdown for deleting a file is:

~~~markdown
```editor:delete-file
file: ~/exercises/delete-test-1.txt
```
~~~

Click the action below to delete the file:

```editor:delete-file
file: ~/exercises/delete-test-1.txt
```

The file should be removed from the file system and its editor tab should be
closed.

## Delete a Closed File

You can also delete a file that is not currently open in the editor. Close the
second file first:

```editor:close-file
file: ~/exercises/delete-test-2.txt
```

Now delete it:

```editor:delete-file
file: ~/exercises/delete-test-2.txt
```

The file should be removed from the file system.

## Verify Deletion

You can verify the files have been deleted by checking the exercises directory:

```terminal:execute
command: ls ~/exercises/delete-test-*.txt 2>&1 || true
```

The files should no longer exist.
