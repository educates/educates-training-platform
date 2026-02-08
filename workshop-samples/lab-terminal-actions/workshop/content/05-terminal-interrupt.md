---
title: "Terminal Interrupt"
---

The `terminal:interrupt` action sends a Ctrl-C signal to a terminal session to
interrupt a running command. The `terminal:interrupt-all` variant sends the
interrupt to all terminal sessions simultaneously.

## Setup: Start a Long-Running Command

First, start a long-running command in terminal 1 that we can then interrupt.

```terminal:execute
command: while true; do echo "Running... $(date)"; sleep 1; done
session: 1
```

## Interrupt Terminal 1

The `terminal:interrupt` action targets terminal session 1 by default. You can
optionally specify the `session` property.

The markdown for this action is:

~~~markdown
```terminal:interrupt
session: 1
```
~~~

Click the action below to interrupt the running command:

```terminal:interrupt
session: 1
```

## Setup: Start Commands in Multiple Terminals

Start long-running commands in all terminals to test `terminal:interrupt-all`.

```terminal:execute-all
command: while true; do echo "Running... $(date)"; sleep 1; done
```

## Interrupt All Terminals

The `terminal:interrupt-all` action sends Ctrl-C to all terminal sessions on the
Terminal tab.

The markdown for this action is:

~~~markdown
```terminal:interrupt-all
```
~~~

Click the action below to interrupt all running commands:

```terminal:interrupt-all
```

## Interrupt All Using Session Wildcard

An alternative to `terminal:interrupt-all` is to use `terminal:interrupt` with
`session` set to `"*"`. This sends Ctrl-C to all terminal sessions in the same way.

First, start long-running commands in all terminals again.

```terminal:execute-all
command: while true; do echo "Running... $(date)"; sleep 1; done
```

The markdown for this action is:

~~~markdown
```terminal:interrupt
session: "*"
```
~~~

Click the action below to interrupt all running commands:

```terminal:interrupt
session: "*"
```
