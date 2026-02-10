---
title: "Replace Lines in Range"
---

The `editor:replace-lines-in-range` clickable action replaces a range of lines
in a file with new content. This is useful when workshop instructions need to
replace a known block of lines with different content.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/replace-range-test.txt
text: |
  Line 1: Header
  Line 2: Old content A
  Line 3: Old content B
  Line 4: Old content C
  Line 5: Footer
```

## Replace a Range of Lines

The `start` and `stop` properties specify the first and last line to replace
(both inclusive). The `text` property provides the replacement content.

The markdown for replacing a range of lines is:

~~~markdown
```editor:replace-lines-in-range
file: ~/exercises/replace-range-test.txt
start: 2
stop: 4
text: |
    Line 2: New content X
    Line 3: New content Y
```
~~~

Click the action below to replace lines 2-4 with new content:

```editor:replace-lines-in-range
file: ~/exercises/replace-range-test.txt
start: 2
stop: 4
text: |
    Line 2: New content X
    Line 3: New content Y
```

## Replace a Single Line

When `start` and `stop` are the same, a single line is replaced.

Reset the file:

```editor:create-file
file: ~/exercises/replace-range-test.txt
text: |
  Line 1: Header
  Line 2: Old content A
  Line 3: Old content B
  Line 4: Old content C
  Line 5: Footer
```

The markdown for replacing a single line is:

~~~markdown
```editor:replace-lines-in-range
file: ~/exercises/replace-range-test.txt
start: 3
stop: 3
text: |
    Line 3: Replaced content
```
~~~

Click the action below to replace just line 3:

```editor:replace-lines-in-range
file: ~/exercises/replace-range-test.txt
start: 3
stop: 3
text: |
    Line 3: Replaced content
```

## Replace Lines at End of File

Replacing lines at the end of the file also works correctly.

Reset the file:

```editor:create-file
file: ~/exercises/replace-range-test.txt
text: |
  Line 1: Header
  Line 2: Content
  Line 3: Old footer A
  Line 4: Old footer B
```

Click the action below to replace the last two lines:

```editor:replace-lines-in-range
file: ~/exercises/replace-range-test.txt
start: 3
stop: 4
text: |
    Line 3: New footer
```

## Replace with More Lines Than Original

The replacement text can have more or fewer lines than the original range.

Reset the file:

```editor:create-file
file: ~/exercises/replace-range-test.txt
text: |
  Line 1: Header
  Line 2: Single line to expand
  Line 3: Footer
```

Click the action below to replace a single line with multiple lines:

```editor:replace-lines-in-range
file: ~/exercises/replace-range-test.txt
start: 2
stop: 2
text: |
    Line 2: Expanded line A
    Line 3: Expanded line B
    Line 4: Expanded line C
```
