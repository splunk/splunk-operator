#!/bin/bash

kubectl delete splunkinstances.splunk-instance.splunk.com --all
kubectl delete deploy splunk-operator
operator-sdk build tmaliksplunk/test
docker push tmaliksplunk/test
kubectl create -f deploy/operator.yaml