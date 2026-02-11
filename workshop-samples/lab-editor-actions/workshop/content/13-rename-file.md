---
title: "Rename File"
---

The `editor:rename-file` clickable action renames or moves a file.

First, create a file to rename:

```editor:create-file
file: ~/exercises/rename-original.txt
text: |
  This file will be renamed.
  It has some content.
```

## Rename a File

The markdown for renaming a file is:

~~~markdown
```editor:rename-file
src: ~/exercises/rename-original.txt
dest: ~/exercises/rename-new-name.txt
```
~~~

Click the action below to rename the file:

```editor:rename-file
src: ~/exercises/rename-original.txt
dest: ~/exercises/rename-new-name.txt
```

The file should now have the new name and be open in the editor with the same
content.

## Rename Without Opening

You can set `open: false` to rename the file without opening it in the editor.

Create another file:

```editor:create-file
file: ~/exercises/rename-no-open-original.txt
text: |
  This file will be renamed without opening.
```

~~~markdown
```editor:rename-file
src: ~/exercises/rename-no-open-original.txt
dest: ~/exercises/rename-no-open-result.txt
open: false
```
~~~

Click the action below:

```editor:rename-file
src: ~/exercises/rename-no-open-original.txt
dest: ~/exercises/rename-no-open-result.txt
open: false
```

The file should be renamed but not opened in the editor.

## Move a File to a Different Directory

Rename can also be used to move a file to a different directory.

```editor:create-file
file: ~/exercises/move-me.txt
text: |
  This file will be moved to a subdirectory.
```

```editor:rename-file
src: ~/exercises/move-me.txt
dest: ~/exercises/subdir/move-me.txt
```

The file should now be in the subdirectory.
