---
title: "Section Heading"
---

The `section:heading` action displays a heading styled as a clickable action
block. Clicking on it marks the action as completed but does not trigger any
other behavior. It can be used to visually separate parts of the instructions
in the same style as other clickable actions.

## Basic Heading

The markdown for a basic section heading with default prefix is:

~~~markdown
```section:heading
title: Instructions
```
~~~

Click the action below to mark it as completed:

```section:heading
title: Instructions
```

## Heading with Custom Prefix

The `prefix` property can be used to override the default "Section" prefix:

~~~markdown
```section:heading
prefix: Task
title: Deploy the Application
```
~~~

Click the action below to mark it as completed:

```section:heading
prefix: Task
title: Deploy the Application
```

## Heading with Description

A `description` can be provided to display text in the body of the action:

~~~markdown
```section:heading
title: Prerequisites
description: Ensure you have completed the previous steps before continuing.
```
~~~

Click the action below to mark it as completed:

```section:heading
title: Prerequisites
description: Ensure you have completed the previous steps before continuing.
```
