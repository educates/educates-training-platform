---
title: "Terminal Operations"
---

The `editor:open-terminal`, `editor:close-terminal`, `editor:send-to-terminal`,
`editor:interrupt-terminal`, and `editor:clear-terminal` clickable actions manage
terminals within the VS Code editor. These are distinct from the dashboard
`terminal:*` actions which manage terminals on the terminals tab.

If `session` is omitted it defaults to `"educates"`. The `session` value is
always treated as a string, so `session: 1` and `session: "1"` are equivalent.

## Open a Terminal with Default Session

The `editor:open-terminal` action opens or creates a terminal in the VS Code
editor. When `session` is omitted the default name `"educates"` is used.

The markdown for opening a terminal with the default session is:

~~~markdown
```editor:open-terminal
```
~~~

Click the action below to open the default terminal:

```editor:open-terminal
```

## Open a Terminal with a Numeric Session

Using a bare number for `session` tests that the value is correctly coerced to
a string. YAML will parse `session: 1` as an integer.

The markdown for opening a terminal with a numeric session is:

~~~markdown
```editor:open-terminal
session: 1
```
~~~

Click the action below to open terminal "1":

```editor:open-terminal
session: 1
```

## Open a Terminal with a Named Session

You can use any name for the `session` property.

The markdown for opening a named terminal is:

~~~markdown
```editor:open-terminal
session: build
```
~~~

Click the action below to open terminal "build":

```editor:open-terminal
session: build
```

## Send a Command to a Terminal

The `editor:send-to-terminal` action sends text to a terminal. By default a
newline is appended so the text is executed as a command. If the terminal does
not exist it will be created.

The markdown for sending a command is:

~~~markdown
```editor:send-to-terminal
text: echo "Hello from build terminal"
session: build
```
~~~

Click the action below to send a command to terminal "build":

```editor:send-to-terminal
text: echo "Hello from build terminal"
session: build
```

## Send a Command Using Numeric Session

This tests that a numeric `session` value is correctly handled.

Click the action below to send a command to terminal "1":

```editor:send-to-terminal
text: echo "Hello from terminal 1"
session: 1
```

## Send Text Without a Newline

Set `endl` to `false` to send text without appending a newline.

The markdown for sending without a newline is:

~~~markdown
```editor:send-to-terminal
text: partial input
session: build
endl: false
```
~~~

Click the action below to send text without a newline:

```editor:send-to-terminal
text: partial input
session: build
endl: false
```

## Interrupt a Terminal

The `editor:interrupt-terminal` action sends an interrupt signal (Ctrl+C) to a
terminal.

The markdown for interrupting a terminal is:

~~~markdown
```editor:interrupt-terminal
session: build
```
~~~

Click the action below to interrupt terminal "build":

```editor:interrupt-terminal
session: build
```

## Clear a Terminal

The `editor:clear-terminal` action clears the terminal buffer.

The markdown for clearing a terminal is:

~~~markdown
```editor:clear-terminal
session: build
```
~~~

Click the action below to clear terminal "build":

```editor:clear-terminal
session: build
```

## Close a Terminal

The `editor:close-terminal` action closes and disposes of a terminal.

The markdown for closing a terminal is:

~~~markdown
```editor:close-terminal
session: build
```
~~~

Click the action below to close terminal "build":

```editor:close-terminal
session: build
```

Click the action below to close terminal "1":

```editor:close-terminal
session: 1
```

Click the action below to close the default terminal:

```editor:close-terminal
```
