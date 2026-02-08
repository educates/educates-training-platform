---
title: "Create Dashboard"
---

The `dashboard:create-dashboard` action creates a new dashboard tab. The content
of the code block is YAML with the dashboard name specified via the `name`
property and the URL to display via the `url` property.

## Create Dashboard with URL

This test creates a new dashboard tab named "Docs" pointing to the Educates
documentation site.

The markdown for this action is:

~~~
```dashboard:create-dashboard
name: Docs
url: https://docs.educates.dev
```
~~~

Click the action below to test it:

```dashboard:create-dashboard
name: Docs
url: https://docs.educates.dev
```

## Create Dashboard with Terminal Session

A dashboard tab can host a new interactive terminal session by using a URL of
the form `terminal:<session>`. The session name should use lower case letters,
numbers and hyphens. Avoid numeric session names like "1", "2" or "3" as these
are used for the default terminal sessions.

The markdown for this action is:

~~~
```dashboard:create-dashboard
name: Extra Terminal
url: terminal:4
```
~~~

Click the action below to test it:

```dashboard:create-dashboard
name: Extra Terminal
url: terminal:4
```

## Create Dashboard without Focus

The `focus` property can be set to `false` to create the dashboard tab without
switching to it. The tab is created in the background.

The markdown for this action is:

~~~
```dashboard:create-dashboard
name: Background
url: https://www.example.com
focus: false
```
~~~

Click the action below to test it. Note that the new tab is created but focus
remains on the current view:

```dashboard:create-dashboard
name: Background
url: https://www.example.com
focus: false
```
