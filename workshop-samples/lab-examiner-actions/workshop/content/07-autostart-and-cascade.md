---
title: "Autostart and Cascade"
---

The `autostart` property causes a test to be triggered automatically when the
page loads, rather than requiring the user to click on it. The `cascade`
property causes the next clickable action on the page to be triggered
automatically after the current one succeeds.

## Autostart Test

The markdown for a test that runs automatically on page load is:

~~~markdown
```examiner:execute-test
name: test-always-passes
title: Auto-run verification check
autostart: true
```
~~~

The test below should have already run and show a success indicator without
needing to be clicked:

```examiner:execute-test
name: test-always-passes
title: Auto-run verification check
autostart: true
```

## Cascade Between Tests

When `cascade` is set to `true`, the next clickable action on the page is
triggered automatically after the current test succeeds. The markdown for this
is:

~~~markdown
```examiner:execute-test
name: test-always-passes
title: First test (cascades to next)
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Second test (triggered by cascade)
```
~~~

Click the first test below. After it passes, the second test should trigger
automatically:

```examiner:execute-test
name: test-always-passes
title: First test (cascades to next)
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Second test (triggered by cascade)
```

## Autostart with Cascade

Combining `autostart` and `cascade` causes the first test to run automatically
and then trigger the next. The markdown for this is:

~~~markdown
```examiner:execute-test
name: test-always-passes
title: Auto-run first check
autostart: true
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Auto-run second check (via cascade)
```
~~~

Both tests below should have already run and show success indicators without
needing to be clicked:

```examiner:execute-test
name: test-always-passes
title: Auto-run first check
autostart: true
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Auto-run second check (via cascade)
```

## Cascade Chain of Three Tests

Multiple tests can be chained together using `cascade`. Each test triggers the
next one after it succeeds:

~~~markdown
```examiner:execute-test
name: test-always-passes
title: Step 1 of 3
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Step 2 of 3
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Step 3 of 3
```
~~~

Click the first test below and watch all three cascade in sequence:

```examiner:execute-test
name: test-always-passes
title: Step 1 of 3
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Step 2 of 3
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Step 3 of 3
```

## Autostart Inside a Collapsible Section

When a test with `autostart: true` is inside a collapsible section, it should
only trigger when the section is expanded, not when the page loads.

The markdown for this is:

~~~markdown
```section:begin
name: autostart-section
title: Expand to Auto-Run Tests
```

```examiner:execute-test
name: test-always-passes
title: First check (auto-runs on expand)
autostart: true
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Second check (triggered by cascade)
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Third check (triggered by cascade)
```

```section:end
name: autostart-section
```
~~~

The tests below should NOT have run yet. Click to expand the section and verify
the first test triggers automatically, then cascades through the remaining two:

```section:begin
name: autostart-section
title: Expand to Auto-Run Tests
```

The first test below should have run automatically when this section was
expanded, then cascaded to the second and third.

```examiner:execute-test
name: test-always-passes
title: First check (auto-runs on expand)
autostart: true
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Second check (triggered by cascade)
cascade: true
```

```examiner:execute-test
name: test-always-passes
title: Third check (triggered by cascade)
```

```section:end
name: autostart-section
```

## Cascade to Close Section

When the last clickable action in a section has `cascade: true`, it triggers
the `section:end` block, causing the section to collapse automatically.

The markdown for this is:

~~~markdown
```section:begin
name: cascade-close
title: Expand to Run Test and Auto-Close
```

```examiner:execute-test
name: test-always-passes
title: Test runs then section closes
cascade: true
```

```section:end
name: cascade-close
```
~~~

Click to expand the section below. Click the test action and the section should
close automatically after the test passes:

```section:begin
name: cascade-close
title: Expand to Run Test and Auto-Close
```

Click the test below. After it passes, this section should collapse:

```examiner:execute-test
name: test-always-passes
title: Test runs then section closes
cascade: true
```

```section:end
name: cascade-close
```
