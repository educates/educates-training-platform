---
title: "Insert and Append Around Selection"
---

The `editor:insert-lines-before-selection` and `editor:append-lines-after-selection`
clickable actions insert text before or after the current text selection. Like
`editor:replace-text-selection` and `editor:delete-text-selection`, these are
two-step operations: first select text, then insert or append around it.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/selection-insert-test.txt
text: |
  # Configuration
  database:
    host: localhost
    port: 5432
  cache:
    host: localhost
    port: 6379
```

## Insert Lines Before Selection

Select the database section first:

```editor:select-matching-text
file: ~/exercises/selection-insert-test.txt
text: "database:"
before: 0
after: 2
```

The markdown for inserting lines before the selection is:

~~~markdown
```editor:insert-lines-before-selection
file: ~/exercises/selection-insert-test.txt
text: |
  # --- Database section start ---
```
~~~

Click the action below to insert a comment before the selected block:

```editor:insert-lines-before-selection
file: ~/exercises/selection-insert-test.txt
text: |
  # --- Database section start ---
```

## Append Lines After Selection

Reset the file:

```editor:create-file
file: ~/exercises/selection-insert-test.txt
text: |
  # Configuration
  database:
    host: localhost
    port: 5432
  cache:
    host: localhost
    port: 6379
```

Select the database section:

```editor:select-matching-text
file: ~/exercises/selection-insert-test.txt
text: "database:"
before: 0
after: 2
```

The markdown for appending lines after the selection is:

~~~markdown
```editor:append-lines-after-selection
file: ~/exercises/selection-insert-test.txt
text: |
  # --- Database section end ---
```
~~~

Click the action below to append a comment after the selected block:

```editor:append-lines-after-selection
file: ~/exercises/selection-insert-test.txt
text: |
  # --- Database section end ---
```

## Combined: Wrap a Selection

You can combine both actions to wrap a selected block with content above and
below it.

Reset the file:

```editor:create-file
file: ~/exercises/selection-insert-test.txt
text: |
  # Configuration
  database:
    host: localhost
    port: 5432
  cache:
    host: localhost
    port: 6379
```

Select the cache section:

```editor:select-matching-text
file: ~/exercises/selection-insert-test.txt
text: "cache:"
before: 0
after: 2
```

Insert a comment before the selection:

```editor:insert-lines-before-selection
file: ~/exercises/selection-insert-test.txt
text: |
  # --- Cache section start ---
```

Now select the cache section again (the selection shifted due to the insert):

```editor:select-matching-text
file: ~/exercises/selection-insert-test.txt
text: "cache:"
before: 0
after: 2
```

Append a comment after the selection:

```editor:append-lines-after-selection
file: ~/exercises/selection-insert-test.txt
text: |
  # --- Cache section end ---
```
