---
title: "Terminal Execute"
---

The `terminal:execute` action executes a command in a terminal session. By
default the command is directed to terminal session 1. The content of the code
block is YAML with the command specified via the `command` property.

## Basic Command Execution

This test executes a simple echo command in the default terminal (session 1).

The markdown for this action is:

~~~
```terminal:execute
command: |-
  echo "Hello from terminal:execute"
```
~~~

Click the action below to test it:

```terminal:execute
command: |-
  echo "Hello from terminal:execute"
```

## Command with Clear

The `clear` property can be set to `true` to clear the full terminal buffer
before executing the command. This clears the entire scrollback buffer, not just
the visible portion.

The markdown for this action is:

~~~
```terminal:execute
command: echo "Terminal was cleared before this command"
clear: true
```
~~~

Click the action below to test it:

```terminal:execute
command: echo "Terminal was cleared before this command"
clear: true
```

## Multi-line Command as Separate Commands

When a multi-line string is provided via the YAML `command` property, each line
is sent to the shell individually. This means the first line is executed and a
new shell prompt is displayed, then the next line is executed, and so on. They
are not treated as a single compound command.

The markdown for this action is:

~~~
```terminal:execute
command: |-
  echo "First command"
  echo "Second command"
  echo "Third command"
```
~~~

Click the action below to test it. Notice that each echo command results in a
separate shell prompt being shown between commands:

```terminal:execute
command: |-
  echo "First command"
  echo "Second command"
  echo "Third command"
```

## Multi-line Command with Continuation

To have multiple lines treated as sequential commands against the same shell
prompt, use a semicolon and backslash continuation character at the end of all
but the last line. This causes the shell to treat them as a single compound
command.

The markdown for this action is:

~~~
```terminal:execute
command: |-
  echo "First command"; \
  echo "Second command"; \
  echo "Third command"
```
~~~

Click the action below to test it. Notice that all three echo commands run
under a single shell prompt:

```terminal:execute
command: |-
  echo "First command"; \
  echo "Second command"; \
  echo "Third command"
```

## Multi-line Content using Heredoc

A practical use of multi-line commands is creating file content using a shell
heredoc. This is a single command that naturally spans multiple lines, where the
heredoc body is sent as standard input to the command.

The markdown for this action is:

~~~
```terminal:execute
command: |-
  cat << 'EOF' > /tmp/sample.txt
  This is line 1 of the file.
  This is line 2 of the file.
  This is line 3 of the file.
  EOF
```
~~~

Click the action below to create the file, then verify its contents:

```terminal:execute
command: |-
  cat << 'EOF' > /tmp/sample.txt
  This is line 1 of the file.
  This is line 2 of the file.
  This is line 3 of the file.
  EOF
```

```terminal:execute
command: cat /tmp/sample.txt
```
