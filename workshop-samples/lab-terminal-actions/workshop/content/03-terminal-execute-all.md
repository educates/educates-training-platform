---
title: "Terminal Execute All"
---

The `terminal:execute-all` action executes a command simultaneously in all
terminal sessions on the Terminal tab of the dashboard. After execution, the
first terminal session is left selected.

## Basic Execute All

The markdown for this action is:

~~~
```terminal:execute-all
command: echo "Executed in all terminals"
```
~~~

Click the action below to test it:

```terminal:execute-all
command: echo "Executed in all terminals"
```

## Execute All with Clear

The `clear` property works with `terminal:execute-all` as well, clearing the
full buffer of every terminal session before executing the command.

The markdown for this action is:

~~~
```terminal:execute-all
command: echo "All terminals cleared then this ran"
clear: true
```
~~~

Click the action below to test it:

```terminal:execute-all
command: echo "All terminals cleared then this ran"
clear: true
```

## Execute All Using Session Wildcard

An alternative to `terminal:execute-all` is to use `terminal:execute` with `session`
set to `"*"`. This targets all terminal sessions in the same way.

The markdown for this action is:

~~~
```terminal:execute
command: echo "Executed in all terminals using wildcard"
session: "*"
```
~~~

Click the action below to test it:

```terminal:execute
command: echo "Executed in all terminals using wildcard"
session: "*"
```

## Execute All with Clear Using Session Wildcard

The `clear` property also works when using the session wildcard approach.

The markdown for this action is:

~~~
```terminal:execute
command: echo "All terminals cleared then this ran using wildcard"
session: "*"
clear: true
```
~~~

Click the action below to test it:

```terminal:execute
command: echo "All terminals cleared then this ran using wildcard"
session: "*"
clear: true
```
