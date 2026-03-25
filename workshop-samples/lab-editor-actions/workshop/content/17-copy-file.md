---
title: "Copy File"
---

The `editor:copy-file` clickable action copies a file to a new location.

First, create a source file to work with:

```editor:create-file
file: ~/exercises/copy-source.txt
text: |
  This is the source file content.
  It has multiple lines.
  Line 3 of source.
```

## Copy a File

The markdown for copying a file is:

~~~markdown
```editor:copy-file
src: ~/exercises/copy-source.txt
dest: ~/exercises/copy-dest.txt
```
~~~

Click the action below to copy the file:

```editor:copy-file
src: ~/exercises/copy-source.txt
dest: ~/exercises/copy-dest.txt
```

The destination file should now be open in the editor with the same content as
the source file.

## Copy Without Opening

You can set `open: false` to copy the file without opening it in the editor.

~~~markdown
```editor:copy-file
src: ~/exercises/copy-source.txt
dest: ~/exercises/copy-no-open.txt
open: false
```
~~~

Click the action below to copy without opening:

```editor:copy-file
src: ~/exercises/copy-source.txt
dest: ~/exercises/copy-no-open.txt
open: false
```

The file should be copied but not opened in the editor.

## Copy Overwrites Existing File

If the destination file already exists, it will be overwritten.

First, create a destination file with different content:

```editor:create-file
file: ~/exercises/copy-overwrite.txt
text: |
  This is the original content that will be overwritten.
```

Now copy the source over it:

```editor:copy-file
src: ~/exercises/copy-source.txt
dest: ~/exercises/copy-overwrite.txt
```

The destination file should now contain the source file content.
