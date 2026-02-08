---
title: "Hidden Sections"
---

The `hidden` property can be used to hide a clickable action from view while
still allowing it to be triggered programmatically through `autostart` or
`cascade`. When used with `section:begin`, the collapsible section toggle is
not visible but the section content can still be revealed.

## Hidden Section Revealed by Cascade

A preceding action with `cascade: true` can trigger a hidden `section:begin`
to expand, revealing its content without showing the section toggle.

The markdown for this pattern is:

~~~text
```terminal:execute
command: echo "Running setup"
cascade: true
```

```section:begin
hidden: true
```

This content is only visible after the preceding command succeeds.

```section:end
```
~~~

Click the command below. After it succeeds, the hidden section content should
appear below it:

```terminal:execute
command: echo "Running setup - hidden content will appear below"
cascade: true
```

```section:begin
hidden: true
```

This content was hidden and has been revealed by the cascade from the command
above. The section toggle is not visible because `hidden: true` was set on
the `section:begin` action.

```section:end
```

## Hidden Section with Named Pairs

When using hidden sections alongside other sections, names should be used to
ensure correct matching:

```terminal:execute
command: echo "Revealing hidden named section"
cascade: true
```

```section:begin
name: hidden-named
hidden: true
```

This hidden named section was revealed by cascade. Using `name` ensures it is
correctly matched with its `section:end`.

```section:end
name: hidden-named
```

## Hidden Section Inside a Visible Section

A hidden section can be nested inside a visible section. The hidden section
content will only appear when triggered by cascade from an action within the
visible section.

```section:begin
name: visible-outer
title: Expand and Run Command
```

Click the command below. After it succeeds, additional content will appear
below it within this section.

```terminal:execute
command: echo "Revealing hidden content within section"
cascade: true
```

```section:begin
name: hidden-inner
hidden: true
```

This additional content was hidden and has been revealed by the cascade. It
is nested inside the visible outer section.

```section:end
name: hidden-inner
```

```section:end
name: visible-outer
```

## Hidden Section with Autostart

A hidden section with `autostart: true` will auto-expand when the page loads,
revealing its content without showing any section toggle. This can be used to
conditionally display content that is always visible.

~~~text
```section:begin
hidden: true
autostart: true
```

This content was auto-expanded on page load using a hidden section with
autostart.

```section:end
```
~~~

The text below should be visible immediately when this page loads, with no
section toggle visible:

```section:begin
hidden: true
autostart: true
```

This content was auto-expanded on page load using a hidden section with
`autostart: true`. There should be no visible section toggle above this text.

```section:end
```
