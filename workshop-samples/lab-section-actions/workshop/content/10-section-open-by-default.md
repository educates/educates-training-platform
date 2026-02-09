---
title: "Sections Open by Default"
---

A collapsible section is normally collapsed when the page loads and must be
clicked to expand it. By setting `open: true` on `section:begin`, the section
will be expanded by default. It can still be toggled shut by clicking on the
clickable action bar, just like a normal section.

## Basic Open Section

The markdown for a section that is open by default is:

~~~markdown
```section:begin
title: Already Expanded
open: true
```

This content is visible when the page loads because the section is open by
default. You can still click the section header to collapse it.

```section:end
```
~~~

The section below should be expanded when the page loads:

```section:begin
title: Already Expanded
open: true
```

This content is visible when the page loads because the section is open by
default. Click the section header above to collapse it, then click again to
re-expand it.

```section:end
```

## Open Section with Clickable Actions

An open section can contain clickable actions just like a normal section. The
actions are visible immediately without needing to expand the section first.

~~~markdown
```section:begin
title: Commands Ready to Run
open: true
```

These commands are visible immediately. Run them and then collapse the section
when you are done.

```terminal:execute
command: echo "First command in open section"
```

```terminal:execute
command: echo "Second command in open section"
```

```section:end
```
~~~

The section below should be expanded with commands ready to run:

```section:begin
title: Commands Ready to Run
open: true
```

These commands are visible immediately. Run them and then collapse the section
when you are done.

```terminal:execute
command: echo "First command in open section"
```

```terminal:execute
command: echo "Second command in open section"
```

```section:end
```

## Swapping Content with Hidden Open Sections

The `open: true` property becomes particularly useful when combined with
`hidden: true`. A hidden section has no visible clickable action bar, so
normally it can only be opened via `autostart` or `cascade`. By also setting
`open: true`, the section content is displayed immediately without any
visible toggle, and without needing `autostart`.

Where this gets interesting is when you combine it with `cascade` to swap
displayed content after an action is run. In the pattern below, clicking the
command triggers a cascade chain that:

1. Closes the first hidden open section (hiding the initial content).
2. Opens the second hidden section (revealing the replacement content).

This effectively swaps one block of text for another when the command is
clicked.

The markdown for this pattern is:

~~~markdown
```terminal:execute
command: ls -las
cascade: true
pause: 0
```

```section:begin
title: First
hidden: true
open: true
cascade: true
pause: 0
cooldown: -1
```

This is the **initial content** that is visible before the command is run.
It will be replaced when you click the command above.

```section:end
toggle: false
cascade: true
```

```section:begin
title: Second
hidden: true
cooldown: -1
```

This is the **replacement content** that appears after the command has been
run. The initial content above has been swapped out.

```section:end
```
~~~

Here is how it works step by step:

* The first `section:begin` has `hidden: true` and `open: true`, so its
  content is displayed immediately with no visible toggle.
* The command has `cascade: true`, so after it runs it triggers the first
  `section:begin`. Because the section is already open and has `cascade: true`,
  it cascades through to `section:end`.
* The `section:end` has `toggle: false` so the section is not closed, but it
  also has `cascade: true`, so the cascade continues to the second
  `section:begin`, which opens it.
* The first section has `cooldown: -1` meaning that once triggered via
  cascade, it stays in its triggered state permanently. The second section
  also uses `cooldown: -1` so once opened it stays open.
* The `pause: 0` settings ensure the cascade chain runs without visible delay.

Try it below. Click the command and watch the text swap:

```terminal:execute
command: ls -las
cascade: true
pause: 0
```

```section:begin
title: First
hidden: true
open: true
cascade: true
pause: 0
cooldown: -1
```

This is the **initial content** that is visible before the command is run.
It will be replaced when you click the command above.

```section:end
toggle: false
cascade: true
```

```section:begin
title: Second
hidden: true
cooldown: -1
```

This is the **replacement content** that appears after the command has been
run. The initial content above has been swapped out.

```section:end
```

## Multi-Step Content Swap

You can extend this pattern to swap through multiple stages of content. Each
step replaces the previous content with the next. Rather than having a
separate clickable action before the first section, the command can be placed
inside each section. When the command is clicked, it cascades to
`section:end`, which closes the current section and then cascades to the next
`section:begin`, opening it.

~~~markdown
```section:begin
name: step-1
hidden: true
open: true
```

**Step 1 instructions:** You are seeing the initial instructions. Click the
command below to proceed to step 2.

```terminal:execute
command: echo "Step 1 complete"
cascade: true
pause: 0
```

```section:end
name: step-1
cascade: true
```

```section:begin
name: step-2
hidden: true
cooldown: -1
```

**Step 2 instructions:** The initial instructions have been replaced. Click
the command below to proceed to the final step.

```terminal:execute
command: echo "Step 2 complete"
cascade: true
pause: 0
```

```section:end
name: step-2
cascade: true
```

```section:begin
name: step-3
hidden: true
cooldown: -1
```

**Final step:** All previous instructions have been replaced with this final
message. The workflow is complete.

```section:end
name: step-3
```
~~~

In this pattern:

* The first section has `hidden: true` and `open: true`, so its content
  (including the command) is displayed immediately with no visible toggle.
* Each command has `cascade: true`, so after running it cascades to its
  `section:end`, which closes the section and hides the content.
* Each `section:end` also has `cascade: true`, so the cascade continues to the
  next `section:begin`, opening it and revealing the next step.
* The subsequent sections have `cooldown: -1` so once opened they stay open
  until their own command triggers the next swap.
* The final section has no cascade on its `section:end`, so the chain stops.

Try the multi-step swap below. Click the first command, then the second
command that appears, and watch the content change at each step:

```section:begin
name: step-1
hidden: true
open: true
```

**Step 1 instructions:** You are seeing the initial instructions. Click the
command below to proceed to step 2.

```terminal:execute
command: echo "Step 1 complete"
cascade: true
pause: 0
```

```section:end
name: step-1
cascade: true
```

```section:begin
name: step-2
hidden: true
cooldown: -1
```

**Step 2 instructions:** The initial instructions have been replaced. Click
the command below to proceed to the final step.

```terminal:execute
command: echo "Step 2 complete"
cascade: true
pause: 0
```

```section:end
name: step-2
cascade: true
```

```section:begin
name: step-3
hidden: true
cooldown: -1
```

**Final step:** All previous instructions have been replaced with this final
message. The workflow is complete.

```section:end
name: step-3
```
