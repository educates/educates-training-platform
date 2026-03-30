---
title: "Append Lines After Line"
---

The `editor:append-lines-after-line` clickable action appends text after a
specified line number in a file. This is the preferred action name for inserting
lines after a line number — the older `editor:insert-lines-after-line` action
is deprecated but still works.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/append-after-line-test.txt
text: |
  Line 1: First line
  Line 2: Second line
  Line 3: Third line
  Line 4: Fourth line
  Line 5: Fifth line
```

## Append Lines After a Line

The markdown for appending lines after line 3 is:

~~~markdown
```editor:append-lines-after-line
file: ~/exercises/append-after-line-test.txt
line: 3
text: |
    --- Appended after line 3 ---
```
~~~

Click the action below to append a line after line 3:

```editor:append-lines-after-line
file: ~/exercises/append-after-line-test.txt
line: 3
text: |
    --- Appended after line 3 ---
```

## Append Multiple Lines After a Line

Reset the file:

```editor:create-file
file: ~/exercises/append-after-line-test.txt
text: |
  Line 1: First line
  Line 2: Second line
  Line 3: Third line
```

The markdown for appending multiple lines after line 1 is:

~~~markdown
```editor:append-lines-after-line
file: ~/exercises/append-after-line-test.txt
line: 1
text: |
    --- Appended block start ---
    This is a multi-line insertion after line 1.
    --- Appended block end ---
```
~~~

Click the action below to append multiple lines:

```editor:append-lines-after-line
file: ~/exercises/append-after-line-test.txt
line: 1
text: |
    --- Appended block start ---
    This is a multi-line insertion after line 1.
    --- Appended block end ---
```
