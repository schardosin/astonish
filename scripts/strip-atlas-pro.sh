#!/usr/bin/env bash
# scripts/strip-atlas-pro.sh — Strip Atlas Pro-only features from PG schema files.
#
# Atlas community edition cannot parse:
#   - CREATE EXTENSION
#   - CREATE [OR REPLACE] FUNCTION ... $$ ... $$ LANGUAGE plpgsql
#   - CREATE TRIGGER / DROP TRIGGER
#   - ALTER TABLE ... ENABLE/DISABLE TRIGGER
#   - ALTER TABLE ... ENABLE ROW LEVEL SECURITY
#   - CREATE POLICY
#
# This script removes those blocks, leaving only tables, indexes, and constraints
# that Atlas community can diff. The full schema files remain the source of truth;
# this is just preprocessing for Atlas.
#
# Usage: scripts/strip-atlas-pro.sh < input.sql > output.sql

set -euo pipefail

IN_FUNCTION=0
IN_POLICY=0
IN_TRIGGER=0
IN_ALTER=0
ALTER_BUF=""

while IFS= read -r line; do
    # Skip CREATE EXTENSION
    if [[ "$line" =~ ^[[:space:]]*CREATE[[:space:]]+EXTENSION ]]; then
        continue
    fi

    # Skip DROP TRIGGER
    if [[ "$line" =~ ^[[:space:]]*DROP[[:space:]]+TRIGGER ]]; then
        continue
    fi

    # Skip ALTER TABLE ... ENABLE ROW LEVEL SECURITY (single line)
    if [[ "$line" =~ ENABLE[[:space:]]+ROW[[:space:]]+LEVEL[[:space:]]+SECURITY ]]; then
        continue
    fi

    # Start of CREATE [OR REPLACE] FUNCTION block
    if [[ "$line" =~ ^[[:space:]]*CREATE[[:space:]]+(OR[[:space:]]+REPLACE[[:space:]]+)?FUNCTION ]]; then
        IN_FUNCTION=1
        continue
    fi

    # End of function block (line contains $$ ... LANGUAGE)
    if [[ $IN_FUNCTION -eq 1 ]]; then
        if [[ "$line" =~ LANGUAGE[[:space:]]+(plpgsql|sql) ]]; then
            IN_FUNCTION=0
        fi
        continue
    fi

    # Start of CREATE TRIGGER block (may span multiple lines)
    if [[ "$line" =~ ^[[:space:]]*CREATE[[:space:]]+TRIGGER ]]; then
        IN_TRIGGER=1
        if [[ "$line" =~ \;[[:space:]]*$ ]]; then
            IN_TRIGGER=0
        fi
        continue
    fi

    # Inside multi-line CREATE TRIGGER
    if [[ $IN_TRIGGER -eq 1 ]]; then
        if [[ "$line" =~ \;[[:space:]]*$ ]]; then
            IN_TRIGGER=0
        fi
        continue
    fi

    # Start of CREATE POLICY block
    if [[ "$line" =~ ^[[:space:]]*CREATE[[:space:]]+POLICY ]]; then
        IN_POLICY=1
        if [[ "$line" =~ \;[[:space:]]*$ ]]; then
            IN_POLICY=0
        fi
        continue
    fi

    # Inside multi-line CREATE POLICY
    if [[ $IN_POLICY -eq 1 ]]; then
        if [[ "$line" =~ \;[[:space:]]*$ ]]; then
            IN_POLICY=0
        fi
        continue
    fi

    # ALTER TABLE — buffer it to check if next line has ENABLE/DISABLE TRIGGER
    if [[ "$line" =~ ^[[:space:]]*ALTER[[:space:]]+TABLE ]]; then
        # Single-line ALTER with TRIGGER keyword — skip entirely
        if [[ "$line" =~ ENABLE[[:space:]]+TRIGGER|DISABLE[[:space:]]+TRIGGER ]]; then
            continue
        fi
        # Single-line ALTER that ends with ; and no trigger — output normally
        if [[ "$line" =~ \;[[:space:]]*$ ]]; then
            echo "$line"
            continue
        fi
        # Multi-line ALTER — buffer and check continuation
        IN_ALTER=1
        ALTER_BUF="$line"
        continue
    fi

    # Inside buffered ALTER TABLE
    if [[ $IN_ALTER -eq 1 ]]; then
        ALTER_BUF="$ALTER_BUF"$'\n'"$line"
        if [[ "$line" =~ \;[[:space:]]*$ ]]; then
            IN_ALTER=0
            # Check if the full ALTER was trigger-related
            if [[ "$ALTER_BUF" =~ ENABLE[[:space:]]+TRIGGER|DISABLE[[:space:]]+TRIGGER ]]; then
                ALTER_BUF=""
                continue
            fi
            echo "$ALTER_BUF"
            ALTER_BUF=""
        fi
        continue
    fi

    echo "$line"
done

# Flush any remaining buffered ALTER (shouldn't happen with valid SQL)
if [[ -n "$ALTER_BUF" ]]; then
    if ! [[ "$ALTER_BUF" =~ ENABLE[[:space:]]+TRIGGER|DISABLE[[:space:]]+TRIGGER ]]; then
        echo "$ALTER_BUF"
    fi
fi
