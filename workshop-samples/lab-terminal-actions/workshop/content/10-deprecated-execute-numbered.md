---
title: "Deprecated: Execute with Terminal Numbers"
---

When the workshop dashboard is configured to display multiple terminals, the
original legacy format allowed targeting a specific terminal by appending a
number suffix to the `execute` annotation: `execute-1`, `execute-2`, and
`execute-3`. These have been superseded by `terminal:execute` with the `session`
property.

## Execute-1 (Terminal 1)

The markdown for this action is:

~~~markdown
```execute-1
echo "Hello from deprecated execute-1"
```
~~~

Click the action below to test it:

```execute-1
echo "Hello from deprecated execute-1"
```

## Execute-2 (Terminal 2)

The markdown for this action is:

~~~markdown
```execute-2
echo "Hello from deprecated execute-2"
```
~~~

Click the action below to test it:

```execute-2
echo "Hello from deprecated execute-2"
```

## Execute-3 (Terminal 3)

The markdown for this action is:

~~~markdown
```execute-3
echo "Hello from deprecated execute-3"
```
~~~

Click the action below to test it:

```execute-3
echo "Hello from deprecated execute-3"
```

The preferred replacement for all of the above is `terminal:execute` with the
`session` property set to the desired terminal number.
