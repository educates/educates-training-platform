---
title: "Insert Lines Before and After"
---

The `editor:insert-lines-before-line` and `editor:insert-lines-after-line`
clickable actions insert text at a specific line position in a file.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/insert-test.txt
text: |
  Line 1: First line
  Line 2: Second line
  Line 3: Third line
  Line 4: Fourth line
  Line 5: Fifth line
```

## Insert Lines Before a Line

The `editor:insert-lines-before-line` action inserts text before the specified
line number.

The markdown for inserting lines before line 3 is:

~~~markdown
```editor:insert-lines-before-line
file: ~/exercises/insert-test.txt
line: 3
text: |
    --- Inserted before line 3 ---
```
~~~

Click the action below to insert a line before line 3:

```editor:insert-lines-before-line
file: ~/exercises/insert-test.txt
line: 3
text: |
    --- Inserted before line 3 ---
```

## Insert Lines After a Line

The `editor:insert-lines-after-line` action inserts text after the specified
line number.

The markdown for inserting lines after line 3 is:

~~~markdown
```editor:insert-lines-after-line
file: ~/exercises/insert-test.txt
line: 3
text: |
    --- Inserted after line 3 ---
```
~~~

Reset the file first, then try inserting after a line:

```editor:create-file
file: ~/exercises/insert-test.txt
text: |
  Line 1: First line
  Line 2: Second line
  Line 3: Third line
  Line 4: Fourth line
  Line 5: Fifth line
```

Click the action below to insert a line after line 3:

```editor:insert-lines-after-line
file: ~/exercises/insert-test.txt
line: 3
text: |
    --- Inserted after line 3 ---
```

## Insert Multiple Lines

Both actions support inserting multiple lines at once using the YAML block
scalar syntax.

Reset the file:

```editor:create-file
file: ~/exercises/insert-test.txt
text: |
  Line 1: First line
  Line 2: Second line
  Line 3: Third line
```

The markdown for inserting multiple lines before line 2 is:

~~~markdown
```editor:insert-lines-before-line
file: ~/exercises/insert-test.txt
line: 2
text: |
    --- Inserted block start ---
    This is a multi-line insertion.
    --- Inserted block end ---
```
~~~

Click the action below to insert multiple lines:

```editor:insert-lines-before-line
file: ~/exercises/insert-test.txt
line: 2
text: |
    --- Inserted block start ---
    This is a multi-line insertion.
    --- Inserted block end ---
```
