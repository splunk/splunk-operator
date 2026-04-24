#!/bin/sh
set -eu

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

mkdir -p ci-output
python3 "${CI_PROJECT_DIR}/gitlab-ci/github-intake-backfill.py"
