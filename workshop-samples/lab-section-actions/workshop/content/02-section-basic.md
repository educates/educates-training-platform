---
title: "Basic Collapsible Section"
---

A collapsible section is created using a pair of `section:begin` and
`section:end` action blocks. The content between them is initially hidden and
can be revealed by clicking on the `section:begin` action. Clicking on it
again will collapse the section.

## Simple Collapsible Section

The markdown for a basic collapsible section is:

~~~text
```section:begin
title: Click to Expand
```

This text is initially hidden and will be revealed when the section is
expanded.

```section:end
```
~~~

Click the action below to expand the section, then click again to collapse it:

```section:begin
title: Click to Expand
```

This text is initially hidden and will be revealed when the section is
expanded. If you can see this text, the section has been successfully expanded.

```section:end
```

## Section with Clickable Action Inside

Clickable actions can be placed inside a collapsible section. They will only
be visible when the section is expanded.

The markdown for this is:

~~~text
```section:begin
title: Run a Command
```

Expand this section and then click the action below to execute a command in
the terminal.

```terminal:execute
command: echo "Hello from inside a collapsible section"
```

```section:end
```
~~~

Click to expand the section and then run the command:

```section:begin
title: Run a Command
```

Expand this section and then click the action below to execute a command in
the terminal.

```terminal:execute
command: echo "Hello from inside a collapsible section"
```

```section:end
```

## Section with Multiple Content Elements

A section can contain multiple paragraphs, code blocks, and clickable actions:

~~~text
```section:begin
title: Multiple Elements
```

First paragraph of instructions.

Second paragraph of instructions.

```terminal:execute
command: echo "First command"
```

```terminal:execute
command: echo "Second command"
```

```section:end
```
~~~

Click to expand the section with multiple elements:

```section:begin
title: Multiple Elements
```

First paragraph of instructions.

Second paragraph of instructions.

```terminal:execute
command: echo "First command"
```

```terminal:execute
command: echo "Second command"
```

```section:end
```
