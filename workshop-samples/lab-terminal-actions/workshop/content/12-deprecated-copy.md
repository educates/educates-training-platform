---
title: "Deprecated: Copy Actions"
---

The `copy` and `copy-and-edit` code block annotations are the original legacy
formats for copying text to the system paste buffer. They have been superseded
by `workshop:copy` and `workshop:copy-and-edit` which use YAML-based
configuration. The legacy formats are still supported for backward compatibility
but should not be used in new workshops.

With the deprecated format, the body of the code block is the text to copy
itself (not YAML).

## Deprecated Copy

The markdown for this action is:

~~~
```copy
echo "Hello from deprecated copy"
```
~~~

Click the action below to copy the text to the paste buffer:

```copy
echo "Hello from deprecated copy"
```

## Deprecated Copy and Edit

The markdown for this action is:

~~~
```copy-and-edit
export MY_VARIABLE=<insert-value-here>
```
~~~

Click the action below to copy the text to the paste buffer:

```copy-and-edit
export MY_VARIABLE=<insert-value-here>
```
