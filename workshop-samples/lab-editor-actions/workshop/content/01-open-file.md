---
title: "Open File"
---

The `editor:open-file` clickable action opens a file in the embedded editor. You
can optionally specify a line number to position the cursor at.

For the exercises, a sample file has been created at `~/exercises/sample.txt`.

## Open a File

The markdown for opening a file is:

~~~markdown
```editor:open-file
file: ~/exercises/sample.txt
```
~~~

Click the action below to open the file in the editor:

```editor:open-file
file: ~/exercises/sample.txt
```

## Open a File at a Specific Line

The `line` property positions the cursor at the specified line number. Line
numbers start at 1.

The markdown for opening a file at line 3 is:

~~~markdown
```editor:open-file
file: ~/exercises/sample.txt
line: 3
```
~~~

Click the action below to open the file with the cursor at line 3:

```editor:open-file
file: ~/exercises/sample.txt
line: 3
```

## Open with Path Variations

The file path can use `~/` to indicate the home directory, `$HOME/` as an
alternative, or a relative path which is resolved relative to the home
directory.

Using `~/` prefix:

```editor:open-file
file: ~/exercises/sample.txt
```

Using `$HOME/` prefix:

```editor:open-file
file: $HOME/exercises/sample.txt
```

Using a relative path:

```editor:open-file
file: exercises/sample.txt
```
