---
title: "Cascade Fallthrough"
---

When cascade is set on a clickable action before `section:end`, triggering the
`section:end` will close the section. If cascade is also set on `section:end`
itself, the cascade will continue to the next clickable action after the
`section:end`, triggering it as well. This is referred to as cascade
fallthrough.

## Cascade Close with Fallthrough

In this example, the command inside the section has `cascade: true`, which
triggers `section:end` to close the section. The `section:end` also has
`cascade: true`, so after closing the section it triggers the next clickable
action outside the section.

The markdown for this pattern is:

~~~markdown
```section:begin
name: fallthrough-close
title: Run Command with Fallthrough
```

Click the command below. After it succeeds, the section will close and the
command after the section will also be triggered.

```terminal:execute
command: echo "Running inside section"
cascade: true
```

```section:end
name: fallthrough-close
cascade: true
```

```terminal:execute
command: echo "Triggered by fallthrough from section:end"
```
~~~

Expand the section and click the command. After the command succeeds, the
section should close and the command after the section should also execute:

```section:begin
name: fallthrough-close
title: Run Command with Fallthrough
```

Click the command below. After it succeeds, the section will close and the
command after the section will also be triggered.

```terminal:execute
command: echo "Running inside section"
cascade: true
```

```section:end
name: fallthrough-close
cascade: true
```

```terminal:execute
command: echo "Triggered by fallthrough from section:end"
```

## Fallthrough Without Closing the Section

In some cases you may want the cascade to fall through to actions after the
section without closing the section itself. Setting `toggle: false` on
`section:end` prevents the section from being collapsed, while still allowing
cascade to continue to the next action.

The markdown for this pattern is:

~~~markdown
```section:begin
name: fallthrough-no-close
title: Fallthrough Without Closing
```

Click the command below. After it succeeds, the section will remain open but
the command after the section will be triggered.

```terminal:execute
command: echo "Running inside section"
cascade: true
```

```section:end
name: fallthrough-no-close
cascade: true
toggle: false
```

```terminal:execute
command: echo "Triggered by fallthrough, but section stayed open"
```
~~~

Expand the section and click the command. After the command succeeds, the
section should remain open and the command after the section should also
execute:

```section:begin
name: fallthrough-no-close
title: Fallthrough Without Closing
```

Click the command below. After it succeeds, the section will remain open but
the command after the section will be triggered.

```terminal:execute
command: echo "Running inside section"
cascade: true
```

```section:end
name: fallthrough-no-close
cascade: true
toggle: false
```

```terminal:execute
command: echo "Triggered by fallthrough, but section stayed open"
```
