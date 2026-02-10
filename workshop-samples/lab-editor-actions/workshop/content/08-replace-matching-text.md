---
title: "Replace Matching Text"
---

The `editor:replace-matching-text` clickable action finds and replaces text in a
single step, without needing to first select the text and then replace it
separately.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/replace-match-test.txt
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
        - name: nginx
          image: nginx:1.19
          ports:
          - containerPort: 80
        - name: sidecar
          image: busybox:1.35
```

## Simple Find and Replace

The `match` property specifies the text to find and the `replacement` property
specifies what to replace it with.

The markdown for a simple find and replace is:

~~~markdown
```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: "nginx:1.19"
replacement: "nginx:1.21"
```
~~~

Click the action below to replace the nginx image tag:

```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: "nginx:1.19"
replacement: "nginx:1.21"
```

## Replace Using Regular Expression

Setting `isRegex` to `true` allows the `match` property to be a regular
expression.

The markdown for a regex-based replacement is:

~~~markdown
```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: "image: (.*)"
replacement: "image: nginx:latest"
isRegex: true
group: 0
```
~~~

Click the action below to replace the first image line using a regex:

```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: "image: (.*)"
replacement: "image: nginx:latest"
isRegex: true
group: 0
```

## Replace with Search Range

The `start` and `stop` properties limit which lines are searched, allowing you
to target a specific match when there are multiple occurrences.

Reset the file first:

```editor:create-file
file: ~/exercises/replace-match-test.txt
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
        - name: nginx
          image: nginx:1.19
          ports:
          - containerPort: 80
        - name: sidecar
          image: busybox:1.35
```

The markdown for replacing within a line range is:

~~~markdown
```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: "image: (.*)"
replacement: "image: redis:7"
isRegex: true
group: 0
start: 14
stop: 18
```
~~~

Click the action below to replace only the sidecar image (the second `image:`
line):

```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: "image: (.*)"
replacement: "image: redis:7"
isRegex: true
group: 0
start: 14
stop: 18
```
