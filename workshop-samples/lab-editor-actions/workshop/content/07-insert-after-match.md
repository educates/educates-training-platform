---
title: "Insert Lines After Match"
---

The `editor:append-lines-after-match` clickable action inserts text after the
first line in the file that contains a matching string. This is useful when you
want to insert content relative to a known marker in the file rather than at a
fixed line number.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/match-insert-test.txt
text: |
  # Configuration
  database:
    host: localhost
    port: 5432
  # End of database section
  cache:
    host: localhost
    port: 6379
  # End of cache section
```

## Insert After a Matching Line

The `match` property specifies the text to search for. The new text is inserted
after the first line containing the match.

The markdown for inserting after a matching line is:

~~~markdown
```editor:append-lines-after-match
file: ~/exercises/match-insert-test.txt
match: "port: 5432"
text: |2
    name: mydb
```
~~~

Click the action below to insert a line after the database port line:

```editor:append-lines-after-match
file: ~/exercises/match-insert-test.txt
match: "port: 5432"
text: |2
    name: mydb
```

The `|2` syntax is a YAML literal block scalar with an explicit indentation
indicator, used here to preserve the leading spaces in the inserted text.

## Insert Multiple Lines After Match

You can insert multiple lines after the matched line.

Reset the file first:

```editor:create-file
file: ~/exercises/match-insert-test.txt
text: |
  # Configuration
  database:
    host: localhost
    port: 5432
  # End of database section
  cache:
    host: localhost
    port: 6379
  # End of cache section
```

The markdown for inserting multiple lines after a match is:

~~~markdown
```editor:append-lines-after-match
file: ~/exercises/match-insert-test.txt
match: "# End of database section"
text: |
  logging:
    level: debug
    file: /var/log/app.log
```
~~~

Click the action below to insert multiple lines after the database section
comment:

```editor:append-lines-after-match
file: ~/exercises/match-insert-test.txt
match: "# End of database section"
text: |
  logging:
    level: debug
    file: /var/log/app.log
  # End of logging section
```
