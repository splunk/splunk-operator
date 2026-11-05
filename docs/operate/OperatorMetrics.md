---
title: Operator Metrics
parent: Operate & Manage
nav_order: 10
---

# Operator metrics authentication

The manager's `/metrics` endpoint requires Kubernetes bearer-token authentication
and reader authorization. This is independent of product-usage `telemetry`.
The chart exposes port 8080 on
`<operator-name>-controller-manager-service.<operator-namespace>.svc`, where
`operator-name` is `splunkOperator.nameOverride` (default `splunk-operator`).

This change does **not** enable HTTPS by default. Configure verified HTTPS before
sending scraper tokens over a network. The manager already supports
`--metrics-secure=true` and `--metrics-cert-path=<mounted-serving-cert-directory>`;
the serving certificate must cover the hostname used by the scraper.

## Reader grants

Bind the scraper's existing ServiceAccount; the chart neither creates nor owns it:

```yaml
splunkOperator:
  metrics:
    readerServiceAccounts:
      - name: prometheus
        namespace: monitoring
```

The grant is `get` on the synthetic resource
`metrics.splunk.com/operatormetrics`; no CRD is needed. It grants neither Splunk
resource access nor API-server `/metrics` access. Existing unrelated grants still
apply. Administrators can provision equivalent reader RBAC independently.

When deploying following the operator-per-namespace method each namespace must be 
able to grant reader access independently, including to a central scraper that 
reads those multiple operators.

The default `/metrics` permission is cluster-wide and includes the Kubernetes API
server's metrics. We use a separate `operatormetrics` permission so each namespace
can control access to its operator's metrics.

Reader authorization is namespace-based, with no operator-specific resource name.
A namespaced reader grant covers matching operator metrics endpoints in that
namespace, including multiple operators installed there. A ClusterRoleBinding
grants access across namespaces. Distinct Role names do not provide per-operator
endpoint isolation within one namespace.

The chart always binds readers with a RoleBinding in the operator namespace.
With `clusterWideAccess=true`, it references the existing metrics-reader
ClusterRole; with `false`, it references a Role. Workload watch scope does not
grant access to other operators' metrics endpoints. A central scraper can be
listed as a reader in each operator installation. The filter authorizes
against `POD_NAMESPACE`, populated from the Pod's namespace by the Downward API,
regardless of `WATCH_NAMESPACE`. Custom deployment manifests must set it too;
missing `POD_NAMESPACE` prevents metrics startup.

## Upgrade existing reader grants

The authorization subject changes from the non-resource URL `/metrics` to
`get` on `metrics.splunk.com/operatormetrics`. Before upgrading, add this rule
for any externally managed scraper grants in each operator's installation
namespace. Keep the old grant during rollout, verify scraping through the
upgraded endpoint, then remove the obsolete `/metrics` grant if nothing else
uses it. The chart updates its own metrics-reader role automatically.

Chart-managed reader bindings now apply only in the operator namespace, even
when the operator watches multiple namespaces. List the scraper in each
installation it must read. Existing administrator-managed ClusterRoleBindings
still grant access across namespaces; review them separately if narrower access
is intended.

## Review delegation

TokenReview and SubjectAccessReview are cluster-scoped APIs. A namespaced Role
cannot delegate these calls. Cluster-wide installations retain chart-managed
review permissions. Namespace installations create **no review delegation RBAC
by default**; an administrator must separately provision it, for example:

```bash
kubectl create clusterrolebinding sok-metrics-review-operator-a \
  --clusterrole=system:auth-delegator \
  --serviceaccount=operator-a:splunk-operator-controller-manager
```

Alternatively, explicitly opt in to chart-managed cluster review delegation:

```yaml
splunkOperator:
  clusterWideAccess: false
  metrics:
    delegatedAuth:
      createClusterRBAC: true
```

The opt-in resources include the operator name and namespace in their names.
This option requires permission to provision ClusterRoles/ClusterRoleBindings;
it does not make a namespace installer a cluster administrator. Admission/webhook
bootstrap prerequisites still apply independently. In namespace mode,
`namespaceOverride` must be empty or match the Helm release namespace; the chart
rejects other values because watch and admission scope use the release namespace.

## Configure HTTPS for Helm

The chart's default transport remains HTTP. This opt-in example uses an existing
serving certificate and Helm post-rendering; it changes no chart defaults.
Complete the [Helm installation prerequisites](../deploy/Helm.md) first.
For the default operator name in namespace `operator-a`, obtain `server.crt` and
`server.key` with a certificate valid for
`splunk-operator-controller-manager-service.operator-a.svc`. The scraper needs
the issuing CA as `ca.crt`.

```bash
kubectl create namespace operator-a --dry-run=client -o yaml | kubectl apply -f -
kubectl -n operator-a create secret tls metrics-tls --cert=server.crt --key=server.key
cat > metrics-tls-postrenderer.sh <<'POSTRENDER'
#!/bin/sh
set -eu
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cat > "$work/resources.yaml"
cat > "$work/kustomization.yaml" <<'YAML'
resources: [resources.yaml]
patches:
  - target:
      group: apps
      version: v1
      kind: Deployment
      name: splunk-operator-controller-manager
    patch: |-
      - op: test
        path: /spec/template/spec/containers/0/name
        value: manager
      - op: add
        path: /spec/template/spec/containers/0/args/-
        value: --metrics-secure=true
      - op: add
        path: /spec/template/spec/containers/0/args/-
        value: --metrics-cert-path=/metrics-tls
      - op: add
        path: /spec/template/spec/volumes/-
        value:
          name: metrics-tls
          secret:
            secretName: metrics-tls
      - op: add
        path: /spec/template/spec/containers/0/volumeMounts/-
        value:
          name: metrics-tls
          mountPath: /metrics-tls
          readOnly: true
YAML
kubectl kustomize "$work"
POSTRENDER
chmod +x metrics-tls-postrenderer.sh
helm upgrade --install splunk-operator splunk/splunk-operator -n operator-a \
  -f your-operator-values.yaml \
  --set-string splunkOperator.splunkGeneralTerms="${SPLUNK_GENERAL_TERMS:?set your acknowledged terms value}" \
  --post-renderer ./metrics-tls-postrenderer.sh
```

Keep this renderer on subsequent Helm upgrades. The values file supplies your
existing access mode, reader accounts and review delegation. If you change
`splunkOperator.nameOverride` or the installation namespace, update the patch
target and certificate DNS name accordingly. Mount `ca.crt` in the scraper and
configure its HTTPS target and CA verification as below.

## Verify an HTTPS-configured endpoint

From the scraper Pod, use the public CA and the certificate's Service DNS name:

```bash
curl --fail-with-body --cacert /metrics-ca/ca.crt \
  -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
  https://splunk-operator-controller-manager-service.operator-a.svc:8080/metrics
```

| Result | HTTP status |
|---|---|
| Missing, malformed, invalid, expired or wrong-audience token | 401 |
| Completed negative TokenReview, including `status.error` | 401 |
| Valid identity without reader permission | 403 |
| Authorized reader | 200 |
| TokenReview/SAR API request failure (including missing review permission) | 500 |

Review request failures recover after API/permission restoration. Authentication
success/rejection is cached for up to one minute (rejections carrying
`status.error` and API failures are not cached). Authorization allows are cached
for five minutes and denials for 30 seconds; allow these windows when changing
grants, or use a fresh identity to test a change. Use projected, rotating tokens
accepted by the Kubernetes API server; tokens for another audience are rejected.
