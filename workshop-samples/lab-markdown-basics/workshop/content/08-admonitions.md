---
title: Admonitions
---

Educates provides custom admonition shortcodes for highlighting important
information. Three types are available: note, warning, and danger. Each renders
as a colored text box with a title.

## Note Admonition

The note admonition renders as a blue text box and is used for general
informational callouts. The markdown for a note is:

~~~
{{</* note */>}}
This is a note admonition. Use it to provide additional context or helpful
tips to the reader.
{{</* /note */>}}
~~~

{{< note >}}
This is a note admonition. Use it to provide additional context or helpful
tips to the reader.
{{< /note >}}

## Note with Custom Title

The markdown for a note with a custom title is:

~~~
{{</* note title="Tip" */>}}
You can customize the title of any admonition by passing the `title`
parameter to the shortcode.
{{</* /note */>}}
~~~

{{< note title="Tip" >}}
You can customize the title of any admonition by passing the `title`
parameter to the shortcode.
{{< /note >}}

## Warning Admonition

The warning admonition renders as a yellow text box and is used to alert
readers to potential issues. The markdown for a warning is:

~~~
{{</* warning */>}}
This is a warning admonition. Use it to highlight something the reader should
be cautious about.
{{</* /warning */>}}
~~~

{{< warning >}}
This is a warning admonition. Use it to highlight something the reader should
be cautious about.
{{< /warning >}}

## Warning with Custom Title

The markdown for a warning with a custom title is:

~~~
{{</* warning title="Caution" */>}}
Proceed carefully when modifying configuration files. Incorrect values may
cause unexpected behavior.
{{</* /warning */>}}
~~~

{{< warning title="Caution" >}}
Proceed carefully when modifying configuration files. Incorrect values may
cause unexpected behavior.
{{< /warning >}}

## Danger Admonition

The danger admonition renders as a red text box and is used for critical
warnings. The markdown for a danger admonition is:

~~~
{{</* danger */>}}
This is a danger admonition. Use it for critical information that could lead
to data loss or system failure if ignored.
{{</* /danger */>}}
~~~

{{< danger >}}
This is a danger admonition. Use it for critical information that could lead
to data loss or system failure if ignored.
{{< /danger >}}

## Danger with Custom Title

The markdown for a danger admonition with a custom title is:

~~~
{{</* danger title="Do Not Proceed" */>}}
This action is irreversible. Ensure you have a backup before continuing.
{{</* /danger */>}}
~~~

{{< danger title="Do Not Proceed" >}}
This action is irreversible. Ensure you have a backup before continuing.
{{< /danger >}}
