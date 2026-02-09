Files Clickable Actions Test
============================

Test workshop for verifying all `files:download-file`, `files:copy-file`,
`files:upload-file`, and `files:upload-files` clickable action types provided
by Educates. This includes basic file download, download with custom name,
download with preview, copy file to clipboard, single file upload, and multiple
file upload.

**Enabling file downloads and uploads:**

To use the `files:download-file` and `files:copy-file` clickable actions, the
files application must be enabled in the Workshop definition. To use the
`files:upload-file` and `files:upload-files` clickable actions, the uploads
application must be enabled. This is done by setting
`spec.session.applications.files.enabled` and
`spec.session.applications.uploads.enabled` to `true`:

```yaml
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-files-actions
spec:
  session:
    applications:
      files:
        enabled: true
      uploads:
        enabled: true
```
