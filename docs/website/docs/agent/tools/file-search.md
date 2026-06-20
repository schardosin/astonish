# File & Search Tools

Nine tools for reading, writing, navigating, and searching the filesystem.

## Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `read_file` | Read file contents (full or range) | auto-approve |
| `write_file` | Create or overwrite a file | always-confirm |
| `edit_file` | Apply targeted edits to a file | always-confirm |
| `file_tree` | Display directory structure | auto-approve |
| `grep_search` | Regex search across files | auto-approve |
| `find_files` | Find files by name pattern | auto-approve |
| `file_info` | Get file metadata (size, permissions, dates) | auto-approve |
| `move_file` | Move or rename a file | always-confirm |
| `delete_file` | Remove a file | always-confirm |

## read_file

Reads file content with optional line range:

```
read_file:
  path: "src/main.go"
  start_line: 10
  end_line: 50
```

Supports binary detection (returns metadata instead of content for binary files).

## write_file

Creates or overwrites a file. In Studio, renders a diff preview before confirmation:

```
write_file:
  path: "config.yaml"
  content: |
    name: my-app
    version: 1.0.0
```

## edit_file

Applies surgical edits without rewriting the entire file:

```
edit_file:
  path: "src/handler.go"
  edits:
    - old: "func handler(w http.ResponseWriter)"
      new: "func handler(w http.ResponseWriter, r *http.Request)"
```

## grep_search

Regex-powered search across the filesystem:

```
grep_search:
  pattern: "TODO|FIXME"
  path: "src/"
  include: "*.go"
  max_results: 50
```

## file_tree

Displays directory structure with configurable depth:

```
file_tree:
  path: "."
  depth: 3
  ignore: ["node_modules", ".git", "vendor"]
```

See [Shell & Process Tools](./shell-process.md) for command execution and [Tools Overview](./index.md) for the full tool catalog.
