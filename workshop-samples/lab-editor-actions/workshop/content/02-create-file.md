---
title: "Create File"
---

The `editor:create-file` clickable action creates a new file with specified
content, or overwrites the contents of an existing file.

## Create a New File

The markdown for creating a new file is:

~~~markdown
```editor:create-file
file: ~/exercises/newfile.txt
text: |
    This is a newly created file.
    It was created by a clickable action.
```
~~~

Click the action below to create the file:

```editor:create-file
file: ~/exercises/newfile.txt
text: |
    This is a newly created file.
    It was created by a clickable action.
```

## Create a File with Structured Content

This is useful when workshop instructions need to provide the complete contents
of a configuration file.

The markdown for creating a YAML file is:

~~~markdown
```editor:create-file
file: ~/exercises/config.yaml
text: |
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: example
    data:
      key: value
```
~~~

Click the action below to create the YAML file:

```editor:create-file
file: ~/exercises/config.yaml
text: |
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: example
    data:
      key: value
```

## Overwrite an Existing File

If the file already exists, `editor:create-file` replaces all of its content.

Click the action below to overwrite the file created above with new content:

```editor:create-file
file: ~/exercises/newfile.txt
text: |
    This content has replaced the original.
    The file was overwritten by a clickable action.
```
