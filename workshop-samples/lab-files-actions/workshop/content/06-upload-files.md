---
title: Upload Multiple Files
---

The `files:upload-files` clickable action allows uploading a set of arbitrarily
named files. All files selected for upload will be placed in the uploads
directory with names the same as the originals from the local machine.

The markdown syntax for uploading multiple files is:

~~~markdown
```files:upload-files
```
~~~

Click below, select one or more files from your local computer, and click the
upload button:

```files:upload-files
```

After uploading, you can verify the files were received by listing the uploads
directory:

```terminal:execute
command: ls -la ~/uploads/
```
