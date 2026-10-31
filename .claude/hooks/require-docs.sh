#!/usr/bin/env bash
# Stop/SubagentStop hook: when non-doc files changed, require CODECHANGELOG.md
# to be updated too (and remind about docs/), before the agent may finish.
set -euo pipefail

input=$(cat)
# Already continuing because of this hook: don't loop.
[ "$(jq -r '.stop_hook_active // false' <<<"$input")" = "true" ] && exit 0

cd "${CLAUDE_PROJECT_DIR:-.}"
changed=$(git status --porcelain --untracked-files=all | cut -c4- | sed 's/.* -> //')
[ -z "$changed" ] && exit 0

code=$(grep -vE '(^docs/|\.md$)' <<<"$changed" || true)
[ -z "$code" ] && exit 0
grep -qx 'CODECHANGELOG.md' <<<"$changed" && exit 0

jq -n --arg files "$(head -15 <<<"$code")" '{
  decision: "block",
  reason: ("Code/config changed without a CODECHANGELOG.md entry. Before finishing: add an entry (WBS items, files changed, actual verification results) and update affected docs (docs/wbs.md, docs/*, README.md, AGENTS.md).\nChanged:\n" + $files)
}'
