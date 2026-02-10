---
title: "YAML Operations"
---

The YAML operation clickable actions provide structured manipulation of YAML
files with comment preservation. They use the YAML library's document API for
round-trip editing, correctly handling all YAML styles including flow/inline
syntax.

First, create a sample YAML file to work with:

```editor:create-file
file: ~/exercises/config.yaml
text: |
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: myapp
    # Environment labels
    labels:
      app: myapp
  spec:
    replicas: 1
    template:
      spec:
        containers:
        - name: nginx
          image: nginx:latest
          ports:
          - containerPort: 80
```

## Set a Value

The `editor:yaml-set` action sets or updates a value at a YAML path. If
intermediate keys don't exist, they will be created.

The markdown for setting a value is:

~~~markdown
```editor:yaml-set
file: ~/exercises/config.yaml
path: spec.replicas
value: 3
```
~~~

Click the action below to change the replica count:

```editor:yaml-set
file: ~/exercises/config.yaml
path: spec.replicas
value: 3
```

Note that the comment on the labels line is preserved.

Set a nested value that creates intermediate keys:

```editor:yaml-set
file: ~/exercises/config.yaml
path: spec.selector.matchLabels.app
value: myapp
```

## Add Item to Sequence

The `editor:yaml-add-item` action appends an item to the end of a YAML
sequence.

The markdown for adding an item is:

~~~markdown
```editor:yaml-add-item
file: ~/exercises/config.yaml
path: spec.template.spec.containers
value:
  name: sidecar
  image: busybox:latest
```
~~~

Click the action below to add a sidecar container:

```editor:yaml-add-item
file: ~/exercises/config.yaml
path: spec.template.spec.containers
value:
  name: sidecar
  image: busybox:latest
```

## Insert Item at Position

The `editor:yaml-insert-item` action inserts an item at a specific index in a
YAML sequence.

The markdown for inserting an item at a specific position is:

~~~markdown
```editor:yaml-insert-item
file: ~/exercises/config.yaml
path: spec.template.spec.containers
index: 0
value:
  name: init
  image: alpine:latest
  command: ["echo", "initializing"]
```
~~~

Click the action below to insert an init container at position 0:

```editor:yaml-insert-item
file: ~/exercises/config.yaml
path: spec.template.spec.containers
index: 0
value:
  name: init
  image: alpine:latest
  command: ["echo", "initializing"]
```

## Replace Sequence Item

The `editor:yaml-replace-item` action replaces a specific item in a sequence,
identified by index or attribute match.

Replace by attribute match using `[key=value]` syntax in the path:

~~~markdown
```editor:yaml-replace-item
file: ~/exercises/config.yaml
path: spec.template.spec.containers[name=nginx]
value:
  name: nginx
  image: nginx:1.25
  ports:
  - containerPort: 8080
```
~~~

Click the action below to replace the nginx container with an updated version:

```editor:yaml-replace-item
file: ~/exercises/config.yaml
path: spec.template.spec.containers[name=nginx]
value:
  name: nginx
  image: nginx:1.25
  ports:
  - containerPort: 8080
```

Replace by index:

```editor:yaml-replace-item
file: ~/exercises/config.yaml
path: spec.template.spec.containers[0]
value:
  name: updated-init
  image: alpine:3.18
```

## Delete a Key or Item

The `editor:yaml-delete` action deletes a key from a mapping or an item from a
sequence.

Delete a mapping key:

~~~markdown
```editor:yaml-delete
file: ~/exercises/config.yaml
path: metadata.labels.app
```
~~~

Click the action below to delete the `app` label:

```editor:yaml-delete
file: ~/exercises/config.yaml
path: metadata.labels.app
```

Delete a sequence item by attribute match:

```editor:yaml-delete
file: ~/exercises/config.yaml
path: spec.template.spec.containers[name=sidecar]
```

## Merge Values into Mapping

The `editor:yaml-merge` action merges multiple key-value pairs into an existing
YAML mapping. Existing keys are updated, new keys are added.

The markdown for merging values is:

~~~markdown
```editor:yaml-merge
file: ~/exercises/config.yaml
path: metadata.labels
value:
  app: myapp
  version: v2
  tier: frontend
```
~~~

Click the action below to merge labels:

```editor:yaml-merge
file: ~/exercises/config.yaml
path: metadata.labels
value:
  app: myapp
  version: v2
  tier: frontend
```

## Select a YAML Path

The `editor:yaml-select` action selects (highlights) a YAML node at a specific
path in the editor. For mapping entries, both the key and its value are selected.

First, recreate the sample file so we have a clean starting point:

```editor:create-file
file: ~/exercises/config.yaml
text: |
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: myapp
    # Environment labels
    labels:
      app: myapp
  spec:
    replicas: 1
    template:
      spec:
        containers:
        - name: nginx
          image: nginx:latest
          ports:
          - containerPort: 80
        - name: sidecar
          image: busybox:latest
```

The markdown for selecting a YAML path is:

~~~markdown
```editor:yaml-select
file: ~/exercises/config.yaml
path: spec.template.spec.containers
```
~~~

Select a scalar value (key and value will be highlighted):

```editor:yaml-select
file: ~/exercises/config.yaml
path: spec.replicas
```

Select a mapping (key and all nested content):

```editor:yaml-select
file: ~/exercises/config.yaml
path: metadata.labels
```

Select a sequence (key and all list items):

```editor:yaml-select
file: ~/exercises/config.yaml
path: spec.template.spec.containers
```

Select a sequence item by index:

```editor:yaml-select
file: ~/exercises/config.yaml
path: spec.template.spec.containers[0]
```

Select a sequence item by attribute match:

```editor:yaml-select
file: ~/exercises/config.yaml
path: spec.template.spec.containers[name=sidecar]
```

Select a deeply nested value:

```editor:yaml-select
file: ~/exercises/config.yaml
path: spec.template.spec.containers[name=nginx].image
```

## Flow Style YAML

These actions also work correctly with flow-style (inline) YAML. Create a file
with flow-style sequences:

```editor:create-file
file: ~/exercises/flow-style.yaml
text: |
  name: example
  # Inline list
  tags: [alpha, beta, gamma]
  config: {debug: true, verbose: false}
```

Set a value in the flow-style mapping:

```editor:yaml-set
file: ~/exercises/flow-style.yaml
path: config.debug
value: false
```

Delete from the flow-style mapping:

```editor:yaml-delete
file: ~/exercises/flow-style.yaml
path: config.verbose
```
