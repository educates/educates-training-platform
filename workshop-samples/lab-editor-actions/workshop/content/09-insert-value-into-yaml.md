---
title: "Insert Value into YAML"
---

The `editor:insert-value-into-yaml` clickable action inserts a YAML value into
an existing YAML structure at a specified path. The `path` property uses dot
notation to navigate the YAML structure.

First, create a sample YAML file to work with:

```editor:create-file
file: ~/exercises/deployment.yaml
text: |
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: myapp
  spec:
    replicas: 1
    template:
      spec:
        containers: []
```

## Insert a Value at a YAML Path

The `path` property specifies where in the YAML structure to insert the value.
The `value` property is the YAML value to insert.

The markdown for inserting a value into a YAML file is:

~~~markdown
```editor:insert-value-into-yaml
file: ~/exercises/deployment.yaml
path: spec.template.spec.containers
value:
- name: nginx
  image: nginx:latest
```
~~~

Click the action below to insert a container definition into the deployment:

```editor:insert-value-into-yaml
file: ~/exercises/deployment.yaml
path: spec.template.spec.containers
value:
- name: nginx
  image: nginx:latest
```

## Insert a Simple Value

The value can be a simple scalar, not just a complex structure.

Reset the file:

```editor:create-file
file: ~/exercises/deployment.yaml
text: |
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: myapp
    labels: {}
  spec:
    replicas: 1
```

The markdown for inserting a simple value is:

~~~markdown
```editor:insert-value-into-yaml
file: ~/exercises/deployment.yaml
path: metadata.labels
value:
  app: myapp
  version: v1
```
~~~

Click the action below to insert labels:

```editor:insert-value-into-yaml
file: ~/exercises/deployment.yaml
path: metadata.labels
value:
  app: myapp
  version: v1
```
