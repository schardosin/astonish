---
title: File & Search Tools
description: Read, write, edit files and search code
---

The File & Search category includes 9 tools for reading, writing, and editing files, searching code, and working with structured data.

## read_file

Read the contents of a file.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | Yes | The path to the file to read |

## write_file

Write content to a file. Intelligently extracts stdout from `shell_command` output when used as input.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `file_path` | string | Yes | File path to write to |
| `content` | string | Yes | Content to write |

## edit_file

Edit a file by finding and replacing text. Supports exact match or regex patterns.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | Yes | Absolute path to the file |
| `old_string` | string | Yes | Text to find (or regex pattern) |
| `new_string` | string | Yes | Replacement text |
| `regex` | bool | No | Treat `old_string` as regex (default: false) |
| `replace_all` | bool | No | Replace all occurrences (default: false) |

## read_pdf

Extract text from a PDF file or URL. Returns plain text with page markers.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | Yes | Local file path or HTTP/HTTPS URL |
| `max_pages` | int | No | Max pages to extract (default: all) |
| `max_chars` | int | No | Max characters to return (default: 100000) |

## file_tree

Get a structured directory tree view as JSON.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | Yes | Root directory to scan |
| `max_depth` | int | No | Max depth to traverse (default: 3) |

## find_files

Find files by name pattern using glob matching.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `pattern` | string | Yes | Glob pattern (e.g., `*.go`, `test_*.py`) |
| `search_path` | string | No | Directory to search from |
| `max_results` | int | No | Max results (default: 50) |

## grep_search

Search for text patterns in files. Uses ripgrep when available for fast results.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `pattern` | string | Yes | Search pattern (literal string) |
| `search_path` | string | No | Directory or file to search |
| `include_globs` | string[] | No | File patterns to include |
| `case_sensitive` | bool | No | Case-sensitive search (default: false) |
| `max_results` | int | No | Max results (default: 50) |

## git_diff_add_line_numbers

Parse a diff or patch and add line numbers to each change line. Useful for referencing specific lines in code reviews.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `diff_content` | string | Yes | The diff or patch content |

## filter_json

Filter JSON data to include only specified fields. Supports dot notation for nested fields (e.g., `user.name`, `items.price`).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `json_data` | string | Yes | JSON string to filter |
| `fields_to_extract` | string[] | Yes | Fields to extract (dot notation for nested) |
