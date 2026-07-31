---
title: Health Checks
parent: Operate & Manage
nav_order: 8
---


# Splunk Operator Health Check with K8 Probes
Splunk Operator supports Startup, Liveness and Readiness Probes (with its own default values) for Splunk Custom Resources. The following probe configurations are allowed to be modified through Custom Resources: 
* initialDelaySeconds
* timeoutSeconds
* periodSeconds
* failureThreshold
* terminationGracePeriodSeconds (startup and liveness only)

Please refer to [Kubernetes documentation](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) for more information on Startup, Liveness and Readiness Probes.
## Default probe values

| Probe Type | initialDelaySeconds | timeoutSeconds | periodSeconds | failureThreshold | terminationGracePeriodSeconds |
| :--- | :--- | :--- | :--- | :--- | :--- |
| Startup Probe | 40 | 30 | 30 | 60 | 660 with `SplunkPodLifecycle`; otherwise Pod value |
| Readiness Probe | 10 | 5 | 5 | 3 | Not supported by Kubernetes |
| Liveness Probe | 30 | 30 | 30 | 3 | 660 with `SplunkPodLifecycle`; otherwise Pod value |

The startup failure budget is approximately 30 minutes. Startup protects
first start and upgrade work; liveness and readiness do not begin until startup
succeeds.

Probe-level termination grace controls a container restart caused by a failed
startup or liveness probe. It is separate from the Pod-level
`spec.terminationGracePeriodSeconds` used when Kubernetes deletes a Pod. When
`SplunkPodLifecycle` supplies the 660-second probe default, the current Splunk
image has 600 seconds for its bounded local shutdown and 60 seconds of kubelet
margin. If `SPLUNK_SHUTDOWN_TIMEOUT_SECONDS` is increased in the image, set the
startup and liveness probe grace to a correspondingly larger value.

The following example shows how to modify the defaults.

### Example to configure Probes for Startup, Liveness and Readiness

```yaml
apiVersion: enterprise.splunk.com/v4
kind:  Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  startupProbe:
    initialDelaySeconds: 40
    timeoutSeconds: 30
    periodSeconds: 30
    failureThreshold: 60
    terminationGracePeriodSeconds: 660
  livenessProbe:
    initialDelaySeconds: 30
    timeoutSeconds: 30
    periodSeconds: 30
    failureThreshold: 3
    terminationGracePeriodSeconds: 660
  readinessProbe:
    initialDelaySeconds: 10
    timeoutSeconds: 5
    periodSeconds: 5
    failureThreshold: 3
```
