---
title: "Terminal Clear"
---

The `terminal:clear` action clears the full terminal buffer (including
scrollback) for a specific terminal session. The `terminal:clear-all` variant
clears all terminal sessions. These actions should have no effect when an
application running in the terminal is using visual mode.

Note: if you only want to clear the visible portion of the terminal (equivalent
to running the `clear` command), use `terminal:execute` with `command: clear`
instead.

## Setup: Generate Output

First, generate some output in all terminals so there is content to clear.

```terminal:execute-all
command: for i in $(seq 1 20); do echo "Line $i"; done
```

## Clear Terminal 1

The markdown for this action is:

~~~markdown
```terminal:clear
session: 1
```
~~~

Click the action below to clear terminal 1:

```terminal:clear
session: 1
```

## Clear All Terminals

The markdown for this action is:

~~~markdown
```terminal:clear-all
```
~~~

Click the action below to clear all terminals:

```terminal:clear-all
```

## Clear All Using Session Wildcard

An alternative to `terminal:clear-all` is to use `terminal:clear` with `session` set
to `"*"`. This clears all terminal sessions in the same way.

First, generate some output in all terminals again so there is content to clear.

```terminal:execute-all
command: for i in $(seq 1 20); do echo "Line $i"; done
```

The markdown for this action is:

~~~markdown
```terminal:clear
session: "*"
```
~~~

Click the action below to clear all terminals:

```terminal:clear
session: "*"
```
