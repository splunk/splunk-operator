# FAQ - Splunk General Terms Migration


## Motivation

All users deploying Splunk Enterprise 10.x or later image versions instances must explicitly acknowledge the Splunk General Terms (SGT)(https://www.splunk.com/en_us/legal/splunk-general-terms.html).

## What's happening?

Starting with the 3.0.0 release, there is now a mandatory acknowledgment mechanism for the Splunk General Terms (SGT) within the Splunk Operator for Kubernetes. **This is a breaking change, and user action is required.** This involves creating a new `SPLUNK_GENERAL_TERMS` environment variable in the splunk operator deployment, which gets passed to every CRD.

To learn more about the required value for this variable, please see the [README](../README.md) or Splunk Enterprise 10.x image’s README.

## How does this affect existing deployments?

Existing deployments of the Splunk Operator for Kubernetes will not be affected until you upgrade to version 3.0.0 or later, which includes support for Splunk Enterprise version 10.x. There are no plans to support Splunk Enterprise version 10.x in SOK releases before 3.0.0, so users interested in Splunk 10.x need to upgrade the Splunk Operator in order to use this version. Adding the new `SPLUNK_GENERAL_TERMS` environment variable to any existing deployments on versions below 3.0.0 is not necessary.

## How to plan for a migration?

When you are ready to upgrade from version 1.x.x or 2.x.x to 3.0.0 or later, there are a few options you have to set the `SPLUNK_GENERAL_TERMS` to the proper value. By default, the SPLUNK_GENERAL_TERMS environment variable will be set to an empty string.
1. Pass the `SPLUNK_GENERAL_TERMS` parameter with the required value to the `make deploy` command
```
make deploy IMG=docker.io/splunk/splunk-operator:<tag name> SPLUNK_GENERAL_TERMS="[required value]"
```
2. Update the value in the Splunk Operator installation file from the release on GitHub
```yaml
...
        env:
        - name: WATCH_NAMESPACE
          value: ""
        - name: RELATED_IMAGE_SPLUNK_ENTERPRISE
          value: splunk/splunk:9.4.0
        - name: OPERATOR_NAME
          value: splunk-operator
        - name: SPLUNK_GENERAL_TERMS
          value: "[required value]"
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
...
```
3. Set the value in a `helm install` command
```
helm install -f new_values.yaml --set installCRDs=true --set splunkOperator.splunkGeneralTerms="[required value]" <RELEASE_NAME> splunk/splunk-operator -n <RELEASE_NAMESPACE>
```
4. Edit the splunk-operator-controller-manager deployment after it is deployed
```
kubectl edit deployment splunk-operator-controller-manager -n splunk-operator
```

## How can I know that the SGT acceptance is not correctly set?

The splunk operator logs and the specific CRs will show errors if there is an issue with the SGT acknowledgement. The following examples work for any of the Custom Resources provided by the Splunk Operator.

Look in the operator logs to see reconciliation errors:
```
> kubectl logs <splunk-operator-controller-manager pod name> -n splunk-operator
...
2025-04-24T19:26:51.669674377Z	ERROR	Reconciler error	{"controller": "searchheadcluster", "controllerGroup": "enterprise.splunk.com", "controllerKind": "SearchHeadCluster", "SearchHeadCluster": {"name":"shc","namespace":"splunk-operator"}, "namespace": "splunk-operator", "name": "shc", "reconcileID": "e2440955-3766-4b88-8e19-fc2d681763a7", "error": "license not accepted, please adjust SPLUNK_GENERAL_TERMS to indicate you have accepted the current/latest version of the license. See README file for additional information"}
...
```

Getting the specific CRs will also show error messages
```
> kubectl get shc -n splunk-operator
NAME   PHASE   DEPLOYER   DESIRED   READY   AGE   MESSAGE
shc    Error   Error      3         0       22h   license not accepted, please adjust SPLUNK_GENERAL_TERMS to indicate you have accepted the current/latest version of the license. See README file for additional information
```

Once the `SPLUNK_GENERAL_TERMS` environment variable is updated, it will get added to the individual CRs and the error will go away. This might take a few minutes to take effect.