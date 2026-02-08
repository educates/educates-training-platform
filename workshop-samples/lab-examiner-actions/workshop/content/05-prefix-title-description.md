---
title: "Prefix, Title, and Description"
---

The `examiner:execute-test` clickable action defaults to using "Examiner" as the
prefix in the banner. The `prefix`, `title`, and `description` properties can be
used to customize the display. The banner is shown as "Prefix: Title" and the
description appears in the body of the action block.

## Custom Prefix

The markdown for a test with a custom prefix is:

~~~markdown
```examiner:execute-test
name: test-always-passes
prefix: Task
title: System check passed
```
~~~

The action below should display as "Task: System check passed":

```examiner:execute-test
name: test-always-passes
prefix: Task
title: System check passed
```

## Custom Title

The markdown for a test with a custom title is:

~~~markdown
```examiner:execute-test
name: test-always-passes
title: Verify the system is ready
```
~~~

The action below should display as "Examiner: Verify the system is ready":

```examiner:execute-test
name: test-always-passes
title: Verify the system is ready
```

## Custom Description

The `description` property provides body text displayed inside the action block.
Note that descriptions are always rendered as preformatted text.

The markdown for a test with a custom description is:

~~~markdown
```examiner:execute-test
name: test-always-passes
title: Check system status
description: This test verifies that the system is in a healthy state and ready for the next exercise.
```
~~~

The action below should display the description text in the body:

```examiner:execute-test
name: test-always-passes
title: Check system status
description: This test verifies that the system is in a healthy state and ready for the next exercise.
```

## Combined Prefix, Title, and Description

All three properties can be used together:

~~~markdown
```examiner:execute-test
name: test-always-passes
prefix: Step 1
title: Environment Validation
description: Confirms all prerequisites are met before proceeding.
```
~~~

The action below should display as "Step 1: Environment Validation" with the
description in the body:

```examiner:execute-test
name: test-always-passes
prefix: Step 1
title: Environment Validation
description: Confirms all prerequisites are met before proceeding.
```
