#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi

cd "${repo_root}"

echo "Checking shell script syntax"
while IFS= read -r file; do
  bash -n "${file}"
done < <(find scripts .agents/skills -type f -name '*.sh' | sort)

if command -v python3 >/dev/null 2>&1; then
  echo "Checking Python script syntax"
  python3 - <<'PY'
from pathlib import Path

path = Path("scripts/generate_testcase.py")
compile(path.read_text(encoding="utf-8"), str(path), "exec")
PY

  echo "Running testcase generator dry-run contract check"
  PYTHONDONTWRITEBYTECODE=1 python3 scripts/generate_testcase.py --spec docs/agent/TESTCASE_SPEC.yaml --dry-run >/dev/null
fi

echo "script_sanity_check: passed."
