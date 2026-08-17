---
title: Certificate Management
parent: Operate & Manage
nav_order: 7
---

# Certificate Management

- [Certificate Management](#certificate-management)
  - [Declaring a certificate on a Custom Resource](#declaring-a-certificate-on-a-custom-resource)
  - [What the operator requires in a cert Secret](#what-the-operator-requires-in-a-cert-secret)
  - [Splunk-managed roles: server and input](#splunk-managed-roles-server-and-input)
    - [server role (management port 8089)](#server-role-management-port-8089)
    - [input role (S2S port 9997)](#input-role-s2s-port-9997)
  - [Switching from a self-managed certificate to operator management](#switching-from-a-self-managed-certificate-to-operator-management)
  - [Mounting your own certificate (no role)](#mounting-your-own-certificate-no-role)
  - [Bringing your own certificate (recommended)](#bringing-your-own-certificate-recommended)
  - [Auto-generating a certificate with cert-manager](#auto-generating-a-certificate-with-cert-manager)
    - [Prerequisites](#prerequisites)
    - [DNS names](#dns-names)
    - [Changing dnsNames or other generation settings after the fact](#changing-dnsnames-or-other-generation-settings-after-the-fact)
    - [One certificate per Custom Resource](#one-certificate-per-custom-resource)
    - [SearchHeadCluster: deployer and search heads share one certificate](#searchheadcluster-deployer-and-search-heads-share-one-certificate)
  - [Certificate rotation](#certificate-rotation)
  - [Removing a certificate from spec.certs\[\]](#removing-a-certificate-from-speccerts)
  - [Security considerations](#security-considerations)
  - [Encrypting secrets at rest](#encrypting-secrets-at-rest)

## Declaring a certificate on a Custom Resource

Every Splunk Enterprise Custom Resource (Standalone, IndexerCluster, SearchHeadCluster, ClusterManager, LicenseManager, MonitoringConsole, IngestorCluster) accepts a list of certificates under `spec.certs`:

```yaml
apiVersion: enterprise.splunk.com/v4
kind: Standalone
metadata:
  name: example
spec:
  certs:
    - secretRef:
        name: my-server-cert
      role: server
    - secretRef:
        name: my-input-cert
      role: input
```

Each entry references a Kubernetes `Secret`, by name, **in the same namespace as the CR** — there is no cross-namespace reference. Up to 10 entries are allowed per CR, and at most one entry may declare `role: server` and at most one may declare `role: input`; any number of role-less entries are allowed (see [Mounting your own certificate](#mounting-your-own-certificate-no-role)).

The referenced Secret can either already exist — you supply the cert material yourself, which is the recommended approach (see [Bringing your own certificate](#bringing-your-own-certificate-recommended)) — or not exist yet, in which case the operator can create it for you via cert-manager if you ask it to (see [Auto-generating a certificate with cert-manager](#auto-generating-a-certificate-with-cert-manager)).

## What the operator requires in a cert Secret

Regardless of whether you create the Secret yourself or the operator auto-generates it, the operator validates that it has one of two shapes:

| Key | Required? | Description |
|-----|-----------|--------------|
| `tls.crt` | Required, unless CA-only (see below) | PEM-encoded certificate (leaf cert; may be followed by intermediate certs to form a chain) |
| `tls.key` | Required, unless CA-only (see below) | PEM-encoded private key matching `tls.crt` |
| `ca.crt` | Optional | PEM-encoded CA certificate used to populate the trust root |

**Valid combinations:**

- `tls.crt` + `tls.key`, with `ca.crt` optional — the normal case. This is required for **both** `server` and `input` roles.
- `ca.crt` only, with `tls.crt` and `tls.key` both absent — a **CA-only** Secret. This only makes sense for a role-less, mount-only Secret (see below) used to trust an externally-managed TLS endpoint; it is not accepted for `role: input`, and for `role: server` it only populates the trust root without a serving certificate.
- Any other partial combination (for example `tls.crt` without `tls.key`) is rejected — the operator will not mount the cert and will surface the failure on the CR status.

The operator does not validate certificate expiry, key type, key usage, or PEM chain structure — it only checks that the required keys are present. Malformed PEM content is caught later, if at all, by Splunk itself at startup.

## Splunk-managed roles: server and input

Setting `role: server` or `role: input` tells the operator to pass the cert into the pod so Splunk can use it for the matching purpose automatically.

### server role (management port 8089)

Used for Splunk's management port (8089) TLS.

### input role (S2S port 9997)

Used for forwarder-to-indexer (S2S) TLS on port 9997. 

## Switching from a self-managed certificate to operator management

If you currently manage TLS for the `server` or `input` role yourself — for example by hand-editing configuration files, or provisioning the certificate through some mechanism outside this operator — and you now want to hand that role over to `spec.certs[]`, understand that once you do, the certificate the operator serves (and, where applicable, trusts) is whatever you reference in `spec.certs[].secretRef`, not your previous certificate. Before making the switch:

- **Check whether the certificate you're about to declare in `spec.certs[]` is compatible with the one it's replacing** — in particular, whether it is signed by the same CA (or a CA that every peer involved already trusts). If it is, the switch is transparent and no further action is needed.
- **If it is not compatible (a different CA)**, any peer currently validating this CR's certificate — or that this CR validates — against the old CA will fail TLS verification as soon as the switch takes effect, until that peer's trust is updated to the new CA. This applies to:
  - `server` role: any component that connects to this CR with server-certificate verification enabled (`sslVerifyServerCert = true`).
  - `input` role: any forwarder sending to this CR with server-certificate verification enabled, and this CR itself if it requires and validates client certificates (`requireClientCert = true`).

  In that situation, **stage trust for the new CA on the affected side(s) first** — for example, update peers so they trust both the old and new CA, and update this CR's input-role client-CA trust if it validates forwarder client certificates. Apply the `spec.certs[]` change, confirm the new certificate is being served and accepted as expected, then remove the old CA from trust once no peer still depends on it. Only temporarily disable verification as a last-resort maintenance-window step when there is no supported way to stage trust, and re-enable it immediately after validation.
- Treat this as a coordinated change across every peer involved, not a one-sided edit to this CR — the operator only manages certificate material for the CR you're editing; it does not update trust or verification settings anywhere else on your behalf.

## Mounting your own certificate (no role)

If you leave `role` unset, the operator mounts the Secret into the pod as-is and does not wire it into any Splunk configuration. Use this to make your own certificate/CA material available inside the pod filesystem for purposes outside of the `server`/`input` roles above — for example, a CA bundle used only by an app, a sidecar, or a script you run yourself. Because these entries are not restricted to one-per-CR the way `server`/`input` are, you can declare as many role-less entries as you need, up to the overall 10-entry limit.

## Bringing your own certificate (recommended)

**This is the recommended way to provision certificates.** Create the Secret yourself before (or after) creating the CR, using the exact key names from [above](#what-the-operator-requires-in-a-cert-secret):

```bash
kubectl create secret generic my-server-cert \
  --from-file=tls.crt=./server.pem \
  --from-file=tls.key=./server.key \
  --from-file=ca.crt=./ca.pem \
  -n <namespace>
```

Then reference it by name in `spec.certs[].secretRef.name`. As long as the Secret already exists when the operator reconciles, it is mounted directly.

To rotate a self-managed certificate, update the Secret's `tls.crt`/`tls.key`/`ca.crt` values in place. See [Certificate rotation](#certificate-rotation) for what happens next.

If you are unable to provision and manage your own certificates — for example you don't have an existing PKI/CA workflow available — the operator can generate one for you instead, described next.

## Auto-generating a certificate with cert-manager

If the Secret named in `secretRef.name` does not exist, set `issuerRef` to have the operator create it for you via [cert-manager](https://cert-manager.io/):

```yaml
spec:
  certs:
    - secretRef:
        name: my-server-cert
      role: server
      issuerRef:
        name: my-selfsigned-issuer
        kind: ClusterIssuer   # or "Issuer" (default)
      dnsNames:
        - example.splunk.svc.cluster.local
```

### Prerequisites

Auto-generation has two hard prerequisites that the operator does **not** set up for you:

1. **cert-manager must be installed in the cluster.** The Splunk Operator's helm chart includes cert-manager as an optional dependency, disabled by default — enable it with `--set cert-manager.enabled=true` (or the equivalent `values.yaml` setting) if the cluster does not already have cert-manager installed. If your cluster already runs cert-manager (installed independently of this chart), leave it disabled here and use your existing installation.
2. **You must create your own `Issuer` or `ClusterIssuer`.** The operator never creates one for you, whether self-signed, CA-based, or ACME. Reconciliation fails (and is retried) if the Issuer/ClusterIssuer named in `issuerRef` is missing or not yet `Ready`. For development or internal-only deployments where clients explicitly trust the generated CA, a self-signed `ClusterIssuer` can work; for production or externally reached endpoints, use an issuer backed by a trusted organizational/public CA:

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: my-selfsigned-issuer
spec:
  selfSigned: {}
```

Once both prerequisites are met, the operator creates a cert-manager `Certificate` object requesting the given `dnsNames`, `duration`, `renewBefore`, and `rotationPolicy`, waits for cert-manager to populate the target Secret, and then proceeds exactly as it would for a Secret you provided yourself.

### DNS names

**It is always recommended that you set `dnsNames` explicitly** so the certificate's SANs match how you actually reach the pod (including any external/ingress hostnames). If you don't set them, the operator auto-derives a reasonable default from the CR:

- The CR's Kubernetes Service FQDN is always included.
- For a multi-replica StatefulSet, a wildcard SAN over the headless Service FQDN is added (for example `*.splunk-example-indexer-headless.ns.svc.cluster.local`) instead of one SAN per pod.
- For a single-replica StatefulSet, the explicit pod-0 FQDN is added instead of a wildcard.
- For MonitoringConsole, only the Service FQDN is used.

Auto-derived SANs only cover in-cluster DNS names. If clients reach the instance through an external hostname (a `LoadBalancer`, `Ingress`, or DNS record outside the cluster), that hostname will not be in the certificate unless you add it explicitly via `dnsNames`.

### Changing dnsNames or other generation settings after the fact

Once cert-manager has generated a certificate for a `spec.certs[]` entry, the operator does not revisit that request. If you later change `dnsNames`, `issuerRef`, `duration`, `renewBefore`, or `rotationPolicy` on that same entry while `secretRef.name` is unchanged, the change is silently ignored — the operator only asks cert-manager to generate a certificate when the target Secret does not already exist, and it already exists from the earlier generation.

If you need a setting like `dnsNames` to actually take effect, either delete the existing Secret (and, for a clean slate, the `Certificate` object it came from) so the next reconcile finds nothing there and generates a fresh certificate with your updated settings, or point `secretRef.name` at a new, not-yet-existing Secret name instead.

### One certificate per Custom Resource

An auto-generated certificate is scoped to the CR that first causes it to be created — the operator stamps an ownership annotation on it and refuses to mount a cert generated for one CR into a different CR. This is unrelated to Secrets you supply yourself, which have no such restriction and may be referenced by multiple CRs if you choose to share them.

### SearchHeadCluster: deployer and search heads share one certificate

For `SearchHeadCluster`, `spec.certs` is declared once at the CR level and applies to **both** the deployer and the search head members — they are not independently configurable. When auto-generating, the operator requests SANs covering both the search heads and the deployer, so the deployer and the search heads end up sharing the same generated certificate and Secret. If you need the deployer and search heads to present different certificates, provide separate, pre-existing Secrets rather than relying on auto-generation for this case — the auto-generation path is only aware of a single shared cert per SearchHeadCluster CR.

## Certificate rotation

The operator itself never issues or renews a certificate's content — that is entirely up to whoever is providing the certificate: cert-manager (governed by `duration`/`renewBefore`/`rotationPolicy`) for auto-generated certs, or you for a self-managed Secret. What the operator does do, for every cert regardless of origin, is detect when a cert Secret's content changes and reconcile that new content out to the affected pods. This applies equally whether cert-manager renewed the Secret automatically or you updated a self-managed Secret by hand.

## Removing a certificate from spec.certs[]

Removing a `server` or `input` entry from `spec.certs[]` tells the operator to stop mounting and managing that certificate — it does not tell Splunk to stop using it. Splunk's TLS configuration for that role, and the certificate material itself, are not automatically cleared or reset when the entry is removed; Splunk keeps serving whatever certificate it was using immediately before the removal, indefinitely, with the operator no longer tracking, rotating, or reporting on it in any way. If you want a different outcome for that role going forward — reverting to Splunk's default behavior, or configuring TLS for it some other way — you need to set that role's TLS configuration yourself; the operator will not do it for you once the entry is gone.

## Security considerations

- **Same-namespace only.** `secretRef` has no namespace field — a CR can only reference a Secret in its own namespace. Cross-namespace references are not supported.
- **RBAC is the trust boundary, not a per-reference check.** The operator does not separately verify that whoever edited the CR has some additional right to the specific Secret or Issuer/ClusterIssuer referenced — it relies on standard Kubernetes RBAC on the CR itself. Anyone with RBAC to write a Splunk Enterprise CR in a namespace is trusted for any Secret/Issuer reference made from that namespace. Splunk Operator's deployment model assumes a namespace is not shared between mutually-untrusting tenants; if you need that isolation, enforce it with separate namespaces, not with finer-grained reference checks inside the operator.
- **Authorizing who can write CRs is your responsibility.** The operator ships the RBAC role manifests, but binding those roles to specific users/groups/service accounts — and therefore deciding who is allowed to cause the operator to mount or generate certificate material — is left to the cluster administrator via standard Kubernetes RBAC/admission control.
- **Auto-generated material is bound to its owning CR.** As described in [One certificate per Custom Resource](#one-certificate-per-custom-resource), a certificate the operator generates for you is tied to the requesting CR and is cleaned up when that CR (and any other CR tied to that certificate) is deleted.

## Encrypting secrets at rest

**This applies to every cert Secret described in this document**, whether you create it yourself or the operator auto-generates it via cert-manager — it is not specific to certificates.

By default, Kubernetes `Secret` objects are only **base64-encoded**, not encrypted, when they are written to etcd. Base64 is an encoding, not encryption — anyone with read access to the etcd data store, an etcd snapshot/backup, or a `kubectl get secret -o yaml` on a sufficiently privileged identity can trivially recover the plaintext `tls.key` and any other private key material stored in these Secrets.

**Hard requirement:** any cluster running Splunk Operator in production must enable encryption at rest for Secret resources before certificate Secrets are populated. See [Encrypting secrets at rest](PasswordManagement.md#encrypting-secrets-at-rest) in [Password Management](PasswordManagement.md) for the same requirement as it applies to the operator's other managed secrets.
