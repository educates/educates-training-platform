Examiner Clickable Actions Test
================================

Test workshop for verifying all `examiner:execute-test` clickable action types
provided by Educates. This includes basic pass/fail tests, tests with arguments,
timeout handling, retries with delay, custom prefix/title/description, form
inputs, autostart, cascade, and cooldown behavior.

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
