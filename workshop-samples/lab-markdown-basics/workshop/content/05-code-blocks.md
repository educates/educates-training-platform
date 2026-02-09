---
title: Code Blocks
---

Markdown supports both inline code and fenced code blocks with optional syntax
highlighting.

## Fenced Code Blocks

A fenced code block uses triple backticks. You can specify a language for
syntax highlighting.

### Python

The markdown for a Python code block is:

~~~markdown
```python
def greet(name):
    return f"Hello, {name}!"

print(greet("World"))
```
~~~

```python
def greet(name):
    return f"Hello, {name}!"

print(greet("World"))
```

### YAML

The markdown for a YAML code block is:

~~~markdown
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: example-config
data:
  key: value
  nested:
    enabled: true
```
~~~

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: example-config
data:
  key: value
  nested:
    enabled: true
```

### Bash

The markdown for a Bash code block is:

~~~markdown
```bash
#!/bin/bash
for i in {1..5}; do
    echo "Iteration $i"
done
```
~~~

```bash
#!/bin/bash
for i in {1..5}; do
    echo "Iteration $i"
done
```

### JSON

The markdown for a JSON code block is:

~~~markdown
```json
{
  "name": "educates",
  "version": "1.0.0",
  "features": ["workshops", "terminals", "dashboards"]
}
```
~~~

```json
{
  "name": "educates",
  "version": "1.0.0",
  "features": ["workshops", "terminals", "dashboards"]
}
```

### Plain Text

The markdown for a plain code block with no language specified is:

~~~markdown
```
This is a plain code block with no syntax highlighting.
It preserves whitespace and formatting exactly as written.
```
~~~

```
This is a plain code block with no syntax highlighting.
It preserves whitespace and formatting exactly as written.
```
