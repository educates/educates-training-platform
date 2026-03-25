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

## Replace All Matches

By default only the first match is replaced. The `count` property controls how
many matches to replace. Setting `count` to `-1` replaces all matches.

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

The markdown for replacing all matches is:

~~~markdown
```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: "image: (.*)"
replacement: "image: updated:latest"
isRegex: true
group: 0
count: -1
```
~~~

Click the action below to replace all image lines at once:

```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: "image: (.*)"
replacement: "image: updated:latest"
isRegex: true
group: 0
count: -1
```

You can also set `count` to a specific number to limit how many matches are
replaced. For example, `count: 2` would replace the first two matches.

## Multi-line Find and Replace

Both the `match` and `replacement` properties can span multiple lines using the
YAML block scalar syntax. This allows replacing entire blocks of text in a
single step.

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

The markdown for a multi-line find and replace is:

~~~markdown
```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: |2-
        - name: nginx
          image: nginx:1.19
          ports:
          - containerPort: 80
replacement: |2-
        - name: nginx
          image: nginx:1.25
          ports:
          - containerPort: 8080
```
~~~

Note the use of `|2-` with an indentation indicator to preserve the leading
whitespace that appears in the file. The indicator value `2` tells the YAML
parser how many characters of indentation belong to the YAML structure rather
than the content, so remaining spaces become part of the matched string.

Click the action below to replace the entire nginx container block:

```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: |2-
        - name: nginx
          image: nginx:1.19
          ports:
          - containerPort: 80
replacement: |2-
        - name: nginx
          image: nginx:1.25
          ports:
          - containerPort: 8080
```

## Multi-line Regex Replace

Regular expressions can also span multiple lines. Literal newlines in the
pattern match newlines in the file, while `.` matches any character except a
newline.

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

The markdown for a multi-line regex replacement is:

~~~markdown
```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: |2-
        - name: (.*)
          image: busybox:(.*)
replacement: |2-
        - name: helper
          image: alpine:3.18
isRegex: true
```
~~~

Click the action below to replace the sidecar container name and image using a
multi-line regex:

```editor:replace-matching-text
file: ~/exercises/replace-match-test.txt
match: |2-
        - name: (.*)
          image: busybox:(.*)
replacement: |2-
        - name: helper
          image: alpine:3.18
isRegex: true
```
