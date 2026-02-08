---
title: "Tests with Form Inputs"
---

The `examiner:execute-test` clickable action supports rendering an HTML form
for collecting user input. The `inputs` property uses the `jsonform` schema and
form format. When the form is submitted, the input values are passed as JSON on
standard input to the test script.

## Examiner Test Script

The test script for processing form inputs reads JSON from stdin. In this
workshop the `test-process-inputs` script is used. It saves the raw JSON to a
file and then checks that a `name` field is present:

~~~
#!/bin/bash

CONFIG=$(cat -)

echo "$CONFIG" > "$HOME/examiner-form-input.json"

NAME=$(echo "$CONFIG" | jq -r -e ".name" 2>/dev/null)

if [ "$?" != "0" ]; then
    exit 1
fi

if [ -z "$NAME" ]; then
    exit 1
fi

exit 0
~~~

The JSON received on stdin contains the form field values as key/value pairs.
After each test below you can view what was received by the script.

## Simple Text Input

The markdown for a test with a single text input field is:

~~~markdown
```examiner:execute-test
name: test-process-inputs
prefix: Task
title: Submit your name
inputs:
  schema:
    name:
      type: string
      title: "Name:"
      default: "my-app"
      required: true
  form:
  - "*"
  - type: submit
    title: Submit
```
~~~

Enter a name in the form below and click "Submit". The test reads the JSON from
stdin and verifies the `name` field is present. It should pass with any
non-empty name:

```examiner:execute-test
name: test-process-inputs
prefix: Task
title: Submit your name
inputs:
  schema:
    name:
      type: string
      title: "Name:"
      default: "my-app"
      required: true
  form:
  - "*"
  - type: submit
    title: Submit
```

After submitting, click below to see the raw JSON that was received by the test
script:

```terminal:execute
command: cat ~/examiner-form-input.json
```

## Multiple Input Fields

The form can include multiple fields of different types. The markdown for a test
with both string and integer inputs is:

~~~markdown
```examiner:execute-test
name: test-process-inputs
prefix: Task
title: Deploy application
inputs:
  schema:
    name:
      type: string
      title: "Application Name:"
      default: "my-app"
      required: true
    replicas:
      type: integer
      title: "Replicas:"
      default: "1"
      required: true
  form:
  - "*"
  - type: submit
    title: Deploy
```
~~~

Fill in both fields below and click "Deploy". The test should pass as long as
the name field is provided:

```examiner:execute-test
name: test-process-inputs
prefix: Task
title: Deploy application
inputs:
  schema:
    name:
      type: string
      title: "Application Name:"
      default: "my-app"
      required: true
    replicas:
      type: integer
      title: "Replicas:"
      default: "1"
      required: true
  form:
  - "*"
  - type: submit
    title: Deploy
```

After submitting, click below to see the raw JSON that was received by the test
script. You should see both the `name` and `replicas` fields:

```terminal:execute
command: cat ~/examiner-form-input.json
```
