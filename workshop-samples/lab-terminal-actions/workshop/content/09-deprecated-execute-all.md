---
title: "Deprecated: Execute All"
---

The `execute-all` code block annotation is the original legacy format for
executing a command in all terminal sessions. It has been superseded by
`terminal:execute-all`. Like the other deprecated formats, the body of the code
block is the command itself rather than YAML.

## Basic Execute All

The markdown for this action is:

~~~
```execute-all
echo "Hello from deprecated execute-all"
```
~~~

Click the action below to test it:

```execute-all
echo "Hello from deprecated execute-all"
```

After execution, the first terminal session is left selected.

## Clear All via Deprecated Execute All

A common use of the original `execute-all` was to clear all terminals.

The markdown for this action is:

~~~
```execute-all
clear
```
~~~

Click the action below to test it:

```execute-all
clear
```

Note that using `execute-all` with the `clear` command only clears the visible
portion of each terminal. The preferred `terminal:execute-all` with
`clear: true` clears the full buffer including scrollback.
