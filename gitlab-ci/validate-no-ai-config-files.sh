#!/usr/bin/env bash
#
# Fail if any AI coding agent / MCP config files are tracked by git.
# These files can contain API tokens and credentials and must never
# be committed.
set -euo pipefail

# Patterns that should never appear in the git tree.
BLOCKED_PATTERNS=(
  "CLAUDE.local.md"
  "AGENTS.override.md"
  "mcp.json"
  ".claude/"
  ".cursor/"
  ".cursorignore"
  ".cursorrules"
  ".github/copilot-instructions.md"
  ".codeium/"
  ".windsurf/"
  ".windsurfrules"
  ".clinerules"
  ".aider"
  ".codex/"
  ".secrets"
  ".env.local"
  ".env.*.local"
)

found=0
for pattern in "${BLOCKED_PATTERNS[@]}"; do
  # git ls-files lists tracked files; we check for exact name or path prefix
  matches=$(git ls-files -- "*${pattern}*" 2>/dev/null || true)
  if [ -n "$matches" ]; then
    printf 'ERROR: Tracked file(s) match blocked AI/MCP config pattern "%s":\n' "$pattern"
    printf '%s\n' "$matches"
    found=1
  fi
done

if [ "$found" -ne 0 ]; then
  printf '\nAI coding agent and MCP config files must not be committed.\n'
  printf 'Add them to .gitignore and remove from tracking with:\n'
  printf '  git rm --cached <file>\n'
  exit 1
fi

printf 'OK: no AI coding agent or MCP config files are tracked.\n'
