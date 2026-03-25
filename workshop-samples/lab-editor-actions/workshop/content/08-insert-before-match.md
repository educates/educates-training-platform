---
title: "Insert Lines Before Match"
---

The `editor:insert-lines-before-match` clickable action inserts text before the
first line in the file that contains a matching string. This complements the
`editor:append-lines-after-match` action covered in the previous page.

First, create a sample file to work with:

```editor:create-file
file: ~/exercises/match-before-test.txt
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

## Insert Before a Matching Line

The `match` property specifies the text to search for. The new text is inserted
before the first line containing the match.

The markdown for inserting before a matching line is:

~~~markdown
```editor:insert-lines-before-match
file: ~/exercises/match-before-test.txt
match: "database:"
text: |
  # Database configuration below
```
~~~

Click the action below to insert a comment line before the database section:

```editor:insert-lines-before-match
file: ~/exercises/match-before-test.txt
match: "database:"
text: |
  # Database configuration below
```

## Insert Multiple Lines Before Match

You can insert multiple lines before the matched line.

Reset the file first:

```editor:create-file
file: ~/exercises/match-before-test.txt
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

The markdown for inserting multiple lines before a match is:

~~~markdown
```editor:insert-lines-before-match
file: ~/exercises/match-before-test.txt
match: "cache:"
text: |
  # Cache configuration below
  # Added by workshop exercise
```
~~~

Click the action below to insert multiple lines before the cache section:

```editor:insert-lines-before-match
file: ~/exercises/match-before-test.txt
match: "cache:"
text: |
  # Cache configuration below
  # Added by workshop exercise
```

## Insert Indented Lines Before Match

Use the `|2` YAML block scalar syntax to preserve leading indentation in the
inserted text.

Reset the file first:

```editor:create-file
file: ~/exercises/match-before-test.txt
text: |
  # Configuration
  database:
    host: localhost
    port: 5432
  # End of database section
```

The markdown for inserting indented lines before a match is:

~~~markdown
```editor:insert-lines-before-match
file: ~/exercises/match-before-test.txt
match: "port: 5432"
text: |2
    name: mydb
```
~~~

Click the action below to insert an indented line before the port line:

```editor:insert-lines-before-match
file: ~/exercises/match-before-test.txt
match: "port: 5432"
text: |2
    name: mydb
```
