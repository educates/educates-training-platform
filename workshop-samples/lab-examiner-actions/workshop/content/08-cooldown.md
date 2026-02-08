---
title: "Cooldown Period"
---

After a clickable action is triggered, a cooldown period prevents it from being
clicked again for a specified duration. The default cooldown for most actions is
3 seconds. The `cooldown` property can override this. The value is in seconds.
The special value `.INF` prevents the action from ever being clicked a second
time.

Note that reloading the page resets all cooldown periods.

The tests on this page use a `test-log-execution` script that appends a
timestamped line to a log file each time it runs. This makes it possible to
confirm whether clicking on an action during the cooldown period actually
triggered the test or was ignored.

Before running the tests, click the action below to start tailing the log file
in the terminal. Each time a test runs, you will see a new line appear:

```terminal:execute
command: touch ~/examiner-cooldown.log && tail -f ~/examiner-cooldown.log
```

## Default Cooldown

Without specifying a cooldown, the default period applies. The markdown for a
basic test is:

~~~markdown
```examiner:execute-test
name: test-log-execution
title: Test with default cooldown
```
~~~

Click the action below and try clicking it again immediately. Watch the
terminal to confirm the second click does not produce a new log line during the
cooldown period:

```examiner:execute-test
name: test-log-execution
title: Test with default cooldown
```

## Custom Cooldown

The `cooldown` property can be set to a custom duration. The markdown for a test
with a 10 second cooldown is:

~~~markdown
```examiner:execute-test
name: test-log-execution
title: Test with 10 second cooldown
cooldown: 10
```
~~~

Click the action below and try clicking it again. Watch the terminal to confirm
the test is not re-executed for 10 seconds:

```examiner:execute-test
name: test-log-execution
title: Test with 10 second cooldown
cooldown: 10
```

## Infinite Cooldown Using .INF

Setting `cooldown` to `.INF` prevents the action from being clicked a second
time. The markdown for this is:

~~~markdown
```examiner:execute-test
name: test-log-execution
title: Test that can only run once
cooldown: .INF
```
~~~

Click the action below. Watch the terminal to confirm only one log line appears
no matter how many times you click:

```examiner:execute-test
name: test-log-execution
title: Test that can only run once (using .INF)
cooldown: .INF
```

## Infinite Cooldown Using -1

The same behaviour can be achieved by setting `cooldown` to `-1` instead of
`.INF`.

~~~markdown
```examiner:execute-test
name: test-log-execution
title: Test that can only run once
cooldown: -1
```
~~~

Click the action below. Watch the terminal to confirm only one log line appears
no matter how many times you click:

```examiner:execute-test
name: test-log-execution
title: Test that can only run once (using -1)
cooldown: -1
```

When done, interrupt the tail command:

```terminal:interrupt
```
