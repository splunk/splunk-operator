---
title: Kubernetes Quality of Service Classes
nav_order: 27.1
---

## Using Kubernetes Quality of Service Classes (QoS)

In addition to the guidelines provided in the [reference hardware](/installation/Prerequisites#hardware-resources-requirements), [Kubernetes QoS Classes](https://kubernetes.io/docs/tasks/configure-pod-container/quality-service-pod/) can be used to configure CPU/Mem resources allocations that map to your _service level objectives_. For further information on utilizing Kubernetes Quality of Service (QoS) classes, see the table below:


| QoS        | Summary| Description |
| ---------- | ------- | ------- |
| _Guaranteed_ | _CPU/Mem ```requests``` = CPU/Mem ```limits```_    | _When the CPU and memory  ```requests``` and ```limits``` values are equal, the pod is given a QoS class of Guaranteed. This level of service is recommended for Splunk Enterprise ___production environments___._ |
| _Burstable_ | _CPU/Mem ```requests``` < CPU/Mem ```limits```_  | _When the CPU and memory  ```requests``` value is set lower than the ```limits``` the pod is given a QoS class of Burstable. This level of service is useful in a user acceptance testing ___(UAT) environment___, where the pods run with minimum resources, and Kubernetes allocates additional resources depending on usage._|
| _BestEffort_ | _No CPU/Mem ```requests``` or ```limits``` are set_ | _When the ```requests``` or ```limits``` values are not set, the pod is given a QoS class of BestEffort. This level of service is sufficient for ___testing, or a small development task___._ |


## Examples of Guaranteed and Burstable QoS

### A Guaranteed QoS Class example:
Set equal ```requests``` and ```limits``` values for CPU and memory to establish a QoS class of Guaranteed. 

{: .note }
A pod will not start on a node that cannot meet the CPU and memory ```requests``` values.

Example: The minimum resource requirements for a Standalone Splunk Enterprise instance in production are 24 vCPU and 12GB RAM. 

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
spec:
  imagePullPolicy: Always
  resources:
    requests:
      memory: "12Gi"
      cpu: "24"
    limits:
      memory: "12Gi"
      cpu: "24"  
```

### A Burstable QoS Class example:
Set the ```requests``` value for CPU and memory lower than the ```limits``` value to establish a QoS class of Burstable. 

Example: This Standalone Splunk Enterprise instance should start with minimal indexing and search capacity, but will be allowed to scale up if Kubernetes is able to allocate additional CPU and Memory up to the ```limits``` values.

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
spec:
  imagePullPolicy: Always
  resources:
    requests:
      memory: "2Gi"
      cpu: "4"
    limits:
      memory: "12Gi"
      cpu: "24"  
```

### A BestEffort QoS Class example:
With no requests or limits values set for CPU and memory, the QoS class is set to BestEffort. The BestEffort QoS is not recommended for use with Splunk Operator.

### Pod Resources Management

__CPU Throttling__

Kubernetes starts throttling CPUs if a pod's demand for CPU exceeds the value set in the ```limits``` parameter. If your nodes have extra CPU resources available, leaving the ```limits``` value unset will allow the pods to utilize more CPUs.

__POD Eviction - OOM__

As oppose to throttling in case of CPU cycles starvation,  Kubernetes will evict a pod from the node if the pod's memory demands exceeds the value set in the ```limits``` parameter.
