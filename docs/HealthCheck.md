# Splunk Operator Health Check with K8 Probes
Splunk Operator supports Startup, Liveness and Readiness Probes (with its own default values) for Splunk Custom Resources. The following probe configurations are allowed to be modified through Custom Resources: 
* initialDelaySeconds
* timeoutSeconds
* periodSeconds
* failureThreshold

Please refer to [Kubernetes documentation](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) for more information on Startup, Liveness and Readiness Probes.
## Default Values for InitialDelaySeconds,TimeoutSeconds, PeriodSeconds and FailureThreshold

| Probe Type | initialDelaySeconds | timeoutSeconds | periodSeconds | failureThreshold | 
| :--- | :--- | :--- | :--- | :--- | 
| Startup Probe | 40 | 30 | 30 | 12 |
| Readiness Probe | 10 | 5 | 5 | 3 | 
| Liveness Probe | 30 | 30 | 30 | 3 | 

These defaults serve for most of the use cases. If any tuning is needed, following is an example on how to modify the defaults.
### Example to configure Probes for Startup, Liveness and Readiness

```
apiVersion: enterprise.splunk.com/v4
kind:  Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  startupProbe:
    initialDelaySeconds: 10
    timeoutSeconds: 5
    periodSeconds: 5
    failureThreshold: 16
  livenessProbe:
    initialDelaySeconds: 300
    timeoutSeconds: 30
    periodSeconds: 30
    failureThreshold: 3
  readinessProbe:
    initialDelaySeconds: 10
    timeoutSeconds: 5
    periodSeconds: 5
    failureThreshold: 3
```