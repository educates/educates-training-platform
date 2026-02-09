---
title: Copy File to Clipboard
---

Instead of downloading a file, the `files:copy-file` clickable action copies
the contents of a file to the browser paste buffer.

The markdown syntax for copying a file to the clipboard is:

~~~markdown
```files:copy-file
path: exercises/sample.txt
```
~~~

Click below to copy the file contents to the clipboard:

```files:copy-file
path: exercises/sample.txt
```

After clicking, you can paste the content into another window or application to
verify it was copied correctly.

The `preview` property can also be used with `files:copy-file` to display the
file contents in the code block:

~~~markdown
```files:copy-file
path: exercises/sample.txt
preview: true
```
~~~

Click below to see the preview and copy the file contents:

```files:copy-file
path: exercises/sample.txt
preview: true
```
