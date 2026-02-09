---
title: Basic File Download
---

The `files:download-file` clickable action triggers saving of a file from the
workshop session to the local computer. The `path` property specifies the file
to download, relative to the home directory of the workshop user.

The markdown syntax for a basic file download is:

~~~markdown
```files:download-file
path: exercises/sample.txt
```
~~~

Click the action below to download the sample file:

```files:download-file
path: exercises/sample.txt
```

The name of the locally saved file will be the basename part of the path, that
is, with leading directories removed. In this case, the downloaded file will be
named `sample.txt`.
