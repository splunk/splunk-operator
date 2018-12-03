#!/bin/bash

kubectl delete splunkenterprises.enterprise.splunk.com --all
kubectl delete deploy splunk-operator
operator-sdk build repo.splunk.com/splunk/products/splunk-operator
docker push repo.splunk.com/splunk/products/splunk-operator
kubectl create -f deploy/operator.yaml