---
title: "Select Matching Text"
---

The `editor:select-matching-text` clickable action highlights text in a file
based on an exact string match or a regular expression. This is often used
before `editor:replace-text-selection` to show what will be replaced.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/select-test.txt
text: |
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: nginx-deployment
  spec:
    replicas: 3
    template:
      spec:
        containers:
        - name: nginx
          image: nginx:1.19
          ports:
          - containerPort: 80
        - name: sidecar
          image: busybox:latest
```

## Exact Text Match

The markdown for selecting text by exact match is:

~~~markdown
```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "nginx:1.19"
```
~~~

Click the action below to select the text "nginx:1.19" in the file:

```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "nginx:1.19"
```

## Selecting with Before and After Context

The `before` and `after` properties control how many additional lines above and
below the matched line are highlighted.

The markdown for selecting with context lines is:

~~~markdown
```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "image: nginx:1.19"
before: 1
after: 1
```
~~~

Click the action below to select the line with context:

```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "image: nginx:1.19"
before: 1
after: 1
```

## Select Entire Line

Setting both `before` and `after` to `0` results in the complete line being
highlighted instead of just the matched region.

~~~markdown
```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "replicas: 3"
before: 0
after: 0
```
~~~

Click the action below to select the full line:

```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "replicas: 3"
before: 0
after: 0
```

## Select All Lines Before or After

Setting `before` or `after` to `-1` selects all lines before or after the
match.

~~~markdown
```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "replicas: 3"
before: -1
after: 0
```
~~~

Click the action below to select everything from the start of the file to the
matched line:

```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "replicas: 3"
before: -1
after: 0
```

## Regular Expression Match

Setting `isRegex` to `true` allows matching with a regular expression.

The markdown for a regex match is:

~~~markdown
```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "image: (.*)"
isRegex: true
```
~~~

Click the action below to match using a regular expression. The entire match
will be highlighted:

```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "image: (.*)"
isRegex: true
```

## Regex with Group Selection

When a regular expression contains subgroups, the `group` property specifies
which subgroup to select.

The markdown for selecting a regex subgroup is:

~~~markdown
```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "image: (.*)"
isRegex: true
group: 1
```
~~~

Click the action below to select only the captured group (the image value):

```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "image: (.*)"
isRegex: true
group: 1
```

## Limiting Search Range

The `start` and `stop` properties limit the range of lines to search. This is
useful when there are multiple matches and you want to target a specific one.
The line number given by `stop` is not included in the search.

The markdown for searching a specific line range is:

~~~markdown
```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "image: (.*)"
isRegex: true
start: 14
stop: 17
```
~~~

Click the action below to match the second `image:` line (the sidecar
container):

```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "image: (.*)"
isRegex: true
start: 14
stop: 17
```

## Negative Line Offsets

When `start` or `stop` is a negative value, it is interpreted as an offset from
the end of the file.

~~~markdown
```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "image: (.*)"
isRegex: true
start: -5
```
~~~

Click the action below to search only the last 5 lines of the file:

```editor:select-matching-text
file: ~/exercises/select-test.txt
text: "image: (.*)"
isRegex: true
start: -5
```

## Multi-line Exact Match

The `text` property can span multiple lines using the YAML block scalar syntax.
This allows selecting a block of text that crosses line boundaries.

The markdown for a multi-line exact match is:

~~~markdown
```editor:select-matching-text
file: ~/exercises/select-test.txt
text: |2-
        containers:
        - name: nginx
          image: nginx:1.19
```
~~~

Note the use of `|2-` with an indentation indicator to preserve the leading
whitespace that appears in the file. The indicator value `2` tells the YAML
parser how many characters of indentation belong to the YAML structure rather
than the content, so remaining spaces become part of the matched string.

Click the action below to select the multi-line block:

```editor:select-matching-text
file: ~/exercises/select-test.txt
text: |2-
        containers:
        - name: nginx
          image: nginx:1.19
```

## Multi-line Regex Match

Regular expressions can also span multiple lines. Literal newlines in the
pattern match newlines in the file, while `.` still matches any character
except a newline, so patterns behave predictably across line boundaries.

The markdown for a multi-line regex match is:

~~~markdown
```editor:select-matching-text
file: ~/exercises/select-test.txt
text: |2-
        - name: (.*)
          image: (.*)
isRegex: true
```
~~~

Click the action below to match a container name and image pair across two
lines:

```editor:select-matching-text
file: ~/exercises/select-test.txt
text: |2-
        - name: (.*)
          image: (.*)
isRegex: true
```
