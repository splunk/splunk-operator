#!/usr/bin/env bash
set -euo pipefail

echo "Bootstrapping Splunk Operator devcontainer..."
go version
kubectl version --client --output=yaml >/dev/null
skaffold version >/dev/null
operator-sdk version >/dev/null
kind version >/dev/null

echo "Installing repository-local tools..."
make kustomize controller-gen envtest >/dev/null

echo "Devcontainer bootstrap complete."
