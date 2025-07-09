# How to upgrade Splunk Operator and Splunk Enterprise Deployments

To upgrade the Splunk Operator for Kubernetes, you will overwrite the prior Operator release with the latest version. Once the lastest version of `splunk-operator-namespace.yaml` ([see below](#upgrading-splunk-operator-and-splunk-operator-deployment)) is applied the CRD's are updated and Operator deployment is updated with newer version of Splunk Operator image. Any new spec defined by the operator will be applied to the pods managed by Splunk Operator for Kubernetes.
​
A Splunk Operator for Kubernetes upgrade might include support for a later version of the Splunk Enterprise Docker image. In that scenario, after the Splunk Operator completes its upgrade, the pods managed by Splunk Operator for Kubernetes will be restarted using the latest Splunk Enterprise Docker image.
​
* Note: The Splunk Operator does not provide a way to downgrade to a previous release.
​
## Prerequisites
​
* Before you upgrade, review the Splunk Operator [change log](https://github.com/splunk/splunk-operator/releases) page for information on changes made in the latest release. The Splunk Enterprise Docker image compatibility is noted in each release version.
​
* If the Splunk Enterprise Docker image changes, review the Splunk Enterprise [Upgrade Readme](https://docs.splunk.com/Documentation/Splunk/latest/Installation/AboutupgradingREADTHISFIRST) page before upgrading.
​
* For general information about Splunk Enterprise compatibility and the upgrade process, see [How to upgrade Splunk Enterprise](https://docs.splunk.com/Documentation/Splunk/latest/Installation/HowtoupgradeSplunk).
​
* If you use forwarders, verify the Splunk Enterprise version compatibility with the forwarders in the [Compatibility between forwarders and Splunk Enterprise indexers](https://docs.splunk.com/Documentation/Forwarder/latest/Forwarder/Compatibilitybetweenforwardersandindexers) documentation.
​


# Splunk Operator Upgrade

## Steps to upgrade from version greater than 1.0.5 to latest

1. Download the latest Splunk Operator installation yaml file.
​
```
wget -O splunk-operator-namespace.yaml https://github.com/splunk/splunk-operator/releases/download/2.7.1/splunk-operator-namespace.yaml
```
​
2. (Optional) Review the file and update it with your specific customizations used during your install.

a. **NOTE** The `SPLUNK_GENERAL_TERMS` environment variable is set to an empty string by default. This will need to be manually updated to the value of "--accept-sgt-current-at-splunk-com". If you do not update this in the yaml file, you can update the splunk-operator-controller-manager deployment directly. See [Configuring Operator to Accept the Splunk General Terms](#configuring-operator-to-accept-the-splunk-general-terms).

​
3. Upgrade the Splunk Operator.​
```
kubectl apply -f splunk-operator-namespace.yaml --server-side
```
​
After applying the yaml, a new operator pod will be created and the existing operator pod will be terminated. Example:
​
```
kubectl get pods
NAME                                                  READY   STATUS    RESTARTS   AGE
splunk-operator-controller-manager-75f5d4d85b-8pshn   1/1     Running   0          5s
```
​
If a Splunk Operator release changes the custom resource (CRD) API version, the administrator is responsible for updating their Custom Resource specification to reference the latest CRD API version.

### Upgrading Splunk Enterprise Docker Image with the Operator Upgrade

Splunk Operator follows the upgrade path steps mentioned in [Splunk documentation](https://docs.splunk.com/Documentation/Splunk/9.4.0/Installation/HowtoupgradeSplunk). If a Splunk Operator release includes an updated Splunk Enterprise Docker image, the operator upgrade will also initiate pod restart using the latest Splunk Enterprise Docker image. To follow the best practices described under the [General Process to Upgrade the Splunk Enterprise], a recommeded upgrade path is followed while initiating pod restarts of different Splunk Instances. At each step, if a particular CR instance exists, a certain flow is imposed to ensure that each instance is updated in the correct order. After an instance is upgraded, the Operator verifies if the upgrade was successful and all the components are working as expected. If any unexpected behaviour is detected, the process is terminated.

If a Splunk Operator release changes the custom resource (CRD) API version, the administrator is responsible for updating their Custom Resource specification to reference the latest CRD API version.

## Steps to Upgrade from 1.0.5 or older version to latest

Upgrading the Splunk Operator from 1.0.5 or older version to latest is a new installation rather than an upgrade from current operator installation. The older Splunk Operator must be cleaned up before installing the new version. You should upgrade operator to 1.1.0 first and then use [normal upgrade process from 1.1.0 to latest](#Steps-to-upgrade-from-version-greater-than-1.0.5-to-latest).

Script [upgrade-to-1.1.0.sh](https://github.com/splunk/splunk-operator/releases/download/1.1.0/upgrade-to-1.1.0.sh) helps you to do the cleanup, and install 1.1.0 Splunk operator. The script expects the current namespace where the operator is installed and the path to the latest operator deployment manifest file. The script performs the following steps

* Backup of all the operator resources within the namespace like
** service-account, deployment, role, role-binding, cluster-role, cluster-role-binding
* Deletes all the old Splunk Operator resources and deployment
* Installs the operator in Splunk-operator namespace.
### Upgrading Splunk Operator and Splunk Operator Deployment

1. Download the upgrade script.

```
wget -O operator-upgarde.sh https://github.com/splunk/splunk-operator/releases/download/1.1.0/upgrade-to-1.1.0.sh
```

2. Download the 1.1.0 Splunk Operator installation yaml file.

```
wget -O splunk-operator-install.yaml https://github.com/splunk/splunk-operator/releases/download/1.1.0/splunk-operator-install.yaml
```

3. (Optional) Review the file and update it with your specific customizations used during your install.

4. Upgrade the Splunk Operator.

Set KUBECONFIG and run the already downloaded `operator-upgrade.sh` script with the following mandatory arguments

* `current_namespace` current namespace where operator is installed
* `manifest_file`: path to 1.1.0 Splunk Operator manifest file

### Example

```bash
>upgrade-to-1.1.0.sh --current_namespace=splunk-operator --manifest_file=splunk-operator-install.yaml
```

Note: This script can be run from `Mac` or `Linux` system. To run this script on `Windows`, use `cygwin`.

## Configuring Operator to Accept the Splunk General Terms

Starting with Operator version 3.0.0, which includes support for Splunk Enterprise version 10.x, an additional Docker-Splunk specific parameter is required to start containers. This is a breaking change, and user action is required. 

For Splunk Enterprise version 10.x and later image versions, license acceptance requires an additional `SPLUNK_GENERAL_TERMS=--accept-sgt-current-at-splunk-com` argument. This indicates that users have read and accepted the current/latest version of the Splunk General Terms, available [here](https://www.splunk.com/en_us/legal/splunk-general-terms.html) as may be updated to from time to time. Unless you have jointly executed with Splunk a negotiated version of these General Terms that explicitly supersedes this agreement, by accessing or using the Splunk Enterprise software, you are agreeing to the Splunk General Terms posted at the time of your access and use, and acknowledge its applicability to this software. Please read and make sure you agree to the Splunk General Terms before you access or use this software. Only after doing so should you include the `SPLUNK_GENERAL_TERMS` environment variable with the value of `--accept-sgt-current-at-splunk-com` to indicate your acceptance of the current/latest Splunk General Terms and launch the Splunk Enterprise software.

By default, the SPLUNK_GENERAL_TERMS environment variable will be set to an empty string. You must either manually update it to have the required additional value `--accept-sgt-current-at-splunk-com` in the splunk-operator-controller-manager deployment, pass the `SPLUNK_GENERAL_TERMS` parameter with the required value to the `make deploy` command, or update the value in the Splunk Operator installation file from the release on GitHub.

If you use the below example and the additional `--accept-sgt-current-at-splunk-com` flag, you are indicating that you have read and accepted the current/latest version of the Splunk General Terms, as may be updated from time to time, and acknowledge its applicability to this software - as noted above.

```yaml
...
        env:
        - name: WATCH_NAMESPACE
          value: "splunk-operator"
        - name: RELATED_IMAGE_SPLUNK_ENTERPRISE
          value: splunk/splunk:9.4.0
        - name: OPERATOR_NAME
          value: splunk-operator
        - name: SPLUNK_GENERAL_TERMS
          value: "--accept-sgt-current-at-splunk-com"
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
...
```

## Configuring Operator to watch specific namespace

If Splunk Operator is installed clusterwide then
Edit `deployment` `splunk-operator-controller-manager-<podid>` in `splunk-operator` namespace, set `WATCH_NAMESPACE` field to the namespace that needs to be monitored by Splunk Operator

```yaml
...
        env:
        - name: WATCH_NAMESPACE
          value: "splunk-operator"
        - name: RELATED_IMAGE_SPLUNK_ENTERPRISE
          value: splunk/splunk:9.4.0
        - name: OPERATOR_NAME
          value: splunk-operator
        - name: SPLUNK_GENERAL_TERMS
          value: ""
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
...
```
​
If a Splunk Operator release includes an updated Splunk Enterprise Docker image, the operator upgrade will also initiate pod restart using the latest Splunk Enterprise Docker image.

## Verify Upgrade is Successful
​
To verify the Splunk Operator has been upgraded to the release image in `splunk-operator-install.yaml`,  you can check the version of the operator image in the deployment spec and subsequently the image in Pod spec of the newly deployed operator pod.

Example:

```bash
kubectl get deployment splunk-operator -o yaml | grep -i image
image: docker.io/splunk/splunk-operator:<desired_operator_version>
imagePullPolicy: IfNotPresent
```

```bash
kubectl get pod <splunk_operator_pod> -o yaml | grep -i image
image: docker.io/splunk/splunk-operator:<desired_operator_version>
imagePullPolicy: IfNotPresent
```
​
To verify that a new Splunk Enterprise Docker image was applied to a pod, you can check the version of the image. Example:
​
```bash
kubectl get pods splunk-<crname>-monitoring-console-0 -o yaml | grep -i image
image: splunk/splunk:9.4.0
imagePullPolicy: IfNotPresent
```
## Splunk Enterprise Cluster upgrade

The Splunk Operator mostly adheres to the upgrade path steps delineated in the Splunk documentation.  All pods of the custom resources are deleted and redeployed sequentially. In cases where multi-zone Indexer clusters are utilized, they undergo redeployment zone by zone. Each pod upgrade is meticulously verified to ensure a successful process, with thorough checks conducted to confirm that everything is functioning as expected. If there are multiple pods per Custom Resource, the pods are terminated and re-deployed in a descending order with the highest numbered pod going first.

This is an example of the process followed by the Splunk Operator if the operator version is upgraded and a later Splunk Enterprise Docker image is available. Pod termination and redeployment occur in the below mentioned order based on the recommended upgrade path:

1. Splunk Operator deployment pod
2. Standalone
3. License manager
4. ClusterManager
5. Search Head cluster
6. Indexer Cluster
7. Monitoring Console

Note: The order above assumes that the custom resources are linked via references. If there are Custom resources without references they will be deleted/redeployed indepedentlty of the order.
