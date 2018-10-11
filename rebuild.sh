#!/bin/bash

kubectl delete splunkinstances.splunk-instance.splunk.com --all
kubectl delete deploy splunk-operator
operator-sdk build repo.splunk.com/tmalik/test
docker push repo.splunk.com/tmalik/test
kubectl create -f deploy/operator.yaml