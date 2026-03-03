#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi

cd "${repo_root}"

python3 - <<'PY'
from pathlib import Path
import re
import sys

required_sections = [
    "## Overview",
    "## Preconditions",
    "## Workflow",
    "## Pass / Fail Criteria",
    "## Output Contract",
]

skills = sorted(Path(".agents/skills").glob("*/SKILL.md"))
if not skills:
    print("skill_lint: no skills found under .agents/skills")
    sys.exit(1)

failed = False
for skill in skills:
    text = skill.read_text(encoding="utf-8")
    local_fail = False

    frontmatter = re.match(r"^---\n(.*?)\n---\n", text, re.DOTALL)
    if not frontmatter:
        print(f"[FAIL] {skill}: missing YAML frontmatter")
        failed = True
        continue

    fm = frontmatter.group(1)
    if not re.search(r"(?m)^name:\s*\S+", fm):
        print(f"[FAIL] {skill}: frontmatter missing 'name'")
        local_fail = True
    if not re.search(r"(?m)^description:\s*.+", fm):
        print(f"[FAIL] {skill}: frontmatter missing 'description'")
        local_fail = True

    body = text[frontmatter.end() :]
    for section in required_sections:
        if section not in body:
            print(f"[FAIL] {skill}: missing section '{section}'")
            local_fail = True

    if local_fail:
        failed = True
    else:
        print(f"[PASS] {skill}")

if failed:
    print("skill_lint: FAILED")
    sys.exit(1)

print(f"skill_lint: passed ({len(skills)} skills)")
PY
