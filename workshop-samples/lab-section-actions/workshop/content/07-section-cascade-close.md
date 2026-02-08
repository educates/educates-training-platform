---
title: "Cascade to Close Sections"
---

The `cascade` property on a clickable action causes the next clickable action
to be automatically triggered when the current action succeeds. When the last
clickable action inside a collapsible section has `cascade: true`, the next
action is the `section:end` block. Triggering `section:end` causes the section
to collapse, effectively auto-closing the section after the action completes.

## Basic Cascade to Close

The markdown for a section that auto-closes after executing a command is:

~~~text
```section:begin
name: cascade-basic
title: Run Command and Auto-Close
```

Click the command below. After it succeeds, the section will automatically
collapse.

```terminal:execute
command: echo "Running command, section will close after this"
cascade: true
```

```section:end
name: cascade-basic
```
~~~

Expand the section below and click the command. After the command succeeds,
the section should automatically collapse:

```section:begin
name: cascade-basic
title: Run Command and Auto-Close
```

Click the command below. After it succeeds, the section will automatically
collapse. You can expand the section again afterwards to verify it still
works.

```terminal:execute
command: echo "Running command, section will close after this"
cascade: true
```

```section:end
name: cascade-basic
```

## Cascade Close After Multiple Actions

When multiple actions are chained with `cascade: true`, they execute in
sequence, with the final one triggering `section:end` to close the section:

~~~text
```section:begin
name: cascade-chain
title: Run Two Commands Then Auto-Close
```

Click the first command. It will cascade to the second command, which will
then cascade to close the section.

```terminal:execute
command: echo "First command"
cascade: true
```

```terminal:execute
command: echo "Second command - section closes after this"
cascade: true
```

```section:end
name: cascade-chain
```
~~~

Expand the section and click the first command to start the chain:

```section:begin
name: cascade-chain
title: Run Two Commands Then Auto-Close
```

Click the first command. It will cascade to the second command, which will
then cascade to close the section.

```terminal:execute
command: echo "First command"
cascade: true
```

```terminal:execute
command: echo "Second command - section closes after this"
cascade: true
```

```section:end
name: cascade-chain
```

## Autostart with Cascade to Close

Combining `autostart` and `cascade` allows a section to automatically run a
command and then close itself when expanded. This can be useful for one-time
setup actions.

~~~text
```section:begin
name: autostart-cascade
title: Auto-Run and Auto-Close
```

This command runs automatically when the section is expanded and then the
section closes itself.

```terminal:execute
command: echo "Auto-running and auto-closing"
autostart: true
cascade: true
```

```section:end
name: autostart-cascade
```
~~~

Expand the section below. The command should run automatically and then the
section should collapse. You can expand it again to re-trigger:

```section:begin
name: autostart-cascade
title: Auto-Run and Auto-Close
```

This command runs automatically when the section is expanded and then the
section closes itself.

```terminal:execute
command: echo "Auto-running and auto-closing"
autostart: true
cascade: true
```

```section:end
name: autostart-cascade
```

## Cascade Close in Nested Sections

When cascade triggers `section:end` in a nested section, only the inner
section should collapse. The outer section should remain expanded.

```section:begin
name: cascade-outer
title: Outer Section
```

This is the outer section. Expand the inner section and click the command.
The inner section should collapse but this outer section should remain open.

```section:begin
name: cascade-inner
title: Inner Section (will auto-close after command)
```

Click the command below. After it succeeds, this inner section will collapse
but the outer section will remain expanded.

```terminal:execute
command: echo "Inner command - inner section closes after this"
cascade: true
```

```section:end
name: cascade-inner
```

This text in the outer section should remain visible after the inner section
collapses.

```section:end
name: cascade-outer
```

## Re-expanding After Cascade Close

After a section is closed by cascade, it should be possible to manually
re-expand it by clicking on the `section:begin` action again:

```section:begin
name: cascade-reopen
title: Close and Reopen Test
```

Click the command to cascade-close this section. Then click the section header
again to re-expand it. The command can be run again.

```terminal:execute
command: echo "Section will close, but you can reopen it"
cascade: true
```

```section:end
name: cascade-reopen
```
