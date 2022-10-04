# Splunk Operator Health Check with K8 Probes
Splunk Operator supports Startup, Liveness and Readiness Probes with its own default values. The following probe configurations are allowed to be modified through CR: InitialDelaySeconds,TimeoutSeconds, PeriodSeconds, FailureThreshold.
[Kubernetes documentation](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)

## Startup Probe 
Splunk Opeator implements Startup Probe using script `tools/k8_probes/startupProbe.sh` which is mounted on splunk pods using config map `splunk-default-probe-configmap`. The configurations InitialDelaySeconds,TimeoutSeconds, PeriodSeconds, FailureThreshold can be customized through the CR.

Standalone example to configure startup Probe. 
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
    timeoutSeconds: 10
    periodSeconds: 10
    failureThreshold: 200
```

## Liveness Probe
Splunk Opeator implements Liveness Probe using script `tools/k8_probes/livenessProbe.sh` which is mounted on splunk pods using config map `splunk-default-probe-configmap`. The configurations InitialDelaySeconds,TimeoutSeconds, PeriodSeconds, FailureThreshold can be customized through the CR.

Standalone example to configure liveness Probe. 
```
apiVersion: enterprise.splunk.com/v4
kind:  Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  livenessProbe:
    initialDelaySeconds: 10
    timeoutSeconds: 10
    periodSeconds: 10
    failureThreshold: 200
```

## Readiness Probe
Splunk Opeator implements Readiness Probe using script `tools/k8_probes/readinessProbe.sh` which is mounted on splunk pods using config map `splunk-default-probe-configmap`. The configurations InitialDelaySeconds,TimeoutSeconds, PeriodSeconds, FailureThreshold can be customized through the CR.

Standalone example to configure readiness Probe. 
```
apiVersion: enterprise.splunk.com/v4
kind:  Standalone
metadata:
  name: example
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  replicas: 1
  livenessProbe:
    initialDelaySeconds: 10
    timeoutSeconds: 10
    periodSeconds: 10
    failureThreshold: 200
```