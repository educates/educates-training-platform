---
title: "Prepend Lines to File"
---

The `editor:prepend-lines-to-file` clickable action inserts text at the beginning
of a file. If the file does not exist, it will be created.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/prepend-test.txt
text: |
  Line 1: Original first line
  Line 2: Original second line
  Line 3: Original third line
```

## Prepend Lines to a File

The markdown for prepending lines to a file is:

~~~markdown
```editor:prepend-lines-to-file
file: ~/exercises/prepend-test.txt
text: |
    Line 0: Prepended content
```
~~~

Click the action below to prepend a line to the file:

```editor:prepend-lines-to-file
file: ~/exercises/prepend-test.txt
text: |
    Line 0: Prepended content
```

## Prepend Multiple Lines

You can prepend multiple lines at once using the YAML block scalar syntax.

Reset the file:

```editor:create-file
file: ~/exercises/prepend-test.txt
text: |
  Line 1: Original first line
  Line 2: Original second line
  Line 3: Original third line
```

The markdown for prepending multiple lines is:

~~~markdown
```editor:prepend-lines-to-file
file: ~/exercises/prepend-test.txt
text: |
    --- Header start ---
    This is a prepended header block.
    --- Header end ---
```
~~~

Click the action below to prepend multiple lines:

```editor:prepend-lines-to-file
file: ~/exercises/prepend-test.txt
text: |
    --- Header start ---
    This is a prepended header block.
    --- Header end ---
```

## Prepend to a Non-existent File

If the file does not exist, `editor:prepend-lines-to-file` will create it, similar
to `editor:append-lines-to-file`.

~~~markdown
```editor:prepend-lines-to-file
file: ~/exercises/created-by-prepend.txt
text: |
    This file was created by prepending to a non-existent file.
```
~~~

Click the action below to create a new file by prepending to it:

```editor:prepend-lines-to-file
file: ~/exercises/created-by-prepend.txt
text: |
    This file was created by prepending to a non-existent file.
```
