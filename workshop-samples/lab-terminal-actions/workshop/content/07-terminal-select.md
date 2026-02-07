---
title: "Terminal Select"
---

The `terminal:select` action switches focus to a specific terminal session
without executing any commands. If the terminal is on a separate dashboard tab,
that tab will be brought to the foreground.

## Select Terminal 1

The markdown for this action is:

~~~
```terminal:select
session: 1
```
~~~

Click the action below to select terminal 1:

```terminal:select
session: 1
```

## Select Terminal 2

The markdown for this action is:

~~~
```terminal:select
session: 2
```
~~~

Click the action below to select terminal 2:

```terminal:select
session: 2
```

## Select Terminal 3

The markdown for this action is:

~~~
```terminal:select
session: 3
```
~~~

Click the action below to select terminal 3:

```terminal:select
session: 3
```

## Select Terminal 4

Terminal 4 is exposed as a separate dashboard tab rather than being part of the
main terminal split layout. Using `terminal:select` with `session: 4` should
switch to the Terminal#4 dashboard tab.

The markdown for this action is:

~~~
```terminal:select
session: 4
```
~~~

Click the action below to select terminal 4:

```terminal:select
session: 4
```
