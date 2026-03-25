---
title: "Delete Text Selection"
---

The `editor:delete-text-selection` clickable action deletes text that has been
previously selected using `editor:select-matching-text` or
`editor:select-lines-in-range`. This is a two-step process: first select the
text, then delete it.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/delete-selection-test.txt
text: |
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: myapp
    labels:
      app: myapp
      version: v1
  spec:
    replicas: 1
```

## Select Then Delete

The typical workflow is to first select text, then delete it.

Step 1: Select the text to delete. Click the action below to select the
`labels` block:

```editor:select-matching-text
file: ~/exercises/delete-selection-test.txt
text: "labels:"
before: 0
after: 2
```

Step 2: Delete the selected text. The markdown is:

~~~markdown
```editor:delete-text-selection
file: ~/exercises/delete-selection-test.txt
```
~~~

Click the action below to delete the selected text:

```editor:delete-text-selection
file: ~/exercises/delete-selection-test.txt
```

## Delete a Single Line Selection

Reset the file:

```editor:create-file
file: ~/exercises/delete-selection-test.txt
text: |
  line 1: alpha
  line 2: bravo
  line 3: charlie
  line 4: delta
  line 5: echo
```

Select a single line using `editor:select-lines-in-range`:

```editor:select-lines-in-range
file: ~/exercises/delete-selection-test.txt
start: 3
```

Delete the selected line:

```editor:delete-text-selection
file: ~/exercises/delete-selection-test.txt
```

## Delete a Regex Group Selection

Reset the file:

```editor:create-file
file: ~/exercises/delete-selection-test.txt
text: |
  name: myapp-v2-beta
  image: nginx:latest
```

Select only the `-beta` suffix using a regex group:

```editor:select-matching-text
file: ~/exercises/delete-selection-test.txt
text: "myapp-v2(-beta)"
isRegex: true
group: 1
```

Delete just the selected group:

```editor:delete-text-selection
file: ~/exercises/delete-selection-test.txt
```
