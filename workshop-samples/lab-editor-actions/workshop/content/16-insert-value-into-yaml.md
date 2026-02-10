---
title: "Deprecated: Insert Value into YAML"
---

The `editor:insert-value-into-yaml` clickable action inserts a YAML value into
an existing YAML structure at a specified path. The `path` property uses dot
notation to navigate the YAML structure.

This clickable action is deprecated as it never really worked properly as may
have been intended or broke at some point. It can only append items to an
existing list and does not support JSON style list values in YAML.

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
        containers:
        - name: sidecar
          image: image:latest
```

## Insert a Value at a YAML Path

The `path` property specifies where in the YAML structure to insert the value.
The `value` property is the YAML value to insert. Because of issues with the
implementation this can only be a non empty list.

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

Click the action below to insert an extra container definition into the deployment:

```editor:insert-value-into-yaml
file: ~/exercises/deployment.yaml
path: spec.template.spec.containers
value:
- name: nginx
  image: nginx:latest
```
