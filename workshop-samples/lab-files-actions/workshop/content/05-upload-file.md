---
title: Upload Single File
---

The `files:upload-file` clickable action allows uploading a single file to the
workshop session. The `path` property specifies the name the file will be saved
as in the uploads directory, which defaults to the `uploads` subdirectory of
the workshop user's home directory.

The markdown syntax for uploading a single named file is:

~~~markdown
```files:upload-file
path: uploaded-sample.txt
```
~~~

Click below, select a file from your local computer, and click the upload
button:

```files:upload-file
path: uploaded-sample.txt
```

After uploading, you can verify the file was received by listing the uploads
directory:

```terminal:execute
command: ls -la ~/uploads/
```

You can also view the contents of the uploaded file:

```terminal:execute
command: cat ~/uploads/uploaded-sample.txt
```
