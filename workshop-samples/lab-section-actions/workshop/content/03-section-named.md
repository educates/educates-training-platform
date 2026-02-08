---
title: "Named Sections"
---

When multiple collapsible sections exist on the same page, each pair of
`section:begin` and `section:end` blocks should be given a unique `name` so
they can be correctly matched. Without a name, all unnamed sections default to
the name `*` which can cause incorrect matching.

## Two Independent Named Sections

The markdown for two independent named sections is:

~~~markdown
```section:begin
name: first
title: First Section
```

Content of the first section.

```section:end
name: first
```

```section:begin
name: second
title: Second Section
```

Content of the second section.

```section:end
name: second
```
~~~

Each section below can be expanded and collapsed independently:

```section:begin
name: first
title: First Section
```

This is the content of the first section. Expanding or collapsing this section
should not affect the second section.

```terminal:execute
command: echo "Command in first section"
```

```section:end
name: first
```

```section:begin
name: second
title: Second Section
```

This is the content of the second section. Expanding or collapsing this section
should not affect the first section.

```terminal:execute
command: echo "Command in second section"
```

```section:end
name: second
```

## Three Named Sections

A page can have any number of independent named sections. Here are three:

```section:begin
name: alpha
title: Alpha
```

Content of the alpha section.

```section:end
name: alpha
```

```section:begin
name: beta
title: Beta
```

Content of the beta section.

```section:end
name: beta
```

```section:begin
name: gamma
title: Gamma
```

Content of the gamma section.

```section:end
name: gamma
```
