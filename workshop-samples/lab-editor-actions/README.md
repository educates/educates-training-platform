Editor Clickable Actions Test
=============================

Test workshop for verifying all editor clickable action types provided by
Educates. This includes opening files, creating files, appending lines,
inserting lines before or after a line number, inserting lines after a match,
selecting matching text (exact and regex), replacing text selections, replacing
matching text in a single step, inserting values into YAML files, and executing
editor commands.

**Enabling the editor:**

To use the editor clickable actions, the editor must be enabled in the Workshop
definition. This is done by setting `spec.session.applications.editor.enabled`
to `true`:

```yaml
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-editor-actions
spec:
  session:
    applications:
      editor:
        enabled: true
```
