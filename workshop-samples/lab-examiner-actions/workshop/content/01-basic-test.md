---
title: "Basic Examiner Test"
---

The `examiner:execute-test` clickable action runs a test script located in the
`workshop/examiner/tests` directory. The test script must be an executable
program that exits with status 0 on success and non-zero on failure.

## Simple Passing Test

The markdown for a test that always passes is:

~~~markdown
```examiner:execute-test
name: test-always-passes
title: Verify test passes
```
~~~

Click the action below to run the test. It should show a success indicator:

```examiner:execute-test
name: test-always-passes
title: Verify test passes
```

## Simple Failing Test

The markdown for a test that always fails is:

~~~markdown
```examiner:execute-test
name: test-always-fails
title: Verify test fails
```
~~~

Click the action below to run the test. It should show a failure indicator:

```examiner:execute-test
name: test-always-fails
title: Verify test fails
```

## Test in a Subdirectory

Test scripts can be organized in subdirectories under `workshop/examiner/tests`.
The subdirectory prefix must be included in the `name` field.

The markdown for a test in a subdirectory is:

~~~markdown
```examiner:execute-test
name: subdir/test-in-subdir
title: Verify test in subdirectory passes
```
~~~

Click the action below to run the test from the subdirectory:

```examiner:execute-test
name: subdir/test-in-subdir
title: Verify test in subdirectory passes
```
