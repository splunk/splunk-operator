# cert-manager Integration with Splunk Operator

This guide explains how to use [cert-manager](https://cert-manager.io/) to provision and manage TLS certificates for Splunk Enterprise pods deployed by the Splunk Operator. As an example, we will demonstrate how to configure a **Standalone** Splunk instance to use cert-manager–issued certificates instead of the default self-signed ones.

---

## Table of Contents

1. [Overview](#overview)  
2. [Prerequisites](#prerequisites)  
3. [Installation Steps](#installation-steps)  
   1. [Install cert-manager](#install-cert-manager)  
   2. [Create a ClusterIssuer or Issuer](#create-a-clusterissuer-or-issuer)  
   3. [Install the Splunk Operator](#install-the-splunk-operator)  
   4. [Configure and Deploy Splunk with cert-manager Annotations](#configure-and-deploy-splunk-with-cert-manager-annotations)  
4. [Standalone CR Example](#standalone-cr-example)  
   1. [CSI Driver Approach](#csi-driver-approach)  
   2. [Sidecar Injector Approach](#sidecar-injector-approach)  
5. [Splunk Configuration for Certificates](#splunk-configuration-for-certificates)  
6. [Validation and Troubleshooting](#validation-and-troubleshooting)  
7. [FAQ](#faq)  
8. [References](#references)

---

## 1. Overview

By default, Splunk generates **self-signed** certificates for Splunk Web (port 8000) and Splunk Management Port (8089). In production environments, self-signed certificates may be considered insecure or untrusted. [cert-manager](https://cert-manager.io/) automates the process of obtaining, renewing, and placing certificates from a **Certificate Authority (CA)** (e.g., Let’s Encrypt, internal CA).

**Key benefits**:

- Automatically renew and replace certificates before they expire.  
- Avoid the complexity of manual certificate provisioning.  
- Provide a trusted, CA-signed certificate for Splunk’s internal and external communication.

---

## 2. Prerequisites

1. **Kubernetes** cluster (v1.19+ recommended).  
2. **Splunk Operator** installed (or you plan to install it).  
3. **cert-manager** installed (1.6+ recommended).  
4. Familiarity with Splunk’s TLS configurations (e.g., `server.conf`, `web.conf`).  

---

## 3. Installation Steps

### 3.1 Install cert-manager

You can install cert-manager using [Helm](https://helm.sh/) or by applying official YAML manifests.

<details>
<summary><strong>Install via Helm (Recommended)</strong></summary>

```bash
helm repo add jetstack https://charts.jetstack.io
helm repo update

helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version v1.11.0 \
  --set installCRDs=true
```
</details>

<details>
<summary><strong>Install via Manifests</strong></summary>

```bash
kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.11.0/cert-manager.yaml
```
</details>

Verify cert-manager is running:
```bash
kubectl get pods -n cert-manager
```
You should see pods named `cert-manager`, `cert-manager-cainjector`, and `cert-manager-webhook` in `Running` state.

---

### 3.2 Create a ClusterIssuer or Issuer

A **ClusterIssuer** (or **Issuer** if scoped to one namespace) defines how cert-manager obtains certificates. Below is an example `ClusterIssuer` using Let’s Encrypt’s staging endpoint (recommended for testing):

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-staging
spec:
  acme:
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    email: you@example.com
    privateKeySecretRef:
      name: letsencrypt-staging-key
    solvers:
      - http01:
          ingress:
            class: nginx
```

Apply:
```bash
kubectl apply -f cluster-issuer-staging.yaml
```

> If you don’t have an Ingress or prefer DNS challenges, adapt accordingly. For offline or restricted environments, you might use a self-signed or internal CA issuer.

---

### 3.3 Install the Splunk Operator

Depending on your version of the Splunk Operator, you can typically install it by:

```bash
kubectl create namespace splunk-operator
kubectl apply -f https://github.com/splunk/splunk-operator/releases/latest/download/splunk-operator-install.yaml -n splunk-operator
```

Confirm the Operator is running:
```bash
kubectl get pods -n splunk-operator
```
Look for `splunk-operator-xxxx` in `Running` state.

---

### 3.4 Configure and Deploy Splunk with cert-manager Annotations

You’ll create a Splunk **Standalone** (or other CR type) resource. For the sake of this guide, we’ll demonstrate two distinct approaches to hooking up cert-manager:

1. **CSI Driver** – Use the [cert-manager CSI driver](https://cert-manager.io/docs/usage/csi/) to mount up-to-date certificates directly into the Splunk pod.  
2. **Sidecar Injector** – Use an external sidecar injection webhook that automatically adds a container which handles certs.  

In **both** cases, you’ll annotate the Splunk CR with something like `splunk.com/cert-manager` to signal that you want to use cert-manager for TLS certificates.

---

## 4. Standalone CR Example

Below is a simplified `Standalone` CR that references a **namespace** called `test`. Adjust for your environment.

```bash
kubectl create namespace test
```

### 4.1 CSI Driver Approach

```yaml
# file: standalone-csi.yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: splunk-standalone-csi
  namespace: test
  annotations:
    splunk.com/cert-manager: "csi"
    # references:
    splunk.com/cert-secret-name: "splunk-cert-secret"
    splunk.com/cert-issuer-name: "letsencrypt-staging"
    splunk.com/cert-issuer-kind: "ClusterIssuer"
spec:
  replicas: 1
  # Additional Splunk config...
```

- `splunk.com/cert-manager: "csi"` tells the operator code to **add a CSI volume** referencing `splunk-cert-secret`.
- `splunk.com/cert-secret-name`: The secret name that cert-manager updates with a valid certificate (which might be automatically managed by the operator or created as a separate `Certificate` resource).
- `splunk.com/cert-issuer-name` & `splunk.com/cert-issuer-kind`: Usually `ClusterIssuer` or `Issuer`.

**Apply**:
```bash
kubectl apply -f standalone-csi.yaml
```

**Result**:

- The operator modifies the `Standalone` pod spec to include a volume from the `csi.cert-manager.io` driver.  
- Splunk can read the certificate from `/mnt/splunk/certificates` (or whichever path your operator sets).  
- If you also want Splunk to automatically **reload** when the certificate changes, you can either rely on a rolling restart or add a small sidecar container in your operator code.

---

### 4.2 Sidecar Injector Approach

```yaml
# file: standalone-injector.yaml
apiVersion: enterprise.splunk.com/v5
kind: Standalone
metadata:
  name: splunk-standalone-injector
  namespace: test
  annotations:
    splunk.com/cert-manager: "injector"
    sidecar-injector-webhook.svc.cluster.local/inject: "true"
    # Example: pass additional hints to the sidecar injector
    sidecar-injector-webhook.svc.cluster.local/cert-secret-name: "splunk-cert-secret"
    sidecar-injector-webhook.svc.cluster.local/cert-mount-path: "/mnt/splunk/certificates"
spec:
  replicas: 1
  # Additional Splunk config ...
```

- `splunk.com/cert-manager: "injector"` might signal your operator to set certain base annotations, or pass them verbatim.  
- The **sidecar injector** is a separate mutating webhook that sees `sidecar-injector-webhook.svc.cluster.local/inject: "true"` and modifies the Pod at admission time, adding a container (and volume) that handle certificates.

**Apply**:
```bash
kubectl apply -f standalone-injector.yaml
```

**Result**:

- The Operator sets the correct Pod annotations.  
- The **sidecar injector** automatically injects a “cert manager sidecar” with the correct volume and volumeMount for the Splunk container.  
- You must ensure the sidecar injection webhook is correctly configured to create a shared volume mount named (for example) `splunk-certs`, mapped to `/mnt/splunk/certificates`.

---

## 5. Splunk Configuration for Certificates

No matter which approach you choose, Splunk must be configured to **use** the certificates at the correct path. Typically, you set:

```ini
# server.conf
[sslConfig]
enableSplunkdSSL = true
serverCert = /mnt/splunk/certificates/tls.crt
sslRootCAPath = /mnt/splunk/certificates/ca.crt
privKeyPath = /mnt/splunk/certificates/tls.key
```

And for Splunk Web (port 8000):

```ini
# web.conf
[settings]
enableSplunkWebSSL = true
httpseKeyFile = /mnt/splunk/certificates/tls.key
httpseCertFile = /mnt/splunk/certificates/tls.crt
```

Where `/mnt/splunk/certificates` is the same path your operator or sidecar injection uses.

> **Tip**: These config files can be delivered via a ConfigMap, the CR `defaults`, or an App framework. Make sure they match the mount path used by your Pod spec.

---

## 6. Validation and Troubleshooting

1. **Check Splunk Pod**  
   ```bash
   kubectl get pods -n test
   kubectl describe pod splunk-standalone-csi-<id> -n test
   ```
   Confirm the volumes and volumeMounts are present:
   - If using CSI: A volume with `csi.cert-manager.io`.  
   - If using injector: A sidecar container and a volume named something like `splunk-certs`.

2. **Check the Certificate**  
   ```bash
   kubectl port-forward svc/splunk-standalone-csi-service -n test 8089:8089
   openssl s_client -connect 127.0.0.1:8089 -showcerts
   ```
   Look at the `Issuer` and `Subject` fields. They should reflect your CA (like Let’s Encrypt staging), **not** a self-signed Splunk cert.

3. **Sidecar Logs** (if used)  
   ```bash
   kubectl logs splunk-standalone-csi-0 -n test -c cert-watch-sidecar
   ```
   Verify it’s detecting changes and triggering restarts if that’s your chosen approach.

4. **Certificate Resource** (if your Operator or you create it)
   ```bash
   kubectl get certificate -n test
   ```
   Make sure its status is `READY=True`.

5. **Operator Logs**  
   ```bash
   kubectl logs deployment/splunk-operator -n splunk-operator
   ```
   Check for errors or warnings if the pods don’t mount the cert or start properly.

---

## 7. FAQ

### 7.1 Do I need both a sidecar **and** the CSI driver?

- You can use **CSI alone** if you plan to do a rolling restart or manual restart when certs change.  
- You can use a **sidecar** (injected or manually defined) to watch for file changes and immediately reload Splunk.  
- Some setups combine **CSI** for mounting and a **sidecar** for instant reloading.

### 7.2 What if the sidecar injection fails or conflicts?

- Check the sidecar injector webhook logs. Often, admission controllers log rejections or conflicts.  
- Ensure you haven’t added a volume named the same but with different definitions in your operator code.

### 7.3 Can I share the same certificate for Splunkd (8089) and Splunk Web (8000)?

- Yes. Just ensure your `server.conf` and `web.conf` reference the same `tls.crt`, `tls.key`, and `ca.crt` files.

### 7.4 How do I handle internal Splunk-to-Splunk traffic (indexers, search heads, etc.)?

- The same certificate can be used. Configure the relevant `[sslConfig]` stanzas or app settings. For multi-instance use (IndexerCluster, SearchHeadCluster, etc.), each Pod can mount the same CA-signed cert as long as the DNS or Common Name is valid.

---

## 8. References

- [cert-manager Docs](https://cert-manager.io/docs/)  
- [Splunk SSL Config](https://docs.splunk.com/Documentation/Splunk/9.4.1/Security/ConfigureandinstallcertificatesforLogObserver)  
- [Splunk Operator on GitHub](https://github.com/splunk/splunk-operator)  
- [CSI Driver for cert-manager](https://cert-manager.io/docs/usage/csi/)  

---
