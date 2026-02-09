Workshop Samples
================

This directory contains various workshop samples. These are not intended for
end user consumption and are intended to provide some minimal pre-canned
workshops which can be used to test and demonstrate different features of
the Educates platform.

Deploying Workshops
-------------------

To deploy a workshop from one of the sub directories, first ensure you have
an Educates cluster available and the `educates` CLI installed. Then from the
root of the workshop sub directory, run the following commands.

Publish the workshop to the Educates cluster:

```
educates publish-workshop
```

Deploy the workshop so that it is available to users:

```
educates deploy-workshop
```

Once deployed, you can browse available workshops and access them by running:

```
educates browse-workshops
```

This will open a browser window where you can select and launch the workshop.

Available Workshops
-------------------

* [lab-dashboard-actions](lab-dashboard-actions/) - Test workshop for verifying
  all dashboard clickable action types including both the current preferred
  action formats and deprecated legacy formats.

* [lab-files-actions](lab-files-actions/) - Test workshop for verifying
  all files clickable action types including file download, download with custom
  name, download with preview, copy file to clipboard, single file upload, and
  multiple file upload.

* [lab-examiner-actions](lab-examiner-actions/) - Test workshop for verifying
  all examiner clickable action types including basic pass/fail tests, arguments,
  timeout, retries, form inputs, autostart, cascade, and cooldown.

* [lab-markdown-basics](lab-markdown-basics/) - Test workshop for verifying
  rendering of common markdown formatting including headings, text styles, lists,
  links, code blocks, tables, blockquotes, and admonition shortcodes.

* [lab-section-actions](lab-section-actions/) - Test workshop for verifying
  all section clickable action types including collapsible sections, nested
  sections, autostart within sections, cascade-triggered section closing,
  and section headings.

* [lab-terminal-actions](lab-terminal-actions/) - Test workshop for verifying
  all terminal clickable action types including both the current preferred
  action formats and deprecated legacy formats.
