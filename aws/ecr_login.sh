#!/usr/bin/env bash
set -euo pipefail

region="${AWS_REGION:-us-west-2}"
account_id="${AWS_ACCOUNT_ID:-667741767953}"
registry="${account_id}.dkr.ecr.${region}.amazonaws.com"

if ! command -v aws >/dev/null 2>&1; then
  echo "aws CLI not found"
  exit 1
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "docker not found"
  exit 1
fi

aws ecr get-login-password --region "${region}" | docker login --username AWS --password-stdin "${registry}"
echo "logged in: ${registry}"

