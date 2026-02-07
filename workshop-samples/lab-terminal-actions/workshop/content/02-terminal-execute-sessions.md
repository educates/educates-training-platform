---
title: "Terminal Execute with Session Targeting"
---

The `terminal:execute` action accepts a `session` property to direct the command
to a specific terminal session. The workshop is configured with a `split/2`
layout providing three terminal sessions in the Terminal tab, plus a fourth
terminal accessible via the "Terminal#4" dashboard tab.

## Execute in Terminal 1

The markdown for this action is:

~~~
```terminal:execute
command: echo "Executed in terminal 1"
session: 1
```
~~~

Click the action below to test it:

```terminal:execute
command: echo "Executed in terminal 1"
session: 1
```

## Execute in Terminal 2

The markdown for this action is:

~~~
```terminal:execute
command: echo "Executed in terminal 2"
session: 2
```
~~~

Click the action below to test it:

```terminal:execute
command: echo "Executed in terminal 2"
session: 2
```

## Execute in Terminal 3

Terminal 3 is the third terminal in the `split/2` layout.

The markdown for this action is:

~~~
```terminal:execute
command: echo "Executed in terminal 3"
session: 3
```
~~~

Click the action below to test it:

```terminal:execute
command: echo "Executed in terminal 3"
session: 3
```

## Execute in Terminal 4

Terminal 4 is not part of the `split/2` layout in the Terminal tab. It is
exposed as a separate dashboard tab named "Terminal#4" configured with `url`
set to `terminal:4`. Because the portion after the `terminal:` prefix in the
URL is `4`, the session name used to target it is also `4`.

The markdown for this action is:

~~~
```terminal:execute
command: echo "Executed in terminal 4"
session: 4
```
~~~

Click the action below to test it. You should see the output appear in the
"Terminal#4" dashboard tab:

```terminal:execute
command: echo "Executed in terminal 4"
session: 4
```

After clicking any of the above, the targeted terminal session will remain
selected so that any subsequent manual input is directed to that terminal.
