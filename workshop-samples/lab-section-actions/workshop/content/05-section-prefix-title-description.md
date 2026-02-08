---
title: "Prefix, Title, and Description"
---

The `section:begin` and `section:heading` actions support `prefix`, `title`,
and `description` properties to customize how they are displayed.

## Custom Prefix

The default prefix is "Section". It can be overridden using the `prefix`
property. The banner displays as "Prefix: Title".

The markdown for a section with custom prefix is:

~~~text
```section:begin
name: question-prefix
prefix: Question
title: "1"
```

What is the default port for HTTPS?

```section:end
name: question-prefix
```
~~~

Click to expand:

```section:begin
name: question-prefix
prefix: Question
title: "1"
```

What is the default port for HTTPS? The answer is 443.

```section:end
name: question-prefix
```

## Custom Title

The `title` property sets the text displayed after the prefix:

~~~text
```section:begin
name: custom-title
title: Optional Advanced Configuration
```

These steps are only needed for advanced deployments.

```section:end
name: custom-title
```
~~~

Click to expand:

```section:begin
name: custom-title
title: Optional Advanced Configuration
```

These steps are only needed for advanced deployments. If you are following the
basic tutorial, you can skip this section.

```section:end
name: custom-title
```

## Custom Description

The `description` property sets the text displayed in the body of the action
block instead of it being empty:

~~~text
```section:begin
name: with-desc
title: Setup Steps
description: Click to reveal the setup instructions.
```

Run the following command to set up the environment.

```terminal:execute
command: echo "Setting up environment..."
```

```section:end
name: with-desc
```
~~~

Click to expand:

```section:begin
name: with-desc
title: Setup Steps
description: Click to reveal the setup instructions.
```

Run the following command to set up the environment.

```terminal:execute
command: echo "Setting up environment..."
```

```section:end
name: with-desc
```

## Combined Prefix, Title, and Description

All three properties can be used together:

~~~text
```section:begin
name: combined
prefix: Task
title: Deploy Application
description: Follow these steps to deploy the sample application.
```

Deploy the application by running:

```terminal:execute
command: echo "Deploying application..."
```

```section:end
name: combined
```
~~~

Click to expand:

```section:begin
name: combined
prefix: Task
title: Deploy Application
description: Follow these steps to deploy the sample application.
```

Deploy the application by running:

```terminal:execute
command: echo "Deploying application..."
```

```section:end
name: combined
```
