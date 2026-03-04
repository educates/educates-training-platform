---
title: "Open Dashboard"
---

The `dashboard:open-dashboard` action switches focus to an existing dashboard
tab. The content of the code block is YAML with the dashboard specified via the
`name` property.

## Basic Open Dashboard

This test opens the pre-configured "Example" dashboard tab.

The markdown for this action is:

~~~markdown
```dashboard:open-dashboard
name: Example
```
~~~

Click the action below to test it:

```dashboard:open-dashboard
name: Example
```

## Open Terminal Dashboard

This test opens the built-in "Terminal" dashboard tab containing the terminal
sessions.

The markdown for this action is:

~~~markdown
```dashboard:open-dashboard
name: Terminal
```
~~~

Click the action below to test it:

```dashboard:open-dashboard
name: Terminal
```
