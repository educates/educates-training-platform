---
title: "Terminal Input"
---

The `terminal:input` action sends text to a terminal session using bracketed
paste mode. This is distinct from `terminal:execute` in that the text is pasted
into the terminal rather than being submitted as a command. Because bracketed
paste mode is used, even if a trailing newline is included in the text, the
terminal will not automatically act on it. The user would still need to press
Enter for the shell to process the pasted text.

The primary use case for `terminal:input` is when a running program is prompting
for interactive input such as a password, confirmation, or other data entry
rather than a shell command.

By default a newline is appended to the text (controlled by the `endl`
property). However, because of bracketed paste mode, this newline is included in
the pasted content but does not trigger command execution at a shell prompt. When
sending input to a program that is actively reading from stdin (such as `read`),
the newline will be consumed as the end-of-input marker by that program.

## Basic Input to a Waiting Prompt

To properly test `terminal:input`, we first need a program that is waiting for
input. The `read` shell builtin is ideal for this as it displays a prompt and
waits for the user to provide a value.

First, start a `read` command that will wait for input:

```terminal:execute
command: |-
  read -p "Enter your name: " NAME && echo "Hello, $NAME!"
```

Now use `terminal:input` to send text to the waiting prompt. Because `read` is
actively waiting for input, it will consume the text and the trailing newline,
completing the input.

The markdown for this action is:

~~~
```terminal:input
text: World
```
~~~

Click the action below to test it:

```terminal:input
text: World
```

You should see "Hello, World!" printed after the input is received.

## Input Without Newline

Setting `endl: false` sends the text without appending a newline. This means the
text appears at the prompt but the program continues to wait for more input
until Enter is pressed manually.

First, start another `read` command waiting for input:

```terminal:execute
command: |-
  read -p "Enter a color: " COLOR && echo "You chose: $COLOR"
```

The markdown for this action is:

~~~
```terminal:input
text: blue
endl: false
```
~~~

Click the action below to test it:

```terminal:input
text: blue
endl: false
```

After clicking the above, you should see "blue" appear at the prompt but the
`read` command will still be waiting. Press Enter manually to complete the input
and see the result.

## Input to Specific Session

The `session` property can be used to direct input to a specific terminal
session.

First, start a `read` command in terminal 2:

```terminal:execute
command: |-
  read -p "Enter a value: " VAL && echo "Got: $VAL"
session: 2
```

The markdown for this action is:

~~~
```terminal:input
text: hello from session 2
session: 2
```
~~~

Click the action below to send input to terminal 2:

```terminal:input
text: hello from session 2
session: 2
```

## Input at a Shell Prompt

When `terminal:input` is used against a shell prompt rather than a program
waiting for input, the text is pasted but not executed due to bracketed paste
mode. Even though a trailing newline is included by default, the shell will not
process the pasted text until Enter is pressed manually.

The markdown for this action is:

~~~
```terminal:input
text: echo "This was pasted, not executed"
```
~~~

Click the action below to test it:

```terminal:input
text: echo "This was pasted, not executed"
```

After clicking the above, you should see the text appear at the shell prompt
but it will not be executed. Press Enter manually to have the shell process the
command, or press Ctrl-C to discard it.
