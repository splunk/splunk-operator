#!/bin/bash

scriptdir=$(dirname "$0")
topdir=${scriptdir}/..

source ${scriptdir}/env.sh

PRIVATE_SPLUNK_OPERATOR_IMAGE=$(bash ${scriptdir}/get-private-registry-operator.sh | tail -1)
PRIVATE_SPLUNK_ENTERPRISE_IMAGE=$(bash ${scriptdir}/get-private-registry-enterprise.sh | tail -1)
./deploy-operator.sh ${PRIVATE_SPLUNK_OPERATOR_IMAGE} ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}
./trigger-tests.sh ${PRIVATE_SPLUNK_OPERATOR_IMAGE} ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}