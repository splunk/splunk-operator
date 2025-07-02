#!/bin/bash

scriptdir=$(dirname "$0")
topdir=${scriptdir}/..

source ${scriptdir}/env.sh

PRIVATE_SPLUNK_OPERATOR_IMAGE=$(bash ${scriptdir}/get-private-registry-operator.sh | tail -1)
PRIVATE_SPLUNK_ENTERPRISE_IMAGE=$(bash ${scriptdir}/get-private-registry-enterprise.sh | tail -1)
bash ${scriptdir}/deploy-operator.sh ${PRIVATE_SPLUNK_OPERATOR_IMAGE} ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}
bash ${scriptdir}/trigger-tests.sh ${PRIVATE_SPLUNK_OPERATOR_IMAGE} ${PRIVATE_SPLUNK_ENTERPRISE_IMAGE}
