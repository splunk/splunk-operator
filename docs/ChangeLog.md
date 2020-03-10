# Splunk Operator for Kubernetes Change Log

## 0.1.0 Alpha (2020-??-??)

* This release depends upon changes made concurrently in the Splunk
  Enterprise container images. You must use the latest splunk/splunk:edge
  nightly image with it, or alternatively any release version 8.0.3 or later.

* The API has been updated to v1alpha2, and involves the replacement of
  the SplunkEnterprise custom resource with 5 new custom resources:
  Spark, LicenseMaster, Standalone, SearchHeadCluster and IndexerCluster.
  Please read the revised [Custom Resources](CustomResources.md) and
  [Examples](Examples.md) documentation for details on all the changes. This
  is a major update and is not backwards-compatible. You will have to
  completely remove any older versions, and any resources managed by the
  operator, before upgrading to this release.

* A new serviceTemplate spec parameter has been added for all Splunk Enterprise
  custom resources. This may be used to define a template the operator uses for
  the creation of (non headless) services.

* Splunk Enterprise clusters may now be created without having to provide a
  license file via the licenseURL parameter. When no license is provided,
  a default trial license will now be used.

* Annotations and labels from the managed custom resources are now appended
  to any corresponding Pod and Service objects that the operator creates.

* A unique pass4SymmKey secret will now be randomly generated, to resolve
  cluster master warnings about using the default value.

* Integrated with CircleCI and Coverall for CICD and code coverage, and
  added a bunch of unit tests to bring coverage up to 93%.

## 0.0.6 Alpha (2019-12-12)

* The operator now injects a podAntiAffinity rule to try to ensure
  that no two pods of the same type are scheduled on the same host

* Ingest updates: HEC is now always enabled by default; added S2S port
  9997 to indexer pod specs, added splunk-indexer-service, doc updates

* Fixed bugs and updated docs, YAML deployment files, roles and bindings
  to better accomodate using a single instance of the Splunk operator to
  manage SplunkEnterprise stacks across an entire cluster (cluster scope).

* Fixed deadlock condition between deployer and search heads when
  apps_location is specified (note this this also requires a patch
  in splunk-ansible https://github.com/splunk/splunk-ansible/pull/312

* Fixed issues with randomly generated secrets sometimes containing
  characters that broken ansible's YAML parsing, causing failures
  to provision new stacks. All randomly-generated secrets now use
  only alpha-numeric characters. Exception is hec_token which now
  uses a UUID like format.

* DFS spark worker pool now uses a Deployment instead of StatefulSet

* Added annotations to get istio to not intercept certain ports

* Removed unused port 60000 from splunk-operator deployment spec

* Pod template change now log the name of the thing that was updated

## 0.0.5 Alpha (2019-10-31)

* Added port 8088 to expose on indexers, and only exposting DFC ports on search heads
* Bug fix: The spark-master deployment was always updated during reconciliation

## 0.0.4 Alpha (2019-10-22)

* Updates to SplunkEnterprise objects are now handled, enabling deployments to be upgraded and modified after creation
* Added liveness and readiness probes to all Splunk Enterprise and Spark pods
* All Splunk Enterprise containers now run using the unprivileged `splunk` user and group
* The minimum required version of Splunk Enterprise is now 8.0
* The `splunk-operator` container now uses Red Hat's [Universal Base Image](https://developers.redhat.com/products/rhel/ubi/) version 8

## 0.0.3 Alpha (2019-08-14)

* Switched single instances, deployer, cluster master and license master
from using Deployment to StatefulSet

## 0.0.2 & 0.0.1

* Internal only releases
