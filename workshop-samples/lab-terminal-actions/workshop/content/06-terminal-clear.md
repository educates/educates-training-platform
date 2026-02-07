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

First, generate some output in terminals 1 and 2 so there is content to clear.

```terminal:execute
command: for i in $(seq 1 20); do echo "Terminal 1 - Line $i"; done
session: 1
```

```terminal:execute
command: for i in $(seq 1 20); do echo "Terminal 2 - Line $i"; done
session: 2
```

## Clear Terminal 1

The markdown for this action is:

~~~
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

~~~
```terminal:clear-all
```
~~~

Click the action below to clear all terminals:

```terminal:clear-all
```
