---
title: Download with Preview
---

The `preview` property can be set to `true` on `files:download-file` to display
a preview of the file contents in the code block part of the clickable action.
The file can still be downloaded by clicking on the action.

The markdown syntax for a download with preview is:

~~~markdown
```files:download-file
path: exercises/sample.txt
preview: true
```
~~~

Click below to see the preview and download the file:

```files:download-file
path: exercises/sample.txt
preview: true
```

The `preview` property can be combined with the `download` property to show a
preview while also using a custom download name:

~~~markdown
```files:download-file
path: exercises/sample.txt
download: sample-preview.txt
preview: true
```
~~~

Click below to see the combined preview and custom name download:

```files:download-file
path: exercises/sample.txt
download: sample-preview.txt
preview: true
```

It is recommended that the preview feature not be used for larger files.
