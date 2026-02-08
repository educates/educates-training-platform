---
title: "Tests with Arguments"
---

Arguments can be passed to test scripts using the `args` field. The arguments
are provided as a YAML list and will be passed as positional parameters to the
test script.

## Test with Matching Argument

The `test-check-arg` script exits 0 when the first argument is "hello". The
markdown for this is:

~~~markdown
```examiner:execute-test
name: test-check-arg
title: Verify argument matches "hello"
args:
- hello
```
~~~

Click the action below. It should pass because the argument matches:

```examiner:execute-test
name: test-check-arg
title: Verify argument matches "hello"
args:
- hello
```

## Test with Non-Matching Argument

When a different argument is provided, the test should fail:

~~~markdown
```examiner:execute-test
name: test-check-arg
title: Verify argument does not match "world"
args:
- world
```
~~~

Click the action below. It should fail because the argument does not match:

```examiner:execute-test
name: test-check-arg
title: Verify argument does not match "world"
args:
- world
```

## Test Checking File Exists

The `test-check-file-exists` script checks whether a file exists at the path
given as the first argument.

The markdown for checking a file that should exist is:

~~~markdown
```examiner:execute-test
name: test-check-file-exists
title: Verify /etc/hostname exists
args:
- /etc/hostname
```
~~~

Click the action below. It should pass because `/etc/hostname` exists:

```examiner:execute-test
name: test-check-file-exists
title: Verify /etc/hostname exists
args:
- /etc/hostname
```

## Test Checking File Does Not Exist

The markdown for checking a file that should not exist is:

~~~markdown
```examiner:execute-test
name: test-check-file-exists
title: Verify /nonexistent does not exist
args:
- /nonexistent
```
~~~

Click the action below. It should fail because `/nonexistent` does not exist:

```examiner:execute-test
name: test-check-file-exists
title: Verify /nonexistent does not exist
args:
- /nonexistent
```
