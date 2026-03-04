Sample Screenshots
==================

The workshop dashboard is the primary interface for users working through a workshop. It displays the workshop instructions on the left hand side and a series of tabbed views on the right hand side. Users follow the instructions step by step, interacting with the live environment alongside them.

For workshops requiring commands to be run, one or more terminal shells will be provided. Commands in the instructions can be annotated as clickable, so that clicking on them automatically executes the command in the correct terminal.

![](dashboard-terminal.png)

The terminals provide access to the editors ``vi`` and ``nano``, but if you want to provide a UI based editor, you can enable the embedded editor view. The embedded IDE is based on VS Code.

![](dashboard-editor.png)

To complement the workshop instructions, or to be available for use by the instructor, slides can be included with a workshop. For slides you can use HTML based slide presentation tools such as ``reveal.js``, ``impress.js``, or you can embed a PDF file.

![](dashboard-slides.png)

If the workshop involves working with Kubernetes, you can enable a web console for accessing the Kubernetes cluster. The default web console uses the Kubernetes dashboard.

![](dashboard-console-kubernetes.png)
