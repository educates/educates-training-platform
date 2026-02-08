---
title: "Open URL"
---

The `dashboard:open-url` action opens a URL in a new browser window or tab. The
content of the code block is YAML with the URL specified via the `url` property.
Unlike other dashboard actions, this does not create or modify dashboard tabs
within the workshop environment.

## Basic Open URL

This test opens the Educates documentation in a new browser window.

The markdown for this action is:

~~~
```dashboard:open-url
url: https://docs.educates.dev
```
~~~

Click the action below to test it:

```dashboard:open-url
url: https://docs.educates.dev
```
