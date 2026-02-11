---
title: "Delete Matching Lines"
---

The `editor:delete-matching-lines` clickable action deletes lines that contain a
matching string or regular expression pattern.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/delete-match-test.txt
text: |
  # Application Config
  database:
    host: localhost
    port: 5432
  # TODO: remove this debug setting
  debug: true
  cache:
    host: localhost
    port: 6379
  # End of config
```

## Delete a Single Matching Line

The `match` property specifies the text to search for. The first line containing
the match text is deleted.

The markdown for deleting a matching line is:

~~~markdown
```editor:delete-matching-lines
file: ~/exercises/delete-match-test.txt
match: "# TODO: remove this debug setting"
```
~~~

Click the action below to delete the TODO comment line:

```editor:delete-matching-lines
file: ~/exercises/delete-match-test.txt
match: "# TODO: remove this debug setting"
```

## Delete Lines Before and After Match

The `before` and `after` properties specify how many additional lines to delete
before and after the matched line. Setting either to `-1` means all lines in
that direction.

Reset the file:

```editor:create-file
file: ~/exercises/delete-match-test.txt
text: |
  # Application Config
  database:
    host: localhost
    port: 5432
  cache:
    host: localhost
    port: 6379
  # End of config
```

The markdown for deleting lines before and after a match is:

~~~markdown
```editor:delete-matching-lines
file: ~/exercises/delete-match-test.txt
match: "host: localhost"
before: 1
after: 1
```
~~~

Click the action below to delete the first `host: localhost` line along with one
line before and after it:

```editor:delete-matching-lines
file: ~/exercises/delete-match-test.txt
match: "host: localhost"
before: 1
after: 1
```

## Delete Matching Lines with Regex

Setting `isRegex` to `true` allows the `match` property to be a regular
expression.

Reset the file:

```editor:create-file
file: ~/exercises/delete-match-test.txt
text: |
  # Application Config
  database:
    host: localhost
    port: 5432
  # TODO: remove debug
  debug: true
  cache:
    host: localhost
    port: 6379
```

The markdown for deleting a line matching a regex is:

~~~markdown
```editor:delete-matching-lines
file: ~/exercises/delete-match-test.txt
match: "^\\s*#.*TODO.*"
isRegex: true
after: 1
```
~~~

Click the action below to delete the TODO comment and the line after it using a
regex match:

```editor:delete-matching-lines
file: ~/exercises/delete-match-test.txt
match: "^\\s*#.*TODO.*"
isRegex: true
after: 1
```
