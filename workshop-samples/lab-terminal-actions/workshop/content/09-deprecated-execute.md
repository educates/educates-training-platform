---
title: "Deprecated: Execute Action"
---

The `execute` code block annotation is the original legacy format for executing
commands in a terminal. It has been superseded by `terminal:execute` which uses
YAML-based configuration. The `execute` format is still supported for backward
compatibility but should not be used in new workshops.

With the deprecated format, the body of the code block is the command itself
(not YAML). By default it targets terminal session 1.

## Basic Execute

The markdown for this action is:

~~~
```execute
echo "Hello from deprecated execute"
```
~~~

Click the action below to test it:

```execute
echo "Hello from deprecated execute"
```

## Deprecated Ctrl-C via Execute

In the original format, sending Ctrl-C to interrupt a running command was done
by using the special string `<ctrl+c>` as the body of an `execute` block. This
is deprecated in favor of the `terminal:interrupt` action.

First, start a long-running command:

```execute
while true; do echo "Running... $(date)"; sleep 1; done
```

The markdown for the deprecated interrupt is:

~~~
```execute
<ctrl+c>
```
~~~

Click the action below to interrupt it using the deprecated method:

```execute
<ctrl+c>
```
