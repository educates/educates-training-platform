---
title: "Reload Dashboard"
---

The `dashboard:reload-dashboard` action reloads an existing dashboard tab. The
content of the code block is YAML with the dashboard specified via the `name`
property. Optionally a new `url` can be provided to change the target URL.

## Basic Reload

This test reloads the pre-configured "Example" dashboard using its current URL.

The markdown for this action is:

~~~markdown
```dashboard:reload-dashboard
name: Example
```
~~~

Click the action below to test it:

```dashboard:reload-dashboard
name: Example
```

## Reload with New URL

The `url` property allows changing the URL displayed in a dashboard tab when
reloading. If the specified dashboard does not already exist, it will be created.
This makes it a safe alternative to `dashboard:create-dashboard` when you are
not sure whether the dashboard already exists.

The markdown for this action is:

~~~markdown
```dashboard:reload-dashboard
name: Example
url: https://docs.educates.dev
```
~~~

Click the action below to test it. The "Example" dashboard will now show the
Educates documentation:

```dashboard:reload-dashboard
name: Example
url: https://docs.educates.dev
```

Now reload it back to the original URL:

```dashboard:reload-dashboard
name: Example
url: https://www.example.com
```

## Reload without Focus

The `focus` property can be set to `false` to reload the dashboard without
switching to it.

The markdown for this action is:

~~~markdown
```dashboard:reload-dashboard
name: Example
url: https://docs.educates.dev
focus: false
```
~~~

Click the action below to test it. The "Example" dashboard will be reloaded in
the background without switching focus:

```dashboard:reload-dashboard
name: Example
url: https://docs.educates.dev
focus: false
```

Reset it back to the original URL for subsequent tests:

```dashboard:reload-dashboard
name: Example
url: https://www.example.com
```
