# E2E Framework (Next-Gen)

This directory contains the new spec-driven E2E framework designed for large-scale test suites.

## Goals

- Modular execution with step registry
- Spec-driven tests to scale to thousands of cases
- Structured logs, metrics, and knowledge graph output
- Clean separation between data, topology, and assertions

## Running

Basic run (loads specs under `e2e/specs`):

```
E2E_SPEC_DIR=./e2e/specs E2E_DATASET_REGISTRY=./e2e/datasets/datf-datasets.yaml \
  go run ./e2e/cmd/e2e-runner
```

Key env vars:

- `E2E_SPEC_DIR`: directory containing spec files
- `E2E_DATASET_REGISTRY`: dataset registry YAML
- `E2E_ARTIFACT_DIR`: where to write results/graph/metrics
- `E2E_CAPABILITIES`: comma-separated capability list to enable optional tests
- `E2E_TOPOLOGY_MODE`: `suite` (default) or `test` for per-test topology
- `E2E_LOG_COLLECTION`: `failure` (default), `always`, or `never`
- `E2E_SPLUNK_LOG_TAIL`: tail N lines of Splunk internal logs (0 = full file)
- `E2E_OTEL_ENABLED`: enable OTLP metrics/traces
- `E2E_OTEL_ENDPOINT`: OTLP gRPC endpoint (host:port)
- `E2E_OTEL_HEADERS`: OTLP headers as comma-separated key=value pairs
- `E2E_OTEL_INSECURE`: disable TLS for OTLP endpoint (default true)
- `E2E_OTEL_SERVICE_NAME`: service name for OTel resources
- `E2E_OTEL_RESOURCE_ATTRS`: extra OTel resource attributes as key=value pairs
- `E2E_NEO4J_ENABLED`: enable Neo4j graph export
- `E2E_NEO4J_URI`: Neo4j connection URI
- `E2E_NEO4J_USER`: Neo4j username
- `E2E_NEO4J_PASSWORD`: Neo4j password
- `E2E_NEO4J_DATABASE`: Neo4j database name (default `neo4j`)

DATF dataset support (objectstore-backed):

- `DATF_S3_BUCKET`: bucket name containing DATF datasets
- `DATF_S3_PREFIX`: prefix path for dataset objects (include trailing slash if needed)
- `S3_REGION` or `AWS_REGION`: region for S3 access

Dataset sources can be `s3`, `gcs`, `azure`, `minio`, or `objectstore`; use `E2E_OBJECTSTORE_*` for credentials and optional per-dataset overrides via `settings` (for example `objectstore_endpoint`).

Object store access (used by objectstore.* steps and dataset fetch):

- `E2E_OBJECTSTORE_PROVIDER`: `s3`, `gcs`, or `azure`
- `E2E_OBJECTSTORE_BUCKET`: bucket/container name
- `E2E_OBJECTSTORE_PREFIX`: base prefix for object keys
- `E2E_OBJECTSTORE_REGION`: region (S3)
- `E2E_OBJECTSTORE_ENDPOINT`: endpoint override (S3/Azure)
- `E2E_OBJECTSTORE_ACCESS_KEY`: access key (S3)
- `E2E_OBJECTSTORE_SECRET_KEY`: secret key (S3)
- `E2E_OBJECTSTORE_SESSION_TOKEN`: session token (S3)
- `E2E_OBJECTSTORE_S3_PATH_STYLE`: set to `true` for path-style S3 endpoints
- `E2E_OBJECTSTORE_GCP_PROJECT`: GCP project ID
- `E2E_OBJECTSTORE_GCP_CREDENTIALS_FILE`: path to GCP credentials JSON
- `E2E_OBJECTSTORE_GCP_CREDENTIALS_JSON`: raw GCP credentials JSON
- `E2E_OBJECTSTORE_AZURE_ACCOUNT`: Azure storage account name
- `E2E_OBJECTSTORE_AZURE_KEY`: Azure storage account key
- `E2E_OBJECTSTORE_AZURE_ENDPOINT`: Azure blob endpoint override
- `E2E_OBJECTSTORE_AZURE_SAS_TOKEN`: Azure SAS token (optional, use instead of key)

Regenerate the DATF dataset registry from core_datf conftests:

```
python3 e2e/tools/datf_extract.py --qa-root /path/to/splunkd/qa \
  --output e2e/datasets/datf-datasets.yaml
```

Artifacts are written to `e2e/artifacts/<run-id>` by default.

## Spec Variants

Use `variants` to clone a base spec with different names/tags (and optional topology params) without duplicating steps:

```yaml
apiVersion: e2e.splunk.com/v1
kind: Test
metadata:
  name: operator_secret_s1_update
  tags: [operator, secret, s1, integration]
variants:
  - name: operator_secret_manager_s1_update
    tags: [managersecret]
  - name: operator_secret_master_s1_update
    tags: [mastersecret]
steps:
  - name: deploy
    action: topology.deploy
```

`step_overrides` can update specific steps for a variant without duplicating the full spec:

```yaml
variants:
  - name: operator_secret_master_c3_update
    step_overrides:
      - name: deploy
        with:
          cluster_manager_kind: master
          license_manager_ref: null
      - name: deploy_license_manager
        action: splunk.license_master.deploy
```

When `replace: true` is set, the step is replaced entirely. Otherwise, `with` keys merge, and `null` removes a key.

## App Framework

Use the app framework steps to build and apply AppFrameworkSpec settings:

```yaml
steps:
  - name: appframework_spec
    action: appframework.spec.build
    with:
      provider: s3
      bucket: ${E2E_OBJECTSTORE_BUCKET}
      prefix: apps/
      volume_name: app-volume
      app_source_name: appsource
      location: release
  - name: apply_appframework
    action: appframework.apply
    with:
      target_kind: clustermanager
      target_name: ${cluster_manager_name}
      spec_path: ${last_appframework_spec_path}
      replace: true
```

If `provider`, `bucket`, or credentials are omitted, the `E2E_OBJECTSTORE_*` settings are used.

## Observability

- Metrics and traces export over OTLP when OTel is enabled, so you can route to Prometheus/Tempo with an OTel Collector.
- Logs are written to artifacts; ship them to Loki with promtail/agent if desired.
- Graph export pushes `graph.json` data to Neo4j for querying and support analysis.
