---
title: "Prefix, Title, and Description"
---

All editor clickable actions support the `prefix`, `title`, and `description`
properties to customize how the action is displayed. The banner is shown as
"Prefix: Title" and the description appears in the body of the action block as
preformatted text.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/custom-display.txt
text: |
  Hello World
```

## Custom Prefix

The default prefix for editor actions is "Editor". The `prefix` property
overrides this.

The markdown for a custom prefix is:

~~~markdown
```editor:open-file
file: ~/exercises/custom-display.txt
prefix: Step 1
title: Open the configuration file
```
~~~

The action below should display as "Step 1: Open the configuration file":

```editor:open-file
file: ~/exercises/custom-display.txt
prefix: Step 1
title: Open the configuration file
```

## Custom Title

The `title` property overrides the default title generated from the action
parameters.

The markdown for a custom title is:

~~~markdown
```editor:append-lines-to-file
file: ~/exercises/custom-display.txt
title: Add greeting message
text: |
    Greetings from Educates!
```
~~~

The action below should display as "Editor: Add greeting message":

```editor:append-lines-to-file
file: ~/exercises/custom-display.txt
title: Add greeting message
text: |
    Greetings from Educates!
```

## Custom Description

The `description` property provides body text displayed inside the action block.
This replaces the default body content. Note that descriptions are always
rendered as preformatted text.

The markdown for a custom description is:

~~~markdown
```editor:create-file
file: ~/exercises/custom-display.txt
title: Reset the file
description: Replaces the file content with the original text.
text: |
    Hello World
```
~~~

The action below should display the description in the body:

```editor:create-file
file: ~/exercises/custom-display.txt
title: Reset the file
description: Replaces the file content with the original text.
text: |
    Hello World
```

## Combined Prefix, Title, and Description

All three properties can be used together on any editor action:

~~~markdown
```editor:append-lines-to-file
file: ~/exercises/custom-display.txt
prefix: Task
title: Append Footer
description: Adds a footer line to the end of the file.
text: |
    --- End of File ---
```
~~~

The action below should display as "Task: Append Footer" with the description
in the body:

```editor:append-lines-to-file
file: ~/exercises/custom-display.txt
prefix: Task
title: Append Footer
description: Adds a footer line to the end of the file.
text: |
    --- End of File ---
```
