---
title: "Execute Command"
---

The `editor:execute-command` clickable action executes a registered VS Code
command. This can be used to trigger editor extensions or built-in VS Code
functionality.

## Execute a VS Code Command

The `command` property specifies the VS Code command ID to execute. The `args`
property provides an optional list of arguments to pass to the command.

The markdown for executing a VS Code command is:

~~~markdown
```editor:execute-command
command: workbench.action.terminal.toggleTerminal
```
~~~

Click the action below to toggle the VS Code integrated terminal:

```editor:execute-command
command: workbench.action.terminal.toggleTerminal
```

## Execute a Command with Arguments

Some VS Code commands accept arguments. These are provided as a list in the
`args` property.

The markdown for a command with arguments is:

~~~markdown
```editor:execute-command
command: workbench.action.openSettings
args:
- editor.fontSize
```
~~~

Click the action below to open the VS Code settings:

```editor:execute-command
command: workbench.action.openSettings
args:
- editor.fontSize
```
