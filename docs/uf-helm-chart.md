---
title: Universal Forwarder Helm Chart
nav_order: 10
---

# splunk-universalforwarder Helm Chart Deployment Guide

Deploys a Splunk Universal Forwarder (UF) on Kubernetes. The UF collects logs and metrics from workloads or from node-level paths and forwards them to a Splunk indexer cluster or Deployment Server.

---

## Table of Contents

1. [What it is and why](#1-what-it-is-and-why)
2. [Prerequisites](#2-prerequisites)
3. [Quick Install](#3-quick-install)
4. [Workload Mode](#4-workload-mode)
5. [Forwarding Configuration](#5-forwarding-configuration)
6. [Password Setup](#6-password-setup)
7. [SSL / Encrypted Forwarding](#7-ssl--encrypted-forwarding)
8. [Storage Configuration](#8-storage-configuration)
9. [Service and Ingress for Data Ingestion](#9-service-and-ingress-for-data-ingestion)
10. [High Availability and Network Controls](#10-high-availability-and-network-controls)
11. [All Configuration Fields](#11-all-configuration-fields)
12. [Upgrade](#12-upgrade)
13. [Undeploy](#13-undeploy)
14. [Troubleshooting](#14-troubleshooting)
15. [Using as a Sub-chart of splunk-enterprise](#15-using-as-a-sub-chart-of-splunk-enterprise)
16. [Architecture Reference](#16-architecture-reference)

---

## 1. What it is and why

A Splunk Universal Forwarder is a lightweight Splunk agent that tails files, listens on network ports, and streams data to a downstream Splunk indexer over TCP port 9997 (the S2S protocol). Running it as a Kubernetes workload lets you centralise log collection without depending on node-level log shipping agents.

This chart:
- Runs as a **Deployment** collecting logs from workloads or network inputs.
- Manages the admin password, Ansible `default.yml`, and `outputs.conf` entirely from Helm values — no manual pod exec required.
- Defaults to **stateless** storage (`storage.emptyDir: true`) — pod restarts cleanly from scratch with no PVCs required. Set `storage.emptyDir: false` to use PersistentVolumeClaims for durable fishbucket checkpoints when monitoring local files.
- Mirrors the security posture of the splunk-operator Standalone CRD: uid 41812, drop all capabilities, no privilege escalation.
- Uses `startupProbe` (40×15s = 600s max) to handle slow Ansible init gracefully, keeping liveness and readiness probes responsive once Splunk is up.

---

## 2. Prerequisites

| Requirement | Notes |
|-------------|-------|
| Helm | v3.x |
| Kubernetes | See [Splunk Operator release and version compatibility matrix](https://github.com/splunk/splunk-operator/blob/main/docs/README.md) for supported versions |
| PersistentVolume provisioner | Only required when `storage.emptyDir: false` |
| `helm-unittest` plugin | Only needed to run `helm unittest` |

> **Splunk General Terms:** Use of the Splunk Universal Forwarder image requires acceptance of the Splunk General Terms. See [Splunk General Terms Acceptance](https://splunk.github.io/splunk-operator/#splunk-general-terms-acceptance) in the Splunk Operator documentation for the required `SPLUNK_GENERAL_TERMS` env var and the legal language you must accept before setting it.

---

## 3. Quick Install

Minimum required: one of `splunkConfig.forwardServer` or `splunkConfig.deploymentServer`, plus a non-default password.

```sh
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --namespace my-namespace \
  --create-namespace \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=MySecurePassword1
```

> **Security note:** `--set` values appear in shell history. For production use a `--values` file or `splunkConfig.existingSecret` (see [Password Setup](#6-password-setup)).

> **Validation:** The chart fails at render time if `splunkConfig.password` is the default `changeme` and no `existingSecret` is provided. Always set a custom password.

Verify the pod is running:

```sh
kubectl get pods -n my-namespace -l app.kubernetes.io/name=splunk-universalforwarder
```

Verify forwarding is active:

```sh
kubectl exec -n my-namespace <pod-name> -- \
  grep "AutoLoadBalancedConnectionStrategy" \
  /opt/splunkforwarder/var/log/splunk/splunkd.log | tail -5
```

---

## 4. Workload Mode

The chart deploys a **Deployment**. One or more replicas can be configured via `replicaCount`. Use cases:
- Centralised syslog receiver (with `service.enabled=true`).
- Forwarding application logs received over a network input.
- General-purpose log forwarding to a downstream indexer.

```sh
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --set replicaCount=2 \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=MySecurePassword1
```

---

## 5. Forwarding Configuration

The chart renders differently depending on which forwarding option you use.

### 5.1 Direct indexer — single (`splunkConfig.forwardServer`)

```sh
--set splunkConfig.forwardServer=indexer.example.com:9997
```

The chart writes `default.yml` with `forward_servers` and `s2s_port`. Ansible calls `splunk add forward-server` during startup, which produces:

```ini
# /opt/splunkforwarder/etc/system/local/outputs.conf
[indexAndForward]
index = false

[tcpout]
defaultGroup = default-autolb-group

[tcpout:default-autolb-group]
server = indexer.example.com:9997
```

### 5.2 Direct indexer — list (`splunkConfig.forwardServers`)

For multiple indexers, use the `forwardServers` list. It takes precedence over `forwardServer` when non-empty.

```yaml
# values-override.yaml
splunkConfig:
  forwardServers:
    - idx1.example.com:9997
    - idx2.example.com:9997
  password: MySecurePassword1
```

```sh
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --values values-override.yaml
```

The first entry is used as the `forward_servers` host and `s2s_port` written to `default.yml`.

### 5.3 Deployment Server (`splunkConfig.deploymentServer`)

```sh
--set splunkConfig.deploymentServer=ds.example.com:8089 \
--set splunkConfig.deploymentClientName=my-cluster-uf
```

The UF registers with the DS on startup. The DS pushes a deployment app that contains `outputs.conf`. No static `outputs.conf` is written at install time.

### 5.4 Advanced: raw conf files (`conf.outputs`, `conf.inputs`)

For multi-indexer load balancing or custom routing that the `forwardServer` shortcut cannot express:

```yaml
# values-override.yaml
conf:
  outputs: |
    [tcpout]
    defaultGroup = primary-indexers

    [tcpout:primary-indexers]
    server = idx1.example.com:9997,idx2.example.com:9997
    autoLBFrequency = 30
    useACK = true

  inputs: |
    [monitor:///var/log/app/*.log]
    index = app_logs
    sourcetype = app_json
```

```sh
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --values values-override.yaml \
  --set splunkConfig.forwardServer=idx1.example.com:9997
```

The conf files are mounted via a ConfigMap at `system/local/`, which shadows Ansible-written files at that path.

> **Validation:** `splunkConfig.forwardServer`, `splunkConfig.forwardServers`, or `splunkConfig.deploymentServer` must always be set. The chart will fail to render if all three are empty.

### 5.5 Adding monitors (`splunkConfig.add`)

```sh
--set splunkConfig.add[0]="monitor /var/log/syslog" \
--set splunkConfig.add[1]="udp 514"
```

Each entry maps to a `splunk add` call during Ansible init. The items are comma-joined into `SPLUNK_ADD`.

---

## 6. Password Setup

### 6.1 Chart-managed Secret (default)

```sh
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=MySecurePassword1
```

The chart creates a Secret with two keys:
- `password` — the plaintext password, b64-encoded
- `default.yml` — the Ansible config blob that includes `splunk.password`

Both are consumed at startup. The UF sets the admin password declaratively via the Splunk REST API (`SPLUNK_DECLARATIVE_ADMIN_PASSWORD=true`).

> **Validation:** The chart fails at render time if `splunkConfig.password="changeme"` and no `existingSecret` is provided. This prevents the most common misconfiguration where the default password leaves remote login disabled.

### 6.2 Custom Secret key (`splunkConfig.passwordKey`)

If your Secret stores the password under a key other than `password`:

```sh
--set splunkConfig.passwordKey=splunk-admin-password
```

The chart's `SPLUNK_PASSWORD` env var and the `secretKeyRef` use this key name.

### 6.3 Bring your own Secret (`splunkConfig.existingSecret`)

Create the Secret manually:

```sh
kubectl create secret generic my-uf-secret \
  --namespace my-namespace \
  --from-literal=password=MySecurePassword1 \
  --from-literal=default.yml='splunk:
  password: "MySecurePassword1"
  forward_servers:
    - indexer.example.com
  s2s_port: "9997"
  kvstore:
    disabled: 1
'
```

Then reference it:

```sh
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.existingSecret=my-uf-secret
```

The chart skips Secret creation entirely and mounts the named Secret. The password validation guard is also skipped when `existingSecret` is set.

### 6.4 Rotating the password

**Stateless mode (`storage.emptyDir: true`, default):** update the Secret value and delete the pod — it restarts from scratch and picks up the new password automatically.

```sh
helm upgrade my-uf ./helm-chart/splunk-universalforwarder \
  --namespace my-namespace \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=NewSecurePassword1
```

**Durable mode (`storage.emptyDir: false`):** password rotation requires deleting the PVCs so Splunk reinitialises with the new credential (the old passwd hash is stored on the PVC):

```sh
# 1. Uninstall the release (PVCs are NOT deleted automatically)
helm uninstall my-uf --namespace my-namespace

# 2. Delete the PVCs
kubectl delete pvc \
  my-uf-splunk-universalforwarder-etc \
  my-uf-splunk-universalforwarder-var \
  --namespace my-namespace

# 3. Reinstall with the new password
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --namespace my-namespace \
  --set storage.emptyDir=false \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=NewSecurePassword1
```

---

## 7. SSL / Encrypted Forwarding

Create a Secret containing your S2S certificate and CA:

```sh
kubectl create secret generic uf-s2s-certs \
  --namespace my-namespace \
  --from-file=tls.crt=./s2s-cert.pem \
  --from-file=ca.crt=./ca-cert.pem
```

Enable SSL in the chart:

```sh
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=MySecurePassword1 \
  --set splunkConfig.ssl.secretName=uf-s2s-certs
```

The chart:
1. Mounts the Secret at `/opt/splunkforwarder/etc/auth/s2s/` (configurable via `splunkConfig.ssl.mountPath`)
2. Sets `SPLUNK_S2S_CERT=/opt/splunkforwarder/etc/auth/s2s/tls.crt`
3. Sets `SPLUNK_S2S_CA=/opt/splunkforwarder/etc/auth/s2s/ca.crt`

Ansible wires these into `server.conf` for encrypted S2S forwarding. Works with any `replicaCount`.

To use different key names within the Secret:

```sh
--set splunkConfig.ssl.certKey=splunk-s2s.crt \
--set splunkConfig.ssl.caKey=splunk-ca.crt
```

---

## 8. Storage Configuration

### 8.1 Stateless mode (default — `storage.emptyDir: true`)

Both `etc/` and `var/` are backed by `emptyDir` volumes on the node's local disk. No PVCs are created. On pod restart Splunk initialises from scratch: Ansible re-runs, `outputs.conf` is rewritten from the Secret, and forwarding resumes.

This is the right choice for a **pure forwarding tier** that receives data over the network or from a Deployment Server, where re-initialising on restart is acceptable.

```sh
# Default — no extra flags needed
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=MySecurePassword1
```

### 8.2 Durable mode (`storage.emptyDir: false`)

Two PersistentVolumeClaims are created. They persist across pod restarts and `helm upgrade`. They are **not** deleted on `helm uninstall`.

| PVC | Mount | Default Size | Contains |
|-----|-------|-------------|----------|
| `<release>-splunk-universalforwarder-etc` | `/opt/splunkforwarder/etc` | 1Gi | Config files, splunk.secret, passwd |
| `<release>-splunk-universalforwarder-var` | `/opt/splunkforwarder/var` | 5Gi | Runtime data, fishbucket, logs |

Use this when `splunkConfig.add` includes `monitor` inputs and you need the fishbucket to survive restarts to avoid re-indexing duplicate data.

```sh
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --set storage.emptyDir=false \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=MySecurePassword1 \
  --set "splunkConfig.add[0]=monitor /var/log/app/*.log"
```

Resize PVCs via:

```sh
--set storage.etcSize=2Gi \
--set storage.varSize=10Gi
```

Use a specific StorageClass:

```sh
--set storage.storageClassName=gp3
```

---

## 9. Service and Ingress for Data Ingestion

The UF chart supports receiving data pushed from external sources — syslog, HEC, or raw TCP — via an optional multi-port Service and an optional Ingress for HTTP Event Collector (HEC).

### 9.1 Multi-port Service

The Service exposes four ports by default when `service.enabled=true`:

| Name | Port | Protocol | Use |
|------|------|----------|-----|
| `syslog-udp` | 514 | UDP | Syslog over UDP |
| `syslog-tcp` | 601 | TCP | Syslog over TCP |
| `splunktcp` | 9997 | TCP | Splunk-to-Splunk (S2S) input |
| `hec` | 8088 | TCP | HTTP Event Collector |

```sh
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=MySecurePassword1 \
  --set service.enabled=true \
  --set "splunkConfig.add[0]=udp 514"
```

This creates a `ClusterIP` Service. Change to `LoadBalancer` for external access:

```sh
--set service.type=LoadBalancer
```

Add Service annotations (e.g., for AWS NLB):

```sh
--set service.annotations."service\.beta\.kubernetes\.io/aws-load-balancer-type"=nlb
```

To customise the port list entirely:

```yaml
# values-override.yaml
service:
  enabled: true
  ports:
    - name: hec
      port: 8088
      protocol: TCP
    - name: syslog
      port: 514
      protocol: UDP
```

> **Validation:** `service.enabled` must be `true` when `ingress.enabled=true`. The chart fails at render time if Ingress is enabled without a backing Service.

### 9.2 HEC Ingress

Expose the HEC endpoint externally via an `Ingress` resource. Requires `service.enabled=true`.

```sh
helm install my-uf ./helm-chart/splunk-universalforwarder \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=MySecurePassword1 \
  --set service.enabled=true \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set "ingress.hosts[0].host=hec.example.com" \
  --set "ingress.hosts[0].paths[0].path=/services/collector" \
  --set "ingress.hosts[0].paths[0].pathType=Prefix"
```

With TLS:

```yaml
# values-override.yaml
service:
  enabled: true

ingress:
  enabled: true
  className: nginx
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
  hosts:
    - host: hec.example.com
      paths:
        - path: /services/collector
          pathType: Prefix
  tls:
    - secretName: hec-tls
      hosts:
        - hec.example.com
```

The Ingress always targets the `hec` backend port (8088) on the chart Service.

---

## 10. High Availability and Network Controls

### 10.1 PodDisruptionBudget

Prevents the Kubernetes scheduler from evicting too many UF pods at once during node maintenance or cluster upgrades.

```sh
--set podDisruptionBudget.enabled=true \
--set podDisruptionBudget.minAvailable=1
```

### 10.2 NetworkPolicy

Lock down which pods can reach the UF and where the UF can send traffic.

```yaml
# values-override.yaml
networkPolicy:
  enabled: true
  ingress:
    - ports:
        - protocol: UDP
          port: 514
        - protocol: TCP
          port: 8088
  egress:
    - ports:
        - protocol: TCP
          port: 9997   # allow forwarding to indexers
```

When `networkPolicy.enabled=true` with empty `ingress`/`egress` lists, the policy blocks all inbound and outbound traffic.

---

## 11. All Configuration Fields

### Image

| Field | Default | Description |
|-------|---------|-------------|
| `image.repository` | `docker.io/splunk/universalforwarder` | Container image registry and name |
| `image.tag` | `10.4.0` | Splunk UF image tag / version |
| `image.pullPolicy` | `IfNotPresent` | Kubernetes image pull policy |
| `image.imagePullSecrets` | `[]` | Pull secrets for private registries |
| `initImage.repository` | `docker.io/library/busybox` | Pinned busybox image for `fix-ownership` init container |
| `initImage.tag` | `1.36.1` | busybox version |

### Workload

| Field | Default | Description |
|-------|---------|-------------|
| `replicaCount` | `1` | Number of Deployment replicas |
| `updateStrategy` | `RollingUpdate maxSurge=1 maxUnavailable=0` | Deployment update strategy |
| `minReadySeconds` | `0` | Seconds a new pod must be Ready before counted available |
| `podAnnotations` | `{}` | Annotations added to each pod |
| `deploymentAnnotations` | `{}` | Annotations added to the Deployment |
| `nodeSelector` | `{}` | Node selector labels |
| `tolerations` | `[]` | Pod tolerations |
| `affinity` | `{}` | Pod affinity/anti-affinity rules |
| `nameOverride` | `""` | Override the chart name component |
| `fullnameOverride` | `""` | Override the full release name |
| `namespaceOverride` | `""` | Deploy into a different namespace than the release |

### Splunk Configuration

| Field | Default | Description |
|-------|---------|-------------|
| `splunkConfig.password` | `changeme` | Admin password — **must be changed; chart fails if left as default** |
| `splunkConfig.existingSecret` | `""` | Name of a pre-existing Secret; skips chart Secret creation and password validation |
| `splunkConfig.passwordKey` | `password` | Key within the Secret holding the password |
| `splunkConfig.forwardServer` | `""` | `host:port` of the downstream indexer (required unless `deploymentServer` or `forwardServers` is set) |
| `splunkConfig.forwardServers` | `[]` | List of indexer endpoints; takes precedence over `forwardServer` when non-empty |
| `splunkConfig.deploymentServer` | `""` | `host:port` of the Deployment Server |
| `splunkConfig.deploymentClientName` | `""` | Client name reported to the Deployment Server |
| `splunkConfig.add` | `[]` | List of `splunk add` arguments, e.g. `["monitor /var/log/app/*.log"]` |
| `splunkConfig.defaultsUrl` | `""` | URL of a remote `default.yml`; overrides the default `/mnt/splunk-secrets/default.yml` |
| `splunkConfig.ssl.secretName` | `""` | Secret containing S2S TLS cert and CA |
| `splunkConfig.ssl.certKey` | `tls.crt` | Key within the SSL Secret for the TLS certificate |
| `splunkConfig.ssl.caKey` | `ca.crt` | Key within the SSL Secret for the CA certificate |
| `splunkConfig.ssl.mountPath` | `/opt/splunkforwarder/etc/auth/s2s` | Mount path for the SSL Secret inside the container |

### Raw Conf Files

| Field | Default | Description |
|-------|---------|-------------|
| `conf.outputs` | `""` | Raw `outputs.conf` content; mounted via ConfigMap at `system/local/outputs.conf` |
| `conf.inputs` | `""` | Raw `inputs.conf` content; mounted via ConfigMap at `system/local/inputs.conf` |

### Storage

| Field | Default | Description |
|-------|---------|-------------|
| `storage.emptyDir` | `true` | Use emptyDir (stateless). Set `false` for PVC-backed durable mode |
| `storage.storageClassName` | `""` | StorageClass for PVCs; empty = cluster default (durable mode only) |
| `storage.etcSize` | `1Gi` | Size of the PVC for `/opt/splunkforwarder/etc` (durable mode only) |
| `storage.varSize` | `5Gi` | Size of the PVC for `/opt/splunkforwarder/var` (durable mode only) |
| `persistence.enabled` | `false` | Legacy hostPath fishbucket (not used in Deployment mode) |

### Service

| Field | Default | Description |
|-------|---------|-------------|
| `service.enabled` | `false` | Create a Service |
| `service.type` | `ClusterIP` | Kubernetes Service type (`ClusterIP`, `NodePort`, `LoadBalancer`) |
| `service.annotations` | `{}` | Annotations on the Service (e.g. cloud LB annotations) |
| `service.ports` | see below | List of port definitions; each has `name`, `port`, `protocol` |

Default `service.ports`:
```yaml
- { name: syslog-udp, port: 514,  protocol: UDP }
- { name: syslog-tcp, port: 601,  protocol: TCP }
- { name: splunktcp,  port: 9997, protocol: TCP }
- { name: hec,        port: 8088, protocol: TCP }
```

### Ingress

| Field | Default | Description |
|-------|---------|-------------|
| `ingress.enabled` | `false` | Create an Ingress for HEC; requires `service.enabled=true` |
| `ingress.className` | `""` | Ingress class (e.g. `nginx`, `alb`) |
| `ingress.annotations` | `{}` | Ingress annotations |
| `ingress.hosts` | `[{host: "", paths: [{path: /services/collector, pathType: Prefix}]}]` | Ingress host rules |
| `ingress.tls` | `[]` | TLS configuration |

### PodDisruptionBudget

| Field | Default | Description |
|-------|---------|-------------|
| `podDisruptionBudget.enabled` | `false` | Create a PodDisruptionBudget |
| `podDisruptionBudget.minAvailable` | `1` | Minimum available pods during disruption |

### NetworkPolicy

| Field | Default | Description |
|-------|---------|-------------|
| `networkPolicy.enabled` | `false` | Create a NetworkPolicy |
| `networkPolicy.ingress` | `[]` | Ingress rules (empty = deny all inbound) |
| `networkPolicy.egress` | `[]` | Egress rules (empty = deny all outbound) |

### Resources

| Field | Default | Description |
|-------|---------|-------------|
| `resources.limits.cpu` | `200m` | CPU limit |
| `resources.limits.memory` | `500Mi` | Memory limit |
| `resources.limits.ephemeral-storage` | `1Gi` | Ephemeral storage limit (prevents node DiskPressure) |
| `resources.requests.cpu` | `100m` | CPU request |
| `resources.requests.memory` | `200Mi` | Memory request |

### Probes

| Field | Default | Description |
|-------|---------|-------------|
| `startupProbe.initialDelaySeconds` | `0` | Seconds before first startup probe |
| `startupProbe.periodSeconds` | `15` | Startup probe interval |
| `startupProbe.failureThreshold` | `40` | Max failures before pod is restarted (40×15s = 600s max startup) |
| `startupProbe.timeoutSeconds` | `10` | Seconds to wait for probe response |
| `readinessProbe.initialDelaySeconds` | `0` | Seconds before first readiness probe (startupProbe gates this) |
| `readinessProbe.periodSeconds` | `15` | Readiness probe interval |
| `readinessProbe.failureThreshold` | `5` | Consecutive failures before pod removed from Service endpoints |
| `readinessProbe.timeoutSeconds` | `10` | Seconds to wait for probe response |
| `livenessProbe.initialDelaySeconds` | `0` | Seconds before first liveness probe (startupProbe gates this) |
| `livenessProbe.periodSeconds` | `30` | Liveness probe interval |
| `livenessProbe.failureThreshold` | `3` | Consecutive failures before container restart |
| `livenessProbe.timeoutSeconds` | `10` | Seconds to wait for probe response |

### Security

| Field | Default | Description |
|-------|---------|-------------|
| `podSecurityContext.runAsUser` | `41812` | Splunk user uid |
| `podSecurityContext.runAsNonRoot` | `true` | Block root in the main and seed-etc containers |
| `podSecurityContext.fsGroup` | `41812` | GID applied to PVC volume mounts by the CSI driver |
| `podSecurityContext.fsGroupChangePolicy` | `Always` | Always re-apply fsGroup on mount |
| `containerSecurityContext.allowPrivilegeEscalation` | `false` | Block setuid/setgid |
| `containerSecurityContext.runAsNonRoot` | `true` | Block root in the main container |
| `containerSecurityContext.runAsUser` | `41812` | Run as splunk user |
| `containerSecurityContext.capabilities.drop` | `[ALL]` | Drop all Linux capabilities |

### ServiceAccount and RBAC

| Field | Default | Description |
|-------|---------|-------------|
| `serviceAccount.create` | `true` | Create a ServiceAccount |
| `serviceAccount.name` | `""` | Override ServiceAccount name (defaults to release fullname) |
| `serviceAccount.annotations` | `{}` | Annotations, e.g. for IRSA (`eks.amazonaws.com/role-arn`) |
| `rbac.create` | `true` | Create Role/ClusterRole + Binding |
| `rbac.clusterScoped` | `false` | Use ClusterRole instead of namespace-scoped Role |

### Debugging and Extras

| Field | Default | Description |
|-------|---------|-------------|
| `debug.enabled` | `false` | Set `SPLUNK_ANSIBLE_DEBUG=true` for verbose Ansible output |
| `extraManifests` | `[]` | Arbitrary extra Kubernetes resources rendered as Go templates |

---

## 12. Upgrade

### Standard upgrade

```sh
helm upgrade my-uf ./helm-chart/splunk-universalforwarder \
  --namespace my-namespace \
  --set splunkConfig.forwardServer=indexer.example.com:9997 \
  --set splunkConfig.password=MySecurePassword1
```

Pass all values you want to keep — `helm upgrade` does not preserve previously `--set` values unless you use `--reuse-values` (not recommended; use a values file instead).

### With a values file (recommended for production)

```sh
# Create values-production.yaml with all your overrides
helm upgrade my-uf ./helm-chart/splunk-universalforwarder \
  --namespace my-namespace \
  --values values-production.yaml
```

### Upgrading the image version

```sh
helm upgrade my-uf ./helm-chart/splunk-universalforwarder \
  --namespace my-namespace \
  --values values-production.yaml \
  --set image.tag=10.4.1
```

The PVCs are reused — Splunk will detect the existing `splunk.secret` and skip a full reinitialisation.

### Checking upgrade history

```sh
helm history my-uf --namespace my-namespace
```

### Rolling back

```sh
helm rollback my-uf <revision> --namespace my-namespace
```

---

## 13. Undeploy

### Remove the release

```sh
helm uninstall my-uf --namespace my-namespace
```

**Stateless mode (`storage.emptyDir: true`, default):** no PVCs exist — uninstall is clean.

**Durable mode (`storage.emptyDir: false`):** PVCs are retained after uninstall (Kubernetes default). Reinstalling with the same release name reuses them and their data.

### Remove the release and delete PVCs (durable mode only)

```sh
helm uninstall my-uf --namespace my-namespace

kubectl delete pvc \
  my-uf-splunk-universalforwarder-etc \
  my-uf-splunk-universalforwarder-var \
  --namespace my-namespace
```

PVC names follow the pattern `<release-name>-splunk-universalforwarder-etc` and `-var`. If you used a custom `fullnameOverride`, substitute that name.

### Remove everything including the namespace

```sh
kubectl delete namespace my-namespace
```

This removes all resources including PVCs.

---

## 14. Troubleshooting

### Pod stuck in `Init:CrashLoopBackOff`

Check which init container is failing:

```sh
kubectl describe pod <pod-name> -n my-namespace
```

**`fix-ownership` failing:**
Usually a policy constraint. Check the pod events:
```
container's runAsUser breaks non-root policy
```
This means your cluster has a PodSecurityAdmission policy that blocks `runAsUser: 0`. You will need to either relax the policy for the namespace or pre-provision PVCs with the correct uid.

**`seed-etc` failing with Permission denied:**
The PVC was not chowned before `seed-etc` ran. This should not happen with the current chart — if you see it, verify `fix-ownership` ran and exited 0 first.

### Pod stuck in `Pending`

```sh
kubectl describe pod <pod-name> -n my-namespace | grep -A 10 Events
```

Common causes:
- **PVC still terminating from a previous deployment** — wait for EBS detach to complete, or check if old pods are still holding the PVC mount.
- **No nodes available** — check node taints / tolerations.
- **StorageClass does not exist** — verify `storage.storageClassName`.

### `outputs.conf` not written / forwarding not working

```sh
# Check outputs.conf content
kubectl exec -n my-namespace <pod-name> -- \
  cat /opt/splunkforwarder/etc/system/local/outputs.conf

# Check splunkd log for connection attempts
kubectl exec -n my-namespace <pod-name> -- \
  grep -E "TcpOutput|AutoLoadBalanced|Connected|forward" \
  /opt/splunkforwarder/var/log/splunk/splunkd.log | tail -20
```

If `outputs.conf` only contains `[indexAndForward] index = false` with no `[tcpout]` block, the `splunk add forward-server` call failed. Common causes:
- Password mismatch (stale PVC has a different password than current Secret)
- Both `forward_servers` and `s2s_port` must be set in `default.yml` — `forward_servers` alone does not trigger the CLI call

### Enable verbose Ansible output

```sh
helm upgrade my-uf ./helm-chart/splunk-universalforwarder \
  --namespace my-namespace \
  --values values-production.yaml \
  --set debug.enabled=true
```

Then check the container logs:

```sh
kubectl logs -n my-namespace <pod-name> -c splunk-universalforwarder | head -200
```

### Helm upgrade fails with `context deadline exceeded`

The pod did not become Ready within the `--timeout` period. This is usually a pod startup issue, not a Helm issue. Check pod events and logs as above. The release is left in `failed` state — fix the underlying issue and run `helm upgrade` again.

---

## 15. Using as a Sub-chart of splunk-enterprise

The `splunk-universalforwarder` chart is included as an optional, disabled-by-default dependency in `helm-chart/splunk-enterprise/`.

### Enable when installing splunk-enterprise

```sh
helm install my-stack ./helm-chart/splunk-enterprise \
  --namespace my-namespace \
  --set universalforwarder.enabled=true \
  --set "universalforwarder.splunkConfig.forwardServer=<indexer-svc>.<namespace>.svc.cluster.local:9997" \
  --set universalforwarder.splunkConfig.password=MySecurePassword1
```

All `splunk-universalforwarder` values are namespaced under `universalforwarder.*` when used as a sub-chart.

### Enable when upgrading an existing splunk-enterprise release

```sh
helm upgrade my-stack ./helm-chart/splunk-enterprise \
  --namespace my-namespace \
  --values values-production.yaml \
  --set universalforwarder.enabled=true \
  --set "universalforwarder.splunkConfig.forwardServer=splunk-my-stack-standalone-service.my-namespace.svc.cluster.local:9997" \
  --set universalforwarder.splunkConfig.password=MySecurePassword1
```

---

## 16. Architecture Reference

### Pod Startup Sequence

![Pod Startup Sequence](diagrams/pod-startup.png)

### Kubernetes Resources

![Kubernetes Resources](diagrams/k8s-resources.png)

### Forwarding Configuration Decision Tree

![Forwarding Configuration](diagrams/forwarding-config.png)

### Password and Auth Flow

![Password and Auth Flow](diagrams/password-auth-flow.png)

For diagram source files, see `helm-chart/splunk-universalforwarder/diagrams/`.
