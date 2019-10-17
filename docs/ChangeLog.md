# Splunk Operator for Kubernetes Change Log

## 0.0.4 Alpha (2019-10-22)

* The `splunk/splunk-operator` base image now uses Red Hat's [Universal Base Image](https://developers.redhat.com/products/rhel/ubi/) v8
* All Enterprise containers are now run using the unprivileged `splunk` user and group
* Minimum Splunk Enterprise version required is now 8.0

## 0.0.3 Alpha (2019-08-14)

* Switched single instances, deployer, cluster master and license master
from using Deployment to StatefulSet

## 0.0.2 & 0.0.1

* Internal only releases