# Splunk Operator for Kubernetes Change Log

## 2.8.1 (2025-07-16)

* This is the 2.8.1 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/main/docs/README.md#prerequisites-for-the-splunk-operator)

* Update `olm.maxOpenShiftVersion` version in operator's bundle

### Supported Splunk Version

>| Splunk Version|
>| --- |
>| 9.4.3 |

### Supported Kubernetes Version

>| Kubernetes Version|
>| --- |
>| 1.27+ |

## 2.8.0 (2025-04-25)

* This is the 2.8.0 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/main/docs/README.md#prerequisites-for-the-splunk-operator)

* CSPL-3586 - Certify FIPS 140-3 Compliance

* CSPL-3624 - Update Helm Charts for 2.8.0 Support

* CSPL-3673 - Github Issue #1157: Failed to install the operator - CRD metadata.annotations too long

* CSPL-3590 - Vulnerability Fix: github.com/golang-jwt/jwt/v5, golang.org/x/net

* CSPL-3674 - Github Readme Updates

* CSPL-3463 - Push RC images to private ECR repository

* CSPL-3499 - Fix Segmentation Fault in Splunk Operator Due to App Volume Name Change

* CSPL-3547 - Support Appframework migration to manager-app

* CSPL-3529 - Disable ARM AL2023 tests for develop builds

* CSPL-3534 - Fix policy string processing in the cluster script

* CSPL-3527 - Create Pull Request Template for Github Repo

* CSPL-3426 - App Framework: Allow .tar.gz file extension

* CSPL-3518 - Task for Github Issue  #1211 Splunk Operator: Why force ClusterIP and ignore k8s default field in service template

* CSPL-3460 - Container Scan: Upgrade google.golang.org/grpc from v1.49.0 to 1.58.3, 1.57.1, 1.56.3 for splunk/splunk-operator

* CSPL-3435 - Support with Bias language migration

* CSPL-3434 - Update base image to ubi-minimal

* CSPL-3269 - Github Issue #1309: App Framework: 2.5.2 can't install packages from splunkbase, placed in s3 bucket

* CSPL-3298 - Fix integration test workflow pull splunk operator image

### Supported Splunk Version

>| Splunk Version|
>| --- |
>| 9.4.1 |

### Supported Kubernetes Version

>| Kubernetes Version|
>| --- |
>| 1.31+ |

## 2.7.1 (2025-01-28)

* This is the 2.7.1 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/main/docs/README.md#prerequisites-for-the-splunk-operator)

* CSPL-3064 - Support for Distroless Image Creation in Splunk Operator for Kubernetes

* CSPL-2966 - Manual App Updates per Custom Resource (CR) in Splunk Operator

* CSPL-3156 - Add kubectl-splunk Plugin for Executing Splunk Commands within Kubernetes Pods

* CSPL-3256 - Support to configure deployer spec in SHC CRD

* CSPL-3253 - Change default storageClassName value in PVC

* CSPL-2920 - Add support for ARM64 pipelines

### Supported Splunk Version

>| Splunk Version|
>| --- |
>| 9.4.0 |

### Supported Kubernetes Version

>| Kubernetes Version|
>| --- |
>| 1.31+ |

## 2.7.0 (2024-12-04)

* This is the 2.7.0 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/main/docs/README.md#prerequisites-for-the-splunk-operator)

* CPSL-2699 - Add Azure and GCP SDK support for AppFramework

* CSPL-3149 - Address openshift documentation for 4.14 or later

* CSPL-3060 - Avoid upgrade path during deployment creation

* CSPL-3058 - Build docker image using SHA instead of tag for redhat UBI

* CSPL-3063 - Add an option to test SVA testing only via helm

* CSPL-2496  - Cleanup AWS resources during cluster cleanup

* CSPL-2887 - Update eksctl, K8s versions, default storage class

* CSPL-2756 - Add documentation for PDB with SOK deployments

* CSPL-2820 - Add Support for Configuring Custom Cluster Domain in Helm Chart

### Supported Splunk Version

>| Splunk Version|
>| --- |
>| 9.3.2 |

### Supported Kubernetes Version

>| Kubernetes Version|
>| --- |
>| 1.31+ |

## 2.6.1 (2024-08-27)

* This is the 2.6.1 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/main/docs/README.md#prerequisites-for-the-splunk-operator)

* CSPL-2655 - Add default.meta in the telemetry addressing vulnerability

* CSPL-2652 - Add TLS config to minio client

* CSPL-2721 - Update MC upgrade path

* CSPL-2747 - Upgrade go(to 1.23.0), go-restful versions to address vulnerabilities

### Supported Splunk Version
>| Splunk Version|
>| --- |
>| 9.3.0 |

### Supported Kubernetes Version
>| Kubernetes Version|
>| --- |
>| 1.25+ |

## 2.6.0 (2024-07-25)

* This is the 2.6.0 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/main/docs/README.md#prerequisites-for-the-splunk-operator)

* CSPL-2721 - Modified Splunk Enterprise deployments upgrade path to speed up the process, updated ubi base image.

* CSPL-2662 - Added troubleshooting documentation.

* CSPL-2626 - Fixed bug in phase initialization, new CR status field, updated aws-sdk-go.

* CSPL-2710: Replace clair with trivy for security scanning.

* CSPL_2598: Fix app package name extraction logic.

* CSPL_2574: Fix bug in appFramework validation.

* CSPL_2532: Change fsGroupChangePolicy to `OnRootMismatch` for Splunk Enterprise and Splunk operator.

### Supported Splunk Version
>| Splunk Version|
>| --- |
>| 9.2.2 |

### Supported Kubernetes Version
>| Kubernetes Version|
>| --- |
>| 1.25+ |

## 2.5.2 (2024-02-28)

CSPL-2535 security context for init container not set (#1290)

### Supported Splunk Version
>| Splunk Version|
>| --- |
>| 9.0.8 |
>| 9.1.3 |

### Supported Kubernetes Version
>| Kubernetes Version|
>| --- |
>| 1.25+ |

## 2.5.1 (2024-02-20)

CSPL-2532: fix for leader election lost issue (#1281)

### Supported Splunk Version
>| Splunk Version|
>| --- |
>| 9.0.8 |
>| 9.1.3 |

### Supported Kubernetes Version
>| Kubernetes Version|
>| --- |
>| 1.25+ |


## 2.5.0 (2024-02-05)

CSPL-2155: Support for Level-2 Upgrade Strategy in Splunk Operator

CSPL-2505: Pod Security standard set to restricted mode

### Supported Splunk Version
>| Splunk Version|
>| --- |
>| 9.0.8 |
>| 9.1.3 |

### Supported Kubernetes Version
>| Kubernetes Version|
>| --- |
>| 1.25+ |


## 2.4.0 (2023-10-13)

* This is the 2.4.0 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/main/docs/README.md#prerequisites-for-the-splunk-operator)

* CSPL-2481: Fixed splunk operator usage examples

* CSPL-2399: Fixed issue where pods are being stucked in Terminating state

* CSPL-2482: Documents updates related to splunk-ansible

* CSPL-2483: Support for additional labels to operator pod in splunk operator helm chart

* CSPL-2480: Support helm flow control to restrict volume values added to deployment template when using PVC

* CSPL-2443: Support private bucket support in Splunk Operator when IRSA used

* CSPL-2457: Upgraded Golang to 1.21.1

* CSPL-2459: Upgraded operator-sdk to 1.31.0

* CSPL-2409: Splunk Operator for Kubernetes reorders environment variables

* Git-1223: Fixed an issue in Storage Class for splunk operator helm chart

* Git-1151: Fixed indexer deletion in upgrade scenario

* Fixed auth proxy service selector labels

* Host multiple splunk operator helm charts on github.io


### Supported Splunk Version
>| Splunk Version|
>| --- |
>| 9.0.6 |
>| 9.1.1 |

### Supported Kubernetes Version
>| Kubernetes Version|
>| --- |
>| 1.25+ |



## 2.3.0 (2023-06-28)

* This is the 2.3.0 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/main/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:9.0.5 image with it

* CSPL-2341: Enable private access links as endpoints for AWS client sessions

* CSPL-2331: Documentation changes for AWS IRSA support

* CSPL-2250: Improve code coverage

* CSPL-2408: Support labels in Splunk Operator helm chart

## 2.2.1 (2023-03-13)

* This is the 2.2.1 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/main/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:9.0.3-a2 image with it

* CSPL-2301: Fixed security vulnerabilities

* Fixed multiple helm chart issues

## 2.2.0 (2023-01-25)

* This is the 2.2.0 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/main/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:9.0.3-a2 image with it

* CSPL-1493 - Added support to automate the installation of the [Enterprise Security (ES) App](https://splunkbase.splunk.com/app/263) via appFramework phase 3

* Fixed multiple helm chart issues

## 2.1.1 (2022-12-07)

* This is the 2.1.1 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/master/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:9.0.2 image with it

* CSPL-2177: Fixed security vulnerability

* CSPL-2171: Fixed namespace specific installation issue in Helm chart and in manifest files

* Fixed Operator Bundle issues

* Fixed some of the documentation issues

## 2.1.0 (2022-11-22)

* This is the 2.1.0 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/master/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:9.0.2 image with it

* CSPL-1480: Azure blob support for Operator App Management Framework

* CSPL-1773: Overhaul Liveness/Readiness Probes

* CSPL-1859: Helm Chart Support for Splunk Operator for Kubernetess

* CSPL-218: Telemetry Support for Operator

* CSPL-1407: Bias Language Support

* Documentation Updates

* Security patches

* CSPL-2088: Fix for KVStore startup failure on a pod reset

* CSPL-2034: Fix for Operator crash when removing app source from the CR spec

* Update required go version to 1.19.2 or later

* Operator SDK upgraded to version 1.25.0. Operator upgrade steps with necessary script updated [here](https://github.com/splunk/splunk-operator/blob/main/docs/SplunkOperatorUpgrade.md)


## 2.0.0 (2022-07-26)

* This is the 2.0.0 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/master/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:9.0.0 image with it

* CSPL-935: Operator App Management Framework Phase 3

* Operator SDK upgraded to version 1.18.1 Operator upgrade steps with necessary script updated [here](https://github.com/splunk/splunk-operator/blob/main/docs/SplunkOperatorUpgrade.md)

* Functional QA automation enhancements

* Documentation Updates

* CSPL-1201: Added validation checks for invalid storage type and provider

* CSPL-1529: Fix a bug where ClusterMaster was not watching for changes in configMap

* CSPL-1604: Update configmap for SHC and CM separately to avoid race condition

* CSPL-1670: Add region as a configurable parameter in volume spec

* CSPL-1729: Detect and update the init container image

* CSPL-1749: ImagePullSecrets config docs along with other common splunk spec parameters

* CSPL-1768: Adding an annotation to define a default container

* CSPL-1769: Change the naming of volumeMounts to adopt setup of init container

* CSPL-1727: Manifest files to differentiate namespace scoped and cluster scoped

## 1.1.0 (2022-04-12)

* This is the 1.1.0 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/master/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:8.2.6 image with it

* Operator SDK upgraded to version 1.15.0. Operator upgrade steps with necessary script updated [here](https://github.com/splunk/splunk-operator/blob/master/docs/SplunkOperatorUpgrade.md)

* Introducing Monitoring Console CRD

* Functional QA automation enhancements

* Documentation Updates

* CSPL-1660: Diagnostic tools added to gather information about Kubernetes environment as well as Splunk diag files

* CSPL-1652: Set `podSecurityContext` `runAsNonRoot` to `true` by default

* CSPL-1649: `SPLUNK_LICENSE_MASTER_URL` duplicated if licenseMasterRef defined in SHC and CM resources

* CSPL-1632: Fix home directory of init-container to allow App framework to be successful using IAM with Service account

* CSPL-1455: Update required go version to 1.17.3

* CSPL-1379 IDXC fails to scale up when MC CR is deployed with pre-existing IDXC

* CSPL-1306: Remove bias language from Functions, Local variables, Paths, URLs

* CSPL-1327: App framework: App installation isn't triggered if one appsource is empty

* fix: For Minio connections trust admin to pick protocol

## 1.0.5 (2021-12-17)
* This is the 1.0.5 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/develop/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:8.2.3.3 image with it

* CSPL-1553: Modify apiVersion in cluster_role.yaml to rbac.authorization.k8s.io/v1 (for compatibility with Kubernetes 1.22+)

## 1.0.4 (2021-12-13)
* This is the 1.0.4 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/develop/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:8.2.3.2 image with it

## 1.0.3 (2021-10-05)
* This is the 1.0.3 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/develop/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:8.2.2 image with it

* CSPL-1230: Remove need for Secret keys in IAM env

* CSPL-1316: Avoid re-entrant code flow for App framework

* CSPL-1302: Bias-language removal Phase 1 [Comments & Docs]

* CSPL-1301: Trigger app install for modified app packages

* CSPL-1283: Fix AWS & minio S3 client code to support App framework on GCS

* CSPL-1271: Fix a bug where standalone with replicas>1 won't come up

* Functional Automation test updates

* Migration of CI/CD from circleCI to GitHub Actions

## 1.0.2 (2021-08-20)
* This is the 1.0.2 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/develop/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:8.2.1-a2 image with it

* CSPL-725 - Operator App Management Framework Phase 2 (Beta Release)

* CSPL-1139: Make the Liveness and Readiness Probe initial delay time configurable

* Documentation updates to include
  * Updated documentation for App Framework
  * Updated documentation for Smartstore examples

* Known Issues
  * CSPL-1250 - [AppFramework] On App install/update to the Search Head Cluster(SHC), the deployer unnecessarily includes the apps which were installed in the previous bundle push along with the new/updated app. This can potentially delay the app install/update process

## 1.0.1 (2021-06-09)
* This is the 1.0.1 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/develop/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:8.2.0 image with it

* Upgraded operator-sdk version from v0.15.1 to v0.18.2

* CSPL-633 - Added new 'extraEnv' parameter in CR spec. This enables customers to pass 'ExtraEnv' variables to the Splunk instance containers

* Documentation updates to include
  * Updated documentation for Multisite example
  * Additional information for using multiple license files
  * Clarify how admins can setup additional Smartstore & Index configuration on top of the existing CR method via Apps

* Functional Test automation increased parallelism

## 1.0.0 GA(2021-04-19)
* This is the GA 1.0.0 release. The Splunk Operator for Kubernetes is a supported platform for deploying Splunk Enterprise with the prerequisites and constraints laid out [here](https://github.com/splunk/splunk-operator/blob/develop/docs/README.md#prerequisites-for-the-splunk-operator)

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:8.1.3 image with it

* CSPL-555 - Use pre-defined system resource values for Montioring Console pod (CPU & Memory) to avoid continuous reset of the MC pod when those values aren't consistent on different CRs

* CSPL-780 - When deleting Search Head Cluster's custom resources, add fix to delete PVCs for both the "search-head" and "deployer" components

* Documentation updates to include
  * System resources & Storage requirements
  * How to Upgrade Splunk Operator
  * Ingress documentation updates with ngingx examples
  * How to configure Indexer cluster to use License Manager

* Nightly build pipeline enhanced to run on EKS Cluster

* Functional Test automation enhancements and refactoring

## 1.0.0-RC(2021-03-22)
* This a release candidate for upcoming GA release.

* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:8.1.3 image with it

* Changed CRD version from v1beta1 to v1, 1.0.0-RC operator version

* CSPL-826 - Created documentation detailing secure Splunk deployments in Kubernetes.

* CSPL-674 - Removed Spark support

* CSPL-624 - Added Splunk Operator upgrade documentation

* Security enhancements

* Test automation enhancements

## 0.2.2 Beta(2021-02-09)
* This release depends upon changes made concurrently in the Splunk Enterprise container images. You should use the splunk/splunk:8.1.2 image with it, or alternatively any release version 8.1.0 or later

* This release updates the CRDs which may require updates to the custom resources being used in existing deployments

* CSPL-526 - Enhanced ingress documentation with guidelines on ingesting data into the K8S cluster using ingress controllers(istio, nginx)

* CSPL-564 - Changed the way licenseMasterRef is configured on the ClusterMaster and IndexerCluster CRDs

* CSPL-609 - Added a shortname stdaln for the Standalone CRD

* CSPL-637 - Updated Splunk port names to conform with Istio ingress controllers convention

* CSPL-660 - Separated storage class specifications for etc and var volumes

* CSPL-663 - Optimize deployment of Splunk apps on SHC using new parameter defaultsUrlApps

* CSPL-694 - Avoid unnecessary pod resets

* CSPL-720 - Added support to configure a custom service account per Splunk Enterprise CRD

* CSPL-721 - Mounted etc and var as emptyDirs volumes on the monitoring console

## 0.2.1 Beta(2020-12-15)
* This release depends upon changes made concurrently in the Splunk Enterprise container images. You must use the latest splunk/splunk:edge nightly image with it, or alternatively any release version 8.1.0 or later

* CSPL-529 - Fixed incorrect deletion of Indexer PVCs upon deletion of ClusterMaster

* CSPL 466 - Fixed infinite reconcile loop of the Operator when an Indexer Cluster is created with peers < SF, RF

* CSPL-532 - Fixed a race condition where changing the idxc.secret on the global secret object could result in an infinite loop of container restarts

* Increased code coverage

* CSPL-534 - Fixed unnecessary pod recycles on scale up/down

* CSPL-592 - Initiate a pod recycle on change of environment variables of containers

* CSPL-658 - Fixed incorrect change of Indexer state from Configured to New in the Monitoring Console

## 0.2.0 Beta (2020-10-15)
* This release depends upon changes made concurrently in the Splunk Enterprise container images. You must use the latest splunk/splunk:edge nightly image with it, or alternatively any release version 8.1.0 or later.

* The API has been updated to v1beta1, with one new Custom Resource Definition added: ClusterMaster. Refer the revised [Custom Resources](CustomResources.md) and [Examples](Examples.md) documentation for details on all the changes. This is a major update and is not backward-compatible. You will have to completely remove any older versions, and any resources managed by the operator, before upgrading to this release.

* Password management has been enhanced to make use of a centralized approach to create & maintain Splunk secrets within a Kubernetes Cluster. Refer [PasswordManagement.md](PasswordManagement.md) for more details in Setup & Usage

* Introduction of SmartStore Index management feature. With this update, SmartStore-enabled Indexes can be configured through Custom resources. For more details, refer to [SmartStore.md](SmartStore.md)

* Added support for deployment of Multi-site Indexer Cluster. This release introduces a new ClusterMaster Custom Resource, thus allowing the Cluster Manager to have it's own resource specifications. Further, the ClusterMaster & IndexerCluster Custom Resources can together be used to configure both Single & Multi-site Indexer clusters. For more details see [Examples.md](Examples.md) & [MultisiteExamples.md](MultisiteExamples.md)

* Feature to automatically add a configured Monitoring Console pod within a namespace. With this release, a Monitoring Console pod is automatically configured & also has the ability to reconfigure itself based on the changes within the namespace. For more details, refer to [Examples.md](Examples.md)

* Introduction of Ginkgo based test framework for CI/CD pipeline. Smoke Test cases added to validate the fundamental use cases related to Splunk custom resources. For more details, refer to [README.md](README.md)

* Feature to enable ephemeral storage support for Splunk volumes

* Add provision to enable custom ports on Splunk containers

## 0.1.0 Alpha (2020-03-20)

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

* Scaling, upgrades and other updates are now more actively managed for the
  SearchHeadCluster and IndexerCluster resources. This helps protect against
  data loss and maximizes availability while changes are being made. You can
  now also use the "kubectl scale" command, and Horizontal Pod Autoscalers
  with all resources (except LicenseMaster, which always uses a single Pod).

* A new serviceTemplate spec parameter has been added for all Splunk Enterprise
  custom resources. This may be used to define a template the operator uses for
  the creation of (non headless) services.

* Splunk Enterprise clusters may now be created without having to provide a
  license file via the licenseURL parameter. When no license is provided,
  a default trial license will now be used.

* Annotations and labels from the managed custom resources are now appended
  to any corresponding Pod and Service objects that the operator creates.

* A unique pass4SymmKey secret will now be randomly generated, to resolve
  cluster manager warnings about using the default value.

* Integrated with CircleCI and Coverall for CICD and code coverage, and
  added a bunch of unit tests to bring coverage up to over 90%.

## 0.0.6 Alpha (2019-12-12)

* The operator now injects a podAntiAffinity rule to try to ensure
  that no two pods of the same type are scheduled on the same host

* Ingest updates: HEC is now always enabled by default; added S2S port
  9997 to indexer pod specs, added splunk-indexer-service, doc updates

* Fixed bugs and updated docs, YAML deployment files, roles and bindings
  to better accomodate using a single instance of the Splunk Operator to
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
* Bug fix: The spark-manager deployment was always updated during reconciliation

## 0.0.4 Alpha (2019-10-22)

* Updates to SplunkEnterprise objects are now handled, enabling deployments to be upgraded and modified after creation
* Added liveness and readiness probes to all Splunk Enterprise and Spark pods
* All Splunk Enterprise containers now run using the unprivileged `splunk` user and group
* The minimum required version of Splunk Enterprise is now 8.0
* The `splunk-operator` container now uses Red Hat's [Universal Base Image](https://developers.redhat.com/products/rhel/ubi/) version 8

## 0.0.3 Alpha (2019-08-14)

* Switched single instances, deployer, cluster manager and license manager
from using Deployment to StatefulSet

## 0.0.2 & 0.0.1

* Internal only releases
