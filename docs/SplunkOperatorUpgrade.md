# How to upgrade Splunk Operator and Splunk Enterprise Deployments
​
To upgrade the Splunk Operator for Kubernetes, you will overwrite the prior operator release with the latest version. Once the lastest version of `splunk-operator-install.yaml` ([see below](#upgrading-splunk-operator-and-splunk-operator-deployment)) is applied the CRD's are updated and operator deployment is updated with newer version of Splunk Operator image. Any new spec defined by the operator will be applied to the pods managed by Splunk Operator for Kubernetes.
​
A Splunk Operator for Kubernetes upgrade might include support for a later version of the Splunk Enterprise Docker image. In that scenario, after the Splunk Operator completes its upgrade, the pods managed by Splunk Operator for Kubernetes will be restarted using the latest Splunk Enterprise Docker image.
​
* Note: The Splunk Operator does not provide a way to downgrade to a previous release.
​
## Prequisites
​
* Before you upgrade, review the Splunk Operator [change log](https://github.com/splunk/splunk-operator/releases) page for information on changes made in the latest release. The Splunk Enterprise Docker image compatibility is noted in each release version.
​
* If the Splunk Enterprise Docker image changes, review the Splunk Enterprise [Upgrade Readme](https://docs.splunk.com/Documentation/Splunk/latest/Installation/AboutupgradingREADTHISFIRST) page before upgrading. 
​
* For general information about Splunk Enterprise compatibility and the upgrade process, see [How to upgrade Splunk Enterprise](https://docs.splunk.com/Documentation/Splunk/latest/Installation/HowtoupgradeSplunk).
​
* If you use forwarders, verify the Splunk Enterprise version compatibility with the forwarders in the [Compatibility between forwarders and Splunk Enterprise indexers](https://docs.splunk.com/Documentation/Forwarder/latest/Forwarder/Compatibilitybetweenforwardersandindexers) documentation.
​
## Upgrading Splunk Operator and Splunk Operator Deployment
​
1. Download the latest Splunk Operator installation yaml file.
​
```
wget -O splunk-operator.yaml https://github.com/splunk/splunk-operator/releases/download/1.0.0-RC/splunk-operator-install.yaml 
```
​
2. (Optional) Review the file and update it with your specific customizations used during your install. 
​
3. Upgrade the Splunk Operator.
​
```
kubectl apply -f splunk-operator.yaml
```
​
After applying the yaml, a new operator pod will be created and the existing operator pod will be terminated. Example:
​
```
kubectl get pods
NAME                               READY   STATUS    RESTARTS   AGE
splunk-operator-75f5d4d85b-8pshn   1/1     Running   0          5s
​
```
​
If a Splunk Operator release changes the custom resource (CRD) API version, the administrator is responsible for updating their Custom Resource specification to reference the latest CRD API version.
​
If a Splunk Operator release includes an updated Splunk Enterprise Docker image, the operator upgrade will also initiate pod restart using the latest Splunk Enterprise Docker image.
​
## Verify Upgrade is Successful
​
To verify the Splunk Operator has been upgraded to the release image in `splunk-operator-install.yaml`,  you can check the version of the operator image in the deployment spec and subsequently the image in Pod spec of the newly deployed operator pod.

Example:

```
kubectl get deployment splunk-operator -o yaml | grep -i image
image: docker.io/splunk/splunk-operator:<desired_operator_version>
imagePullPolicy: IfNotPresent
```

```
kubectl get pod <splunk_operator_pod> -o yaml | grep -i image
image: docker.io/splunk/splunk-operator:<desired_operator_version>
imagePullPolicy: IfNotPresent 
```
​
To verify that a new Splunk Enterprise Docker image was applied to a pod, you can check the version of the image. Example:
​
```
kubectl get pods splunk-default-monitoring-console-0 -o yaml | grep -i image
image: splunk/splunk:8.1.2
imagePullPolicy: IfNotPresent
image: splunk/splunk:8.1.2
```
​
## Splunk Enterprise Cluster upgrade example
This is an example of the process followed by the Splunk Operator if the operator version is upgraded and a later Splunk Enterprise Docker image is available:
​
1. A new Splunk Operator pod will be created, and the existing operator pod will be terminated.
2. Any existing License Master, Search Head, Deployer, ClusterMaster, Standalone pods will be terminated to be redeployed with the upgraded spec.
3. After a ClusterMaster pod is restarted, the Indexer Cluster pods which are connected to it are terminated and redeployed.
4. After all pods in the Indexer cluster and Search head cluster are redeployed, the Monitoring Console pod is terminated and redeployed.
* Note: If there are multiple pods per Custom Resource, the pods are terminated and re-deployed in a descending order with the highest numbered pod going first