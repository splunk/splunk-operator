#!/usr/bin/env bash
set -euo pipefail

region="${AWS_REGION:-us-west-2}"
repo="${1:-}"

if [[ -z "${repo}" ]]; then
  echo "usage: $0 <repo-name>"
  echo "example: $0 vivek/splunk-operator"
  exit 2
fi

if ! command -v aws >/dev/null 2>&1; then
  echo "aws CLI not found"
  exit 1
fi

if aws ecr describe-repositories --region "${region}" --repository-names "${repo}" >/dev/null 2>&1; then
  echo "exists: ${repo}"
  exit 0
fi

aws ecr create-repository --region "${region}" --repository-name "${repo}" >/dev/null
echo "created: ${repo}"

