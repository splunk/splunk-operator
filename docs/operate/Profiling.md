---
title: Operator Profiling
parent: Operate & Manage
nav_order: 10
---

# Profile the Splunk Operator

The Splunk Operator supports Go runtime profiling through the standard
`/debug/pprof/` handlers. Profiling is disabled by default because profiles can
contain sensitive process details and collecting them adds runtime overhead.

## Enable pprof

### Helm

Enable profiling with Helm when investigating a specific performance problem:

```yaml
splunkOperator:
  pprof:
    enabled: true
```

The profiler uses a dedicated listener at `127.0.0.1:6060` by default. This
secure default keeps pprof off the pod network. Setting
`splunkOperator.pprof.bindAddress` to a non-loopback address intentionally
overrides that default and can expose sensitive profile data over the pod
network. Only do this when external authentication and network access controls
protect the endpoint.

### Kustomize local development

For a local source deployment, the `make deploy` target renders the Kustomize
manifests and can append a manager argument. Enable profiling with:

```bash
make deploy IMG=<operator-image> \
  SPLUNK_GENERAL_TERMS="<required value>" \
  MANAGER_EXTRA_ARG="--pprof=true"
```

The appended argument overrides the base manifest's explicit `--pprof=false`
setting. Use the same `kubectl port-forward` workflow described below to reach
the default loopback listener.

### Direct binary

Direct binary deployments can use `--pprof`. Use `--pprof-bind-address` to
choose a different listener address. A non-loopback address intentionally
overrides the secure default and can expose pprof over the network; use one only
when external authentication and network access controls protect the endpoint.
Use `--pprof-bind-address=0` to explicitly disable pprof.

## Collect a profile

Forward the dedicated listener to your workstation:

```bash
kubectl -n <namespace> port-forward \
  deployment/splunk-operator-controller-manager 6060:6060
```

In another terminal, collect and inspect a 30-second CPU profile:

```bash
go tool pprof \
  'http://127.0.0.1:6060/debug/pprof/profile?seconds=30'
```

Use the heap endpoint for memory investigation:

```bash
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
```

Disable profiling after the investigation:

```yaml
splunkOperator:
  pprof:
    enabled: false
```
