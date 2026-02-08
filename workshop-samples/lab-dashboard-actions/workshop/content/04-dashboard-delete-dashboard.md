---
title: "Delete Dashboard"
---

The `dashboard:delete-dashboard` action deletes a dashboard tab. The content of
the code block is YAML with the dashboard specified via the `name` property.

Note that built-in dashboards corresponding to applications provided by the
workshop environment (such as the default Terminal, Console, Editor or Slides)
cannot be deleted. Only custom dashboards created via the workshop configuration
or the `dashboard:create-dashboard` action can be deleted.

## Create then Delete a Dashboard

First, create a temporary dashboard to demonstrate deletion.

```dashboard:create-dashboard
name: Temporary
url: https://www.example.com
```

Now delete it. The markdown for the delete action is:

~~~markdown
```dashboard:delete-dashboard
name: Temporary
```
~~~

Click the action below to test it:

```dashboard:delete-dashboard
name: Temporary
```

## Delete with Terminal Session

Deleting a custom dashboard that includes a terminal session does not destroy the
underlying terminal session. The terminal session can be reconnected by creating
a new dashboard for the same session name.

First, create a dashboard with a terminal session:

```dashboard:create-dashboard
name: Temp Terminal
url: terminal:temp
```

Run a command in the terminal session so we can verify it persists after
reconnection:

```terminal:execute
command: echo "This terminal session will persist"
session: temp
```

Now delete the dashboard:

~~~markdown
```dashboard:delete-dashboard
name: Temp Terminal
```
~~~

Click the action below to test it:

```dashboard:delete-dashboard
name: Temp Terminal
```

Recreate the dashboard for the same terminal session to verify the session was
not destroyed:

```dashboard:create-dashboard
name: Temp Terminal
url: terminal:temp
```

The previous command output should still be visible in the reconnected terminal
session. Clean up by deleting it again:

```dashboard:delete-dashboard
name: Temp Terminal
```
