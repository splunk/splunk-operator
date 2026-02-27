#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: verify_repo.sh [options]

Options:
  --bundle    Verify bundle/helm outputs (runs scripts/verify_bundle.sh)
  --tests     Run unit tests (make test)
  --fmt       Check gofmt formatting (no changes)
  --vet       Run go vet ./...
  --no-fmt    Skip gofmt check
  --no-vet    Skip go vet
  --fast      Only verify CRD/RBAC outputs (skip fmt/vet/tests/bundle)
  --all       Run bundle, tests, fmt, and vet
  -h, --help  Show this help

Default behavior runs CRD/RBAC verification plus fmt and vet.
USAGE
}

bundle=false
tests=false
fmt=true
vet=true

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bundle)
      bundle=true
      shift
      ;;
    --tests)
      tests=true
      shift
      ;;
    --fmt)
      fmt=true
      shift
      ;;
    --vet)
      vet=true
      shift
      ;;
    --no-fmt)
      fmt=false
      shift
      ;;
    --no-vet)
      vet=false
      shift
      ;;
    --fast)
      bundle=false
      tests=false
      fmt=false
      vet=false
      shift
      ;;
    --all)
      bundle=true
      tests=true
      fmt=true
      vet=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${script_dir}" rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "${repo_root}" ]]; then
  echo "Unable to locate repo root. Run from inside the git repo." >&2
  exit 1
fi

cd "${repo_root}"

printf "Running CRD/RBAC verification...\n"
./scripts/verify_crd.sh

if [[ "${bundle}" == "true" ]]; then
  printf "Running bundle verification...\n"
  ./scripts/verify_bundle.sh
fi

if [[ "${fmt}" == "true" ]]; then
  printf "Checking gofmt formatting...\n"
  if ! command -v gofmt >/dev/null 2>&1; then
    echo "gofmt not found in PATH." >&2
    exit 1
  fi
  unformatted=$(gofmt -l $(git ls-files '*.go'))
  if [[ -n "${unformatted}" ]]; then
    echo "gofmt needed for the following files:" >&2
    echo "${unformatted}" >&2
    exit 1
  fi
fi

if [[ "${vet}" == "true" ]]; then
  printf "Running go vet...\n"
  go vet ./...
fi

if [[ "${tests}" == "true" ]]; then
  printf "Running unit tests...\n"
  make test
fi

echo "verify_repo.sh completed successfully."
