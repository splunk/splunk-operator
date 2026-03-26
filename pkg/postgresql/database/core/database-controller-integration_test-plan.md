# PostgresDatabase Controller Integration Test Plan

> Temporary location only. Final long-term home is TBD.

## Goal

Stabilize and complete `envtest`-based controller integration coverage for the
`PostgresDatabase` reconciliation path without running the tests in this turn.

Primary files involved:

- `internal/controller/suite_test.go`
- `internal/controller/postgresdatabase_controller_test.go`
- `internal/controller/postgresdatabase_controller.go`
- `pkg/postgresql/database/core/database.go`

## Current State

The current work completed the targeted `PostgresDatabase` controller `envtest`
coverage and the focused suite passes locally.

1. `internal/controller/suite_test.go` now:
   - resolves the CloudNativePG module directory with `go list`
   - adds CNPG CRD bases to `envtest`
   - registers CNPG types in the scheme
   - does **not** register `PostgresDatabaseReconciler` with the shared manager, which is good for manual deterministic tests

2. `internal/controller/postgresdatabase_controller_test.go` now has focused controller specs for:
   - missing cluster path
   - ready cluster path with manual `Reconcile(...)` calls
   - role conflict path
   - deletion / reclaim policy path
   - pooler endpoint configmap path

3. The main findings from implementation and local verification were:
   - the happy path needs four reconciles, not three
   - `buildManagedRolesPatch(...)` must use explicit GVK rather than relying on fetched `TypeMeta`
   - status fields on seeded `PostgresCluster` fixtures must be written through the status subresource after create
   - deletion/reclaim tests must seed `managedRoles` via SSA ownership that matches production behavior, otherwise cleanup patches cannot remove deleted roles

## Known Issues To Fix First

### 1. The happy-path test currently expects the wrong reconcile phase

The current "ready cluster" spec expects a CNPG `Database` to exist immediately
after the reconcile that patches managed roles.

That is not how `PostgresDatabaseService(...)` behaves.

Actual order in `pkg/postgresql/database/core/database.go`:

1. add finalizer and return
2. validate cluster
3. create secrets
4. create ConfigMaps
5. patch `PostgresCluster.spec.managedRoles` if roles are missing
6. return `RequeueAfter: retryDelay`
7. only on a later reconcile, once roles are present and reconciled, create CNPG `Database`
8. only on a later reconcile, once CNPG `Database.status.applied == true`, finish status and set `ObservedGeneration`

Implication:

- the current happy-path spec needs at least **four** reconciles, not three

### 1a. `buildManagedRolesPatch(...)` must not depend on fetched TypeMeta

Confirmed from the focused test failure:

- `patching managed roles for PostgresDatabase ready-cluster: Object 'Kind' is missing in 'unstructured object has no kind'`

Root cause:

- `buildManagedRolesPatch(...)` builds an `unstructured.Unstructured` using
  `cluster.APIVersion` and `cluster.Kind`
- after reading an object back from the API, those fields are not reliable for
  this use
- that produced a server-side-apply patch without `kind`, and the patch failed

Why we changed it:

- this is a production correctness fix, not a test-only workaround
- server-side apply patches must carry explicit GVK
- the implementation should use stable values:
  - `apiVersion: enterprisev4.GroupVersion.String()`
  - `kind: "PostgresCluster"`
- setting `TypeMeta` only in the test would hide a real bug in the application service

### 2. The missing-cluster spec still combines two concerns

The current test verifies:

- finalizer addition
- missing-cluster status/requeue

This can work, but it is cleaner and easier to debug if you split it into:

1. `adds finalizer on first reconcile`
2. `requeues with ClusterNotFound once finalizer already exists`

Recommendation: split these two behaviors into separate `It(...)` blocks later if
you want sharper failure isolation. This is now optional, not blocking.

### 3. `envtest` prerequisites must be explicit when you run locally

The controller suite depends on:

- `bin/setup-envtest`
- downloaded Kubernetes API server / etcd assets
- `KUBEBUILDER_ASSETS`

Important clarification:

- for these `internal/controller` `envtest` tests, you do **not** need a real Kubernetes cluster
- you do **not** need CloudNativePG / postgres operator running in a cluster
- you do **not** need PostgreSQL running
- `envtest` starts a local API server and etcd process on your machine and installs CRDs into that ephemeral control plane
- the error you hit is specifically about missing local `envtest` binaries, not about a missing cluster

Use the repo-standard setup before running:

```bash
make setup-envtest
```

Then run the focused suite with a writable Go cache:

```bash
mkdir -p /tmp/gocache /tmp/gotmp
ASSETS_DIR="$PWD/bin/k8s/1.34.1-darwin-arm64"
env \
  GOCACHE=/tmp/gocache \
  GOTMPDIR=/tmp/gotmp \
  KUBEBUILDER_ASSETS="$ASSETS_DIR" \
  go test ./internal/controller -run TestAPIs -count=1 -args \
  -ginkgo.focus='PostgresDatabase Controller' \
  -ginkgo.v \
  -ginkgo.trace
```

If the asset version differs on your machine, resolve it first and then use the
absolute path:

```bash
./bin/setup-envtest use 1.34.1 --bin-dir ./bin -p path
```

Important:

- do not put `export` inside the `env ... go test` command
- prefer an absolute path in `KUBEBUILDER_ASSETS`
- if you still see `/usr/local/kubebuilder/bin/etcd` in the error, the `go test` process did not receive `KUBEBUILDER_ASSETS`

## Detailed Implementation Plan

### Step 1. Tighten `suite_test.go` and leave it deterministic

File:

- `internal/controller/suite_test.go`

Actions:

1. Keep CNPG CRD installation and scheme registration.
2. Keep `PostgresDatabaseReconciler` **out** of the shared manager setup.
3. Leave the tests in `postgresdatabase_controller_test.go` as manual
   reconciler invocations.

Expected result:

- no background reconcile races
- deterministic state transitions inside each spec

### Step 2. Rewrite the ready-cluster spec around the real reconcile sequence

File:

- `internal/controller/postgresdatabase_controller_test.go`

Use this exact sequence.

#### Phase A: Create base objects

Create:

1. namespace
2. `PostgresDatabase`
3. `PostgresCluster`
   - `spec.class = "dev"`
   - no `managedRoles` preset
4. CNPG `Cluster`
   - minimal valid spec
   - `status.managedRolesStatus.byStatus[reconciled] = ["appdb_admin", "appdb_rw"]`
   - `status.writeService = "tenant-rw"`
   - `status.readService = "tenant-ro"`

Also set `PostgresCluster.status`:

- `phase = "Ready"`
- `provisionerRef` -> CNPG cluster

#### Phase B: Reconcile #1

Call `Reconcile(...)`.

Expected result:

- finalizer is added
- result is empty
- no secrets/configmaps/CNPG databases are required yet

#### Phase C: Seed status to skip live grant logic

Fetch `PostgresDatabase` and set:

```go
status.databases = []enterprisev4.DatabaseInfo{{Name: "appdb"}}
```

Why:

- `hasNewDatabases(...)` checks whether the spec contains a database missing
  from `status.databases`
- this is the seam that avoids reaching the live SQL adapter path

#### Phase D: Reconcile #2

Call `Reconcile(...)` again.

Expected result:

1. cluster validation passes
2. secrets are created
3. ConfigMaps are created
4. `PostgresCluster.spec.managedRoles` is patched by the service
5. result is `RequeueAfter == 15 * time.Second`

Assertions after Reconcile #2:

- admin secret exists
- RW secret exists
- ConfigMap exists
- ConfigMap has:
  - `rw-host`
  - `ro-host`
  - `admin-user`
  - `rw-user`
- owner references on the two secrets and ConfigMap point to the `PostgresDatabase`
- fetch `PostgresCluster` and assert `spec.managedRoles` contains:
  - `appdb_admin`
  - `appdb_rw`

Important:

- do **not** expect CNPG `Database` yet

#### Phase E: Reconcile #3

Call `Reconcile(...)` a third time.

Expected result:

1. roles are now present in `PostgresCluster.spec`
2. CNPG cluster status says those roles are reconciled
3. service creates CNPG `Database`
4. `verifyDatabasesReady(...)` sees `status.applied == nil/false`
5. result is `RequeueAfter == 15 * time.Second`

Assertions after Reconcile #3:

- CNPG `Database` exists
- `spec.name == "appdb"`
- `spec.owner == "appdb_admin"`
- `spec.clusterRef.name == cnpgClusterName`
- owner reference points to the `PostgresDatabase`

#### Phase F: Mark CNPG Database as ready

Fetch CNPG `Database` and set:

```go
applied := true
cnpgDatabase.Status.Applied = &applied
```

Update the status subresource.

#### Phase G: Reconcile #4

Call `Reconcile(...)` a fourth time.

Expected result:

- result is empty
- `status.phase == "Ready"`
- `ObservedGeneration == metadata.generation`
- `status.databases` is populated with secret/configmap refs
- `ClusterReady`, `SecretsReady`, `ConfigMapsReady`, `RolesReady`, and
  `DatabasesReady` are `True`
- `PrivilegesReady` should still be absent because the live grant step was skipped

### Step 3. Completed focused controller coverage

The following `PostgresDatabase` controller cases are now implemented and passed
in the focused suite:

1. missing cluster path
2. ready cluster happy path without live SQL grants
3. role conflict path
4. deletion / reclaim policy path
5. pooler endpoint configmap path

## Suggested Local Execution Order

1. Ensure envtest assets exist with `make setup-envtest`.
2. Run only the focused PostgreSQL controller specs.

## Exit Criteria

You can consider this task done when:

1. the focused `PostgresDatabase Controller` specs pass locally
2. the happy-path spec matches the real multi-reconcile state machine
3. the tests do not depend on the live DB adapter path
4. the suite remains deterministic without background manager reconciles for `PostgresDatabase`

## Verified Findings

These were confirmed during implementation and local focused test runs:

1. `envtest` failures mentioning `/usr/local/kubebuilder/bin/etcd` were caused by
   missing `KUBEBUILDER_ASSETS`, not by a missing Kubernetes cluster.
2. `buildManagedRolesPatch(...)` needed explicit GVK to make SSA patches valid.
3. CNPG `Database` creation happens one reconcile later than managed-role patching.
4. For conflict fixtures, `PostgresCluster.Status` must be updated after create;
   embedding status in the create object was not sufficient for the tested path.
5. For deletion fixtures, `managedRoles` had to be seeded with SSA ownership
   matching production semantics; plain create ownership prevented expected role removal.
