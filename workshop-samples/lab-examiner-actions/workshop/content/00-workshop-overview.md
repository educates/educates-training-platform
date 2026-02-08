---
title: Workshop Overview
---

This workshop is a test suite for verifying the `examiner:execute-test`
clickable action supported by Educates. It is not intended as an end-user
workshop but rather as a functional test to confirm that the examiner action
behaves correctly across all its configuration options.

The following features are tested across the workshop pages:

**Basic functionality:**

- Simple passing and failing tests
- Tests located in subdirectories
- Tests accepting command-line arguments

**Configuration options:**

- `timeout` — Kill tests that run too long
- `retries` — Retry failed tests a specified number of times
- `delay` — Wait between retries
- `prefix` — Override the default "Examiner" prefix on the action title
- `title` — Custom title text for the action
- `description` — Custom body text for the action

**Form inputs:**

- `inputs` — Render an HTML form and pass values as JSON on stdin

**Behavioural options:**

- `autostart` — Automatically trigger the test when the page loads
- `cascade` — Trigger the next clickable action after a successful test
- `cooldown` — Control the delay before the action can be clicked again

**Enabling the examiner:**

To use the `examiner:execute-test` clickable action, the examiner must be
enabled in the Workshop definition. This is done by setting
`spec.session.applications.examiner.enabled` to `true`:

```yaml
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-examiner-actions
spec:
  session:
    applications:
      examiner:
        enabled: true
```

Test scripts must be placed in the `workshop/examiner/tests` directory and be
executable. They can also be organized into subdirectories.
