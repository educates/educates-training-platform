---
title: "Append Lines to File"
---

The `editor:append-lines-to-file` clickable action appends text to the end of
an existing file. If the file does not exist, it will be created.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/append-test.txt
text: |
  Line 1: Original content
  Line 2: Original content
```

## Append Lines to a File

The markdown for appending lines to a file is:

~~~markdown
```editor:append-lines-to-file
file: ~/exercises/append-test.txt
text: |
    Line 3: Appended content
    Line 4: Appended content
```
~~~

Click the action below to append lines to the file:

```editor:append-lines-to-file
file: ~/exercises/append-test.txt
text: |
    Line 3: Appended content
    Line 4: Appended content
```

## Append to a Non-existent File

If the file does not exist, `editor:append-lines-to-file` will create it. This
can be used as an alternative to `editor:create-file` when you want to build up
a file incrementally.

~~~markdown
```editor:append-lines-to-file
file: ~/exercises/created-by-append.txt
text: |
    This file was created by appending to a non-existent file.
```
~~~

Click the action below to create a new file by appending to it:

```editor:append-lines-to-file
file: ~/exercises/created-by-append.txt
text: |
    This file was created by appending to a non-existent file.
```
