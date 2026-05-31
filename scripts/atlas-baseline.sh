#!/usr/bin/env bash
# scripts/atlas-baseline.sh — Initialize Atlas migration state for existing migrations.
#
# This script creates atlas.sum files in each migration directory so that
# Atlas knows the current migration state. Run this ONCE after setting up
# the Atlas project, then commit the generated atlas.sum files.
#
# Prerequisites:
#   - atlas CLI installed (curl -sSf https://atlasgo.sh | sh)
#   - Docker running (for postgres dev database)
#
# Usage:
#   ./scripts/atlas-baseline.sh
#
set -euo pipefail

ATLAS="${ATLAS:-atlas}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "Baselining Atlas migration state..."
echo "Root: $ROOT"
echo ""

# Preprocess {{schema}} → public for team/personal PG
echo "Preprocessing schema placeholders..."
sed 's/{{schema}}/public/g' "$ROOT/schema/team/schema.pg.sql" > "$ROOT/schema/team/schema.pg.resolved.sql"
sed 's/{{schema}}/public/g' "$ROOT/schema/personal/schema.pg.sql" > "$ROOT/schema/personal/schema.pg.resolved.sql"

cd "$ROOT"

# For each environment, compute the atlas.sum (hash file) for existing migrations.
# `atlas migrate hash` recomputes the integrity file without generating new migrations.
ENVS="platform_pg platform_lite org_pg org_lite team_pg team_lite personal_pg personal_lite"

for ENV in $ENVS; do
    echo "  Hashing: $ENV"
    $ATLAS migrate hash --env "$ENV"
done

echo ""
echo "Done! atlas.sum files created in each migration directory."
echo ""
echo "Next steps:"
echo "  1. Review the generated atlas.sum files"
echo "  2. git add pkg/store/*/migrations/*/atlas.sum"
echo "  3. git commit -m 'chore: baseline atlas migration checksums'"
