---
title: Tables
---

Markdown tables are created using pipes (`|`) and hyphens (`-`). Column
alignment can be controlled with colons in the separator row.

## Basic Table

The markdown for a basic table is:

~~~markdown
| Name     | Role       | Status   |
|----------|------------|----------|
| Alice    | Developer  | Active   |
| Bob      | Designer   | Active   |
| Charlie  | Manager    | Inactive |
~~~

| Name     | Role       | Status   |
|----------|------------|----------|
| Alice    | Developer  | Active   |
| Bob      | Designer   | Active   |
| Charlie  | Manager    | Inactive |

## Column Alignment

The markdown for a table with column alignment is:

~~~markdown
| Left Aligned | Center Aligned | Right Aligned |
|:-------------|:--------------:|--------------:|
| Left         |    Center      |         Right |
| Text         |    Text        |          Text |
| More         |    More        |          More |
~~~

| Left Aligned | Center Aligned | Right Aligned |
|:-------------|:--------------:|--------------:|
| Left         |    Center      |         Right |
| Text         |    Text        |          Text |
| More         |    More        |          More |

## Table with Formatting

The markdown for a table with inline formatting is:

~~~markdown
| Feature        | Syntax              | Example          |
|----------------|---------------------|------------------|
| Bold           | `**text**`          | **bold**         |
| Italic         | `*text*`            | *italic*         |
| Code           | `` `text` ``        | `code`           |
| Strikethrough  | `~~text~~`          | ~~strikethrough~~|
| Link           | `[text](url)`       | [example](https://www.example.com) |
~~~

| Feature        | Syntax              | Example          |
|----------------|---------------------|------------------|
| Bold           | `**text**`          | **bold**         |
| Italic         | `*text*`            | *italic*         |
| Code           | `` `text` ``        | `code`           |
| Strikethrough  | `~~text~~`          | ~~strikethrough~~|
| Link           | `[text](url)`       | [example](https://www.example.com) |
