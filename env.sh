#!/usr/bin/env bash

export NAMESPACE=test
export HELM_REPO_PATH=../../../../helm-chart
export KUTTL_SPLUNK_OPERATOR_IMAGE=docker.io/tgarg1701/splunk-operator:2.4.0
export KUTTL_SPLUNK_ENTERPRISE_IMAGE=docker.io/splunk/splunk:9.0.3-a2
export KUTTL_SPLUNK_ENTERPRISE_NEW_IMAGE=docker.io/splunk/splunk:9.0.5
export AWS_DEFAULT_REGION=us-west-2
