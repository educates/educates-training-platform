---
title: "Workshop Copy"
---

The `workshop:copy` and `workshop:copy-and-edit` actions copy text to the system
paste buffer so it can be pasted into a terminal or other application. The
`workshop:copy-and-edit` variant displays a different icon to indicate that the
pasted value will need to be manually modified before use.

The `{{</* copy */>}}` shortcode provides inline copy functionality for code
spans within paragraph text.

## Copy Text to Paste Buffer

The markdown for this action is:

~~~markdown
```workshop:copy
text: echo "Hello from the paste buffer!"
```
~~~

Click the action below to copy the text, then paste it into a terminal to
verify:

```workshop:copy
text: echo "Hello from the paste buffer!"
```

## Copy Multi-line Text

The `text` property supports multi-line content using YAML block scalars.

The markdown for this action is:

~~~markdown
```workshop:copy
text: |-
  echo "Line 1"
  echo "Line 2"
  echo "Line 3"
```
~~~

Click the action below to copy the multi-line text:

```workshop:copy
text: |-
  echo "Line 1"
  echo "Line 2"
  echo "Line 3"
```

## Copy and Edit

The `workshop:copy-and-edit` action works the same as `workshop:copy` but
displays an edit icon to signal that the copied text contains placeholder values
that should be changed before use.

The markdown for this action is:

~~~markdown
```workshop:copy-and-edit
text: export MY_VARIABLE=<insert-value-here>
```
~~~

Click the action below to copy the text:

```workshop:copy-and-edit
text: export MY_VARIABLE=<insert-value-here>
```

## Inline Copy Shortcode

The `{{</* copy */>}}` shortcode can be placed immediately after an inline code
span. It renders a small copy icon next to the code span. Clicking anywhere
within the code span copies its content to the paste buffer. The copy icon will
momentarily show in an inverted style to indicate the content has been copied.

The markdown for this is:

~~~markdown
Run the command `echo "inline copy test"`{{</* copy */>}} to verify.
~~~

Run the command `echo "inline copy test"`{{< copy >}} to verify.
