---
title: "Nested Sections"
---

Collapsible sections can be nested inside one another. When nesting sections,
each pair of `section:begin` and `section:end` must be given a unique `name`
so they can be correctly matched.

## Two Levels of Nesting

The markdown for nested sections is:

~~~markdown
```section:begin
name: outer
title: Outer Section
```

Content before the inner section.

```section:begin
name: inner
title: Inner Section
```

Content of the inner section.

```section:end
name: inner
```

Content after the inner section.

```section:end
name: outer
```
~~~

Click the outer section to expand it. Inside you will find an inner section
that can also be expanded. Collapsing the outer section should also collapse
any expanded inner sections.

```section:begin
name: outer
title: Outer Section
```

This is content inside the outer section but before the inner section.

```section:begin
name: inner
title: Inner Section
```

This is content inside the inner section. If you can see this, both the outer
and inner sections have been expanded.

```terminal:execute
command: echo "Command inside the inner section"
```

```section:end
name: inner
```

This is content inside the outer section but after the inner section.

```section:end
name: outer
```

## Three Levels of Nesting

Sections can be nested to multiple levels:

```section:begin
name: level-1
title: Level 1
```

Content at level 1.

```section:begin
name: level-2
title: Level 2
```

Content at level 2.

```section:begin
name: level-3
title: Level 3
```

Content at level 3. This is the deepest level.

```terminal:execute
command: echo "Command at level 3"
```

```section:end
name: level-3
```

```section:end
name: level-2
```

```section:end
name: level-1
```

## Multiple Inner Sections

An outer section can contain multiple sibling inner sections:

```section:begin
name: container
title: Container Section
```

This outer section contains two inner sections.

```section:begin
name: inner-a
title: Inner Section A
```

Content of inner section A.

```section:end
name: inner-a
```

```section:begin
name: inner-b
title: Inner Section B
```

Content of inner section B.

```section:end
name: inner-b
```

```section:end
name: container
```
