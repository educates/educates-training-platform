---
title: "Autostart Within Sections"
---

When a clickable action inside a collapsible section has `autostart: true`, it
should not be triggered when the page loads. Instead, it should only be
triggered when the section is expanded. This page tests autostart behavior at
various nesting levels.

## Autostart in a Single Section

The markdown for a section containing an autostart action is:

~~~text
```section:begin
name: autostart-basic
title: Expand to Auto-Run Command
```

The command below will execute automatically when this section is expanded.

```terminal:execute
command: echo "This command ran automatically when the section was expanded"
autostart: true
```

```section:end
name: autostart-basic
```
~~~

The terminal should NOT show the echo output until you expand the section
below. Click to expand and verify the command runs automatically:

```section:begin
name: autostart-basic
title: Expand to Auto-Run Command
```

The command below should have executed automatically when this section was
expanded. Check the terminal to verify the output appeared.

```terminal:execute
command: echo "This command ran automatically when the section was expanded"
autostart: true
```

```section:end
name: autostart-basic
```

## Autostart with Other Actions

A section can contain both autostart and manual actions. Only the autostart
action should trigger automatically on expand:

```section:begin
name: autostart-mixed
title: Mixed Autostart and Manual Actions
```

The first command below should have run automatically. The second command
requires a manual click.

```terminal:execute
command: |-
  echo "AUTO: This ran automatically on section expand"
autostart: true
```

```terminal:execute
command: |-
  echo "MANUAL: This requires a click to run"
```

```section:end
name: autostart-mixed
```

## Autostart in Nested Sections

When sections are nested, autostart actions should only trigger when their
immediate containing section is expanded.

The outer section contains an autostart action and an inner section. The inner
section also contains an autostart action. Expanding the outer section should
trigger only the outer autostart action. Expanding the inner section should
trigger the inner autostart action.

```section:begin
name: autostart-outer
title: Outer Section (expand to auto-run outer command)
```

The command below should have run automatically when this outer section was
expanded.

```terminal:execute
command: |-
  echo "AUTO-OUTER: This ran when the outer section was expanded"
autostart: true
```

Now expand the inner section to trigger its autostart action:

```section:begin
name: autostart-inner
title: Inner Section (expand to auto-run inner command)
```

The command below should have run automatically when this inner section was
expanded.

```terminal:execute
command: |-
  echo "AUTO-INNER: This ran when the inner section was expanded"
autostart: true
```

```section:end
name: autostart-inner
```

```section:end
name: autostart-outer
```

## Multiple Autostart Actions in One Section

When a section contains multiple autostart actions, all of them should trigger
when the section is expanded:

```section:begin
name: autostart-multiple
title: Expand to Auto-Run Multiple Commands
```

Both commands below should have run automatically when this section was
expanded.

```terminal:execute
command: |-
  echo "AUTO-1: First autostart command"
autostart: true
```

```terminal:execute
command: |-
  echo "AUTO-2: Second autostart command"
autostart: true
```

```section:end
name: autostart-multiple
```
