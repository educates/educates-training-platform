---
title: "Retries and Delay"
---

When a test fails, it can be retried automatically by setting the `retries`
property. The `delay` property controls how many seconds to wait between
retries. Testing stops as soon as the test passes.

## Failing Test with Retries

The `test-always-fails` script will never pass, so it should retry the
specified number of times and then show a failure.

The markdown for this is:

~~~markdown
```examiner:execute-test
name: test-always-fails
title: Verify retries are exhausted
retries: 3
delay: 1
```
~~~

Click the action below. It should show a spinner while retrying, and then fail
after 3 retries:

```examiner:execute-test
name: test-always-fails
title: Verify retries are exhausted
retries: 3
delay: 1
```

## Passing Test with Retries

The `test-always-passes` script passes on the first attempt, so no retries
should occur even though they are configured.

~~~markdown
```examiner:execute-test
name: test-always-passes
title: Verify test passes on first attempt
retries: 5
delay: 1
```
~~~

Click the action below. It should pass immediately without any retries:

```examiner:execute-test
name: test-always-passes
title: Verify test passes on first attempt
retries: 5
delay: 1
```

## Infinite Retries Until Condition Met

The `retries` property can be set to the special YAML value `.INF` or to `-1`
to retry indefinitely until the test passes or the page is navigated away from.
This is useful for waiting until the user has completed some action.

In this example, the test checks whether a file `~/examiner-test-file` exists.
First click the command to create the file, then the test will pass on the
next retry.

The markdown for the test with infinite retries using `.INF` is:

~~~markdown
```examiner:execute-test
name: test-check-file-exists
title: Verify ~/examiner-test-file exists
args:
- /home/eduk8s/examiner-test-file
retries: .INF
delay: 1
timeout: 5
```
~~~

Click the test action first. It will start retrying and show a spinner:

```examiner:execute-test
name: test-check-file-exists
title: Verify ~/examiner-test-file exists (using .INF)
args:
- /home/eduk8s/examiner-test-file
retries: .INF
delay: 1
timeout: 5
```

Now click the action below to create the file. The test above should then pass
on the next retry:

```terminal:execute
command: touch ~/examiner-test-file
```

To reset this test for re-running, remove the file:

```terminal:execute
command: rm -f ~/examiner-test-file
```

## Infinite Retries Using -1

The same behaviour can be achieved by setting `retries` to `-1` instead of
`.INF`.

~~~markdown
```examiner:execute-test
name: test-check-file-exists
title: Verify ~/examiner-test-file exists
args:
- /home/eduk8s/examiner-test-file
retries: -1
delay: 1
timeout: 5
```
~~~

Click the test action first. It will start retrying and show a spinner:

```examiner:execute-test
name: test-check-file-exists
title: Verify ~/examiner-test-file exists (using -1)
args:
- /home/eduk8s/examiner-test-file
retries: -1
delay: 1
timeout: 5
```

Now click the action below to create the file. The test above should then pass
on the next retry:

```terminal:execute
command: touch ~/examiner-test-file
```

To reset this test for re-running, remove the file:

```terminal:execute
command: rm -f ~/examiner-test-file
```
