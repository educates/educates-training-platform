---
title: Download with Custom Name
---

If you want the file saved locally under a different name, add the `download`
property to the `files:download-file` action. The local filename cannot include
a directory path.

The markdown syntax for a download with a custom name is:

~~~markdown
```files:download-file
path: exercises/sample.txt
download: my-custom-sample.txt
```
~~~

Click below to download the file with a custom name:

```files:download-file
path: exercises/sample.txt
download: my-custom-sample.txt
```

Data variables can also be used in the `download` property. For example, to
include the session name in the downloaded filename:

~~~markdown
```files:download-file
path: exercises/sample.txt
download: sample-{{</* param session_name */>}}.txt
```
~~~

Click below to download the file with the session name included:

```files:download-file
path: exercises/sample.txt
download: sample-{{< param session_name >}}.txt
```
