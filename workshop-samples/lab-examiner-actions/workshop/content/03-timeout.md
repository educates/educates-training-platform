---
title: "Timeout Handling"
---

By default, test scripts are killed after 15 seconds if they have not completed.
The `timeout` property can be used to override this. The value is in seconds. A
value of 0 will result in the default timeout being applied.

## Test with Sufficient Timeout

The `test-slow` script sleeps for 3 seconds before exiting successfully. With a
timeout of 10 seconds, the test should have enough time to complete.

The markdown for this is:

~~~markdown
```examiner:execute-test
name: test-slow
title: Verify test completes within timeout
timeout: 10
```
~~~

Click the action below. It should pass after approximately 3 seconds:

```examiner:execute-test
name: test-slow
title: Verify test completes within timeout
timeout: 10
```

## Test with Short Timeout

With a timeout of 1 second, the `test-slow` script will be killed before it
completes.

The markdown for this is:

~~~markdown
```examiner:execute-test
name: test-slow
title: Verify test is killed by timeout
timeout: 1
```
~~~

Click the action below. It should fail because the test is killed before the 3
second sleep completes:

```examiner:execute-test
name: test-slow
title: Verify test is killed by timeout
timeout: 1
```
