---
title: "Delete Lines in Range"
---

The `editor:delete-lines-in-range` clickable action deletes lines from a file by
line number range.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/delete-range-test.txt
text: |
  Line 1: First line
  Line 2: Second line
  Line 3: Third line
  Line 4: Fourth line
  Line 5: Fifth line
  Line 6: Sixth line
  Line 7: Seventh line
  Line 8: Eighth line
```

## Delete a Single Line

When only `start` is specified, a single line is deleted.

The markdown for deleting a single line is:

~~~markdown
```editor:delete-lines-in-range
file: ~/exercises/delete-range-test.txt
start: 3
```
~~~

Click the action below to delete line 3:

```editor:delete-lines-in-range
file: ~/exercises/delete-range-test.txt
start: 3
```

## Delete a Range of Lines

When both `start` and `stop` are specified, all lines in the range (inclusive)
are deleted.

Reset the file first:

```editor:create-file
file: ~/exercises/delete-range-test.txt
text: |
  Line 1: First line
  Line 2: Second line
  Line 3: Third line
  Line 4: Fourth line
  Line 5: Fifth line
  Line 6: Sixth line
  Line 7: Seventh line
  Line 8: Eighth line
```

The markdown for deleting a range of lines is:

~~~markdown
```editor:delete-lines-in-range
file: ~/exercises/delete-range-test.txt
start: 3
stop: 6
```
~~~

Click the action below to delete lines 3 through 6:

```editor:delete-lines-in-range
file: ~/exercises/delete-range-test.txt
start: 3
stop: 6
```

## Delete Lines at End of File

Deleting lines at the end of the file also works correctly.

Reset the file:

```editor:create-file
file: ~/exercises/delete-range-test.txt
text: |
  Line 1: First line
  Line 2: Second line
  Line 3: Third line
  Line 4: Fourth line
  Line 5: Fifth line
```

Click the action below to delete the last two lines:

```editor:delete-lines-in-range
file: ~/exercises/delete-range-test.txt
start: 4
stop: 5
```
