---
title: "Replace Text Selection"
---

The `editor:replace-text-selection` clickable action replaces text that has been
previously selected using `editor:select-matching-text`. This is a two-step
process: first select the text, then replace it.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/replace-test.txt
text: |
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: myapp
  spec:
    replicas: 1
    template:
      spec:
        containers:
        - name: myapp
          image: nginx:1.19
          ports:
          - containerPort: 80
```

## Select Then Replace

The typical workflow is to first select text, then replace it.

Step 1: Select the text to replace. The markdown is:

~~~markdown
```editor:select-matching-text
file: ~/exercises/replace-test.txt
text: "nginx:1.19"
```
~~~

Click the action below to select the text:

```editor:select-matching-text
file: ~/exercises/replace-test.txt
text: "nginx:1.19"
```

Step 2: Replace the selected text. The markdown is:

~~~markdown
```editor:replace-text-selection
file: ~/exercises/replace-test.txt
text: nginx:latest
```
~~~

Click the action below to replace the selected text:

```editor:replace-text-selection
file: ~/exercises/replace-test.txt
text: nginx:latest
```

## Replace with Multi-line Text

The replacement text can span multiple lines using the YAML block scalar syntax.

Reset the file:

```editor:create-file
file: ~/exercises/replace-test.txt
text: |
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: myapp
  spec:
    replicas: 1
    template:
      spec:
        containers:
        - name: myapp
          image: nginx:1.19
          ports:
          - containerPort: 80
```

Select the replicas line with before and after set to 0 to select the whole
line:

```editor:select-matching-text
file: ~/exercises/replace-test.txt
text: "replicas: 1"
before: 0
after: 0
```

Replace with multiple lines:

~~~markdown
```editor:replace-text-selection
file: ~/exercises/replace-test.txt
text: |2
    replicas: 3
    selector:
      matchLabels:
        app: myapp
```
~~~

Click the action below to replace with multi-line text:

```editor:replace-text-selection
file: ~/exercises/replace-test.txt
text: |2
    replicas: 3
    selector:
      matchLabels:
        app: myapp
```

## Replace with Regex Group Selection

You can use a regex to select a specific part of a line and replace only that
part.

Reset the file:

```editor:create-file
file: ~/exercises/replace-test.txt
text: |
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: myapp
  spec:
    replicas: 1
    template:
      spec:
        containers:
        - name: myapp
          image: nginx:1.19
          ports:
          - containerPort: 80
```

Select only the image tag using a regex group:

```editor:select-matching-text
file: ~/exercises/replace-test.txt
text: "image: (.*)"
isRegex: true
group: 1
```

Replace the selected group with a new image:

```editor:replace-text-selection
file: ~/exercises/replace-test.txt
text: alpine:3.18
```

## Select Lines by Range

The `editor:select-lines-in-range` clickable action selects a range of lines by
line number. This can be combined with `editor:replace-text-selection` to replace
specific lines.

Reset the file:

```editor:create-file
file: ~/exercises/replace-test.txt
text: |
  line 1: alpha
  line 2: bravo
  line 3: charlie
  line 4: delta
  line 5: echo
  line 6: foxtrot
```

Select a single line (line 3). The markdown is:

~~~markdown
```editor:select-lines-in-range
file: ~/exercises/replace-test.txt
start: 3
```
~~~

Click the action below to select line 3:

```editor:select-lines-in-range
file: ~/exercises/replace-test.txt
start: 3
```

Now select a range of lines (lines 2 to 4). The markdown is:

~~~markdown
```editor:select-lines-in-range
file: ~/exercises/replace-test.txt
start: 2
stop: 4
```
~~~

Click the action below to select lines 2 to 4:

```editor:select-lines-in-range
file: ~/exercises/replace-test.txt
start: 2
stop: 4
```

Replace the selected lines with new content:

```editor:replace-text-selection
file: ~/exercises/replace-test.txt
text: |2
    line 2: BRAVO
    line 3: CHARLIE
    line 4: DELTA
```
