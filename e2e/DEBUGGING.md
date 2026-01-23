# Debugging E2E Tests with VSCode

This guide covers how to debug E2E tests using VSCode's built-in debugger.

## Prerequisites

1. **VSCode Extensions** (install from Extensions marketplace):
   - [Go extension](https://marketplace.visualstudio.com/items?itemName=golang.Go) by Go Team at Google
   - Optional: [PlantUML](https://marketplace.visualstudio.com/items?itemName=jebbs.plantuml) for viewing diagrams

2. **Delve Debugger**:
   ```bash
   go install github.com/go-delve/delve/cmd/dlv@latest
   ```

3. **Kubernetes Cluster**:
   - EKS, GKE, or local cluster (kind, minikube)
   - `KUBECONFIG` set up correctly
   - Operator deployed in the cluster

## Quick Start: Debug a Test

### Method 1: Use Pre-configured Launch Configurations

1. Open VSCode in the project root
2. Press `F5` or go to **Run and Debug** panel
3. Select one of these configurations:
   - **E2E: Debug Runner (Fast Smoke)** - Runs smoke tests
   - **E2E: Debug Runner (Custom Spec)** - Debugs currently open YAML file
   - **E2E: Debug with Observability** - Runs with Neo4j + OTel enabled
   - **E2E: Debug Single Test with Tags** - Filter by tags

4. Set breakpoints in your code (click left margin or press `F9`)
5. Click **Start Debugging** (or press `F5`)

### Method 2: Debug Current File

1. Open any test spec YAML file (e.g., `e2e/specs/operator/smoke_fast.yaml`)
2. Press `F5` and select **E2E: Debug Runner (Custom Spec)**
3. The debugger will run with the currently open file

## Launch Configurations Explained

### 1. E2E: Debug Runner (Fast Smoke)

Runs the fast smoke test suite with debugger attached.

```json
{
  "name": "E2E: Debug Runner (Fast Smoke)",
  "program": "${workspaceFolder}/e2e/cmd/e2e-runner",
  "args": [
    "-cluster-provider", "eks",
    "-operator-namespace", "splunk-operator",
    "-skip-teardown",
    "${workspaceFolder}/e2e/specs/operator/smoke_fast.yaml"
  ]
}
```

**Use when:**
- Testing changes to the runner
- Debugging action handlers
- Understanding execution flow

**Key features:**
- `-skip-teardown` keeps resources after test for inspection
- Debug-level logging enabled

### 2. E2E: Debug Runner (Custom Spec)

Debugs whatever YAML file you currently have open.

```json
{
  "name": "E2E: Debug Runner (Custom Spec)",
  "args": [
    "-skip-teardown",
    "${file}"  // Uses currently open file
  ]
}
```

**Use when:**
- Writing a new test spec
- Debugging a specific failing test
- Iterating on a single test

**Workflow:**
1. Open your test YAML file
2. Press `F5` → Select "E2E: Debug Runner (Custom Spec)"
3. Breakpoints hit in runner code
4. Inspect variables, step through execution

### 3. E2E: Debug with Observability

Runs tests with full observability stack enabled.

```json
{
  "name": "E2E: Debug with Observability",
  "env": {
    "E2E_NEO4J_ENABLED": "true",
    "E2E_NEO4J_URI": "bolt://127.0.0.1:7687",
    "E2E_OTEL_ENABLED": "true",
    "E2E_OTEL_ENDPOINT": "127.0.0.1:4317"
  }
}
```

**Prerequisites:**
```bash
# Deploy observability stack first
cd e2e/observability/k8s
./deploy-observability.sh

# Port-forward services
kubectl port-forward -n observability svc/neo4j 7474:7474 7687:7687 &
kubectl port-forward -n observability svc/otel-collector 4317:4317 &
```

**Use when:**
- Testing graph export functionality
- Verifying metrics collection
- Debugging telemetry code

### 4. E2E: Debug Single Test with Tags

Filters tests by tags before running.

```json
{
  "name": "E2E: Debug Single Test with Tags",
  "args": [
    "-include-tags", "smoke",
    "${workspaceFolder}/e2e/specs/operator/*.yaml"
  ]
}
```

**Use when:**
- Running subset of tests (e.g., only `smoke`, `cluster-manager`)
- Debugging tag filtering logic
- Testing across multiple spec files

**Tag examples:**
- `-include-tags smoke` - Only smoke tests
- `-include-tags standalone,cluster-manager` - Multiple topologies
- `-exclude-tags slow` - Skip slow tests

## Debugging Techniques

### Setting Breakpoints

**In Runner Code:**
```go
// e2e/framework/runner/runner.go
func (r *Runner) runSpec(ctx context.Context, testSpec spec.TestSpec) results.TestResult {
    // Set breakpoint here to debug test execution
    r.logger.Info("starting test", zap.String("name", testSpec.Metadata.Name))

    // Breakpoint to inspect actions before execution
    for _, step := range testSpec.Tests {
        // ...
    }
}
```

**In Action Handlers:**
```go
// e2e/framework/steps/k8s_actions.go
func (a *K8sWaitForPodAction) Execute(ctx context.Context, env *steps.Environment) (map[string]interface{}, error) {
    // Breakpoint to debug pod waiting logic
    pods, err := a.kube.GetPods(ctx, namespace, labelSelector)

    // Inspect pod status
    for _, pod := range pods.Items {
        // Check conditions, status, etc.
    }
}
```

### Inspecting Variables

When breakpoint hits:

1. **Variables Panel** - Shows all local variables
2. **Watch Expressions** - Add custom expressions:
   ```
   testSpec.Metadata.Name
   env.Outputs
   ctx.Err()
   r.cfg.SkipTeardown
   ```

3. **Debug Console** - Evaluate expressions:
   ```go
   len(pods.Items)
   pod.Status.Phase
   string(testResult.Status)
   ```

### Conditional Breakpoints

Right-click breakpoint → **Edit Breakpoint** → Add condition:

```go
// Only break when specific test runs
testSpec.Metadata.Name == "standalone-deployment"

// Only break on errors
err != nil

// Break after N iterations
i > 5
```

### Logpoints

Instead of adding `fmt.Println()`, use logpoints:

Right-click line → **Add Logpoint** → Enter message:
```
Test status: {testResult.Status}, Duration: {testResult.Duration}
```

## Common Debugging Scenarios

### Scenario 1: Test Fails, Need to See Why

**Problem:** Test fails with cryptic error, need to understand execution flow.

**Solution:**
1. Set breakpoint at start of `runSpec()` in `runner/runner.go`
2. Run **E2E: Debug Runner (Custom Spec)** with your test file open
3. Step through (`F10` - step over, `F11` - step into)
4. Watch the `testResult` and `err` variables
5. When error occurs, inspect stack trace

### Scenario 2: Action Not Working as Expected

**Problem:** `splunk_search` action returns unexpected results.

**Solution:**
1. Find action handler in `e2e/framework/steps/splunk_actions.go`
2. Set breakpoint in `Execute()` method
3. Run test with debugger
4. When breakpoint hits:
   - Inspect `params` - Are they correct?
   - Check `env.Outputs` - Previous step outputs available?
   - Step into Splunk client calls
   - Examine raw API responses

### Scenario 3: Topology Not Deploying

**Problem:** Test hangs during topology deployment.

**Solution:**
1. Set breakpoint in `topology/builder.go` at `Build()` method
2. Check what resources are being created
3. Set breakpoint in `k8s/client.go` at `Create()` calls
4. Inspect Kubernetes API responses
5. Use Debug Console to query cluster:
   ```go
   kube.GetPods(ctx, namespace, "")
   ```

### Scenario 4: Understanding Variable Substitution

**Problem:** Variables like `${search_result.count}` not resolving.

**Solution:**
1. Set breakpoint in `runner/runner.go` at variable resolution code
2. Find where `env.Outputs` is populated
3. Trace how outputs from one action become inputs to next
4. Watch `env.Outputs` in Variables panel

### Scenario 5: Debugging Neo4j Export

**Problem:** Data not appearing in Neo4j graph.

**Solution:**
1. Ensure observability stack is running
2. Use **E2E: Debug with Observability** configuration
3. Set breakpoint in `graph/exporter.go` at `Export()` method
4. Check Neo4j connection status
5. Inspect graph node/relationship creation
6. Verify Cypher queries being executed

## Advanced: Remote Debugging

Debug tests running in a pod on the cluster:

### Step 1: Build Debug Binary

```bash
# Build with debug symbols
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -gcflags="all=-N -l" \
  -o bin/e2e-runner-debug \
  ./e2e/cmd/e2e-runner
```

### Step 2: Create Debug Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: e2e-debug
  namespace: splunk-operator
spec:
  containers:
  - name: debug
    image: golang:1.22
    command: ["/bin/sh", "-c", "sleep infinity"]
    volumeMounts:
    - name: binary
      mountPath: /app
  volumes:
  - name: binary
    hostPath:
      path: /path/to/bin
```

### Step 3: Copy Binary and Start Delve

```bash
kubectl cp bin/e2e-runner-debug splunk-operator/e2e-debug:/app/

kubectl exec -it e2e-debug -n splunk-operator -- sh
cd /app
dlv exec ./e2e-runner-debug --headless --listen=:2345 --api-version=2 -- \
  -cluster-provider eks \
  -operator-namespace splunk-operator \
  /app/specs/smoke_fast.yaml
```

### Step 4: Connect VSCode

Add to `launch.json`:
```json
{
  "name": "Remote Debug E2E",
  "type": "go",
  "request": "attach",
  "mode": "remote",
  "remotePath": "/app",
  "port": 2345,
  "host": "localhost"
}
```

Port-forward and attach:
```bash
kubectl port-forward e2e-debug 2345:2345 -n splunk-operator
```

Then press `F5` → Select "Remote Debug E2E"

## Tips and Tricks

### 1. Skip Teardown for Inspection

Always use `-skip-teardown` when debugging so you can inspect resources after test:

```bash
kubectl get pods -n <test-namespace>
kubectl logs <pod-name> -n <test-namespace>
kubectl describe standalone <name> -n <test-namespace>
```

### 2. Use Debug Log Level

Set `E2E_LOG_LEVEL=debug` to see detailed logs:

```json
"env": {
  "E2E_LOG_LEVEL": "debug"
}
```

### 3. Combine with Artifacts

After debug session, check artifacts:
```bash
cat artifacts/results.json | jq '.tests[] | select(.status=="failed")'
open artifacts/test-sequence-*.png
```

### 4. Debug Individual Actions

To test a single action in isolation:

```go
// Create test file: e2e/cmd/test-action/main.go
package main

import (
    "context"
    "github.com/splunk/splunk-operator/e2e/framework/steps"
    "github.com/splunk/splunk-operator/e2e/framework/k8s"
)

func main() {
    ctx := context.Background()
    kube, _ := k8s.NewClient("")

    action := &steps.K8sWaitForPodAction{
        Kube: kube,
        Params: map[string]interface{}{
            "label_selector": "app=splunk",
            "timeout": "600s",
        },
    }

    // Set breakpoint here
    result, err := action.Execute(ctx, &steps.Environment{})
    _ = result
    _ = err
}
```

Add launch config:
```json
{
  "name": "Debug Single Action",
  "type": "go",
  "request": "launch",
  "mode": "debug",
  "program": "${workspaceFolder}/e2e/cmd/test-action"
}
```

### 5. Use Test Fixtures

Create minimal test specs for debugging:

```yaml
# e2e/specs/debug/minimal.yaml
metadata:
  name: debug-test
  tags: [debug]

tests:
  - name: "Simple test"
    actions:
      - action: k8s_exec
        params:
          pod_selector: "app=splunk"
          command: "echo hello"
        output: result

      - action: assert_equals
        params:
          actual: ${result.stdout}
          expected: "hello\n"
```

## Troubleshooting Debugger Issues

### Debugger Won't Start

**Error:** "Could not find Go"

```bash
# Install Go extension
code --install-extension golang.Go

# Ensure Go is in PATH
which go
```

### Breakpoints Not Hitting

1. **Build without optimizations:**
   ```bash
   go build -gcflags="all=-N -l" -o bin/e2e-runner ./e2e/cmd/e2e-runner
   ```

2. **Check breakpoint is on executable line** (not comment/blank line)

3. **Verify breakpoint is in code path:**
   - Add logpoint first to confirm code executes
   - Check conditional breakpoints aren't too restrictive

### Timeout Errors During Debug

Kubernetes operations may timeout while stepping through code:

**Solution:** Increase timeouts in test spec:
```yaml
actions:
  - action: k8s_wait_for_pod
    params:
      timeout: 3600s  # 1 hour for debugging
```

### Can't See Variable Values

**Error:** "Variable optimized out"

This happens with optimized builds. Rebuild without optimizations:
```bash
go build -gcflags="all=-N -l" ./e2e/cmd/e2e-runner
```

## VSCode Keyboard Shortcuts

| Action | Shortcut |
|--------|----------|
| Start Debugging | `F5` |
| Stop Debugging | `Shift+F5` |
| Restart | `Ctrl+Shift+F5` |
| Continue | `F5` |
| Step Over | `F10` |
| Step Into | `F11` |
| Step Out | `Shift+F11` |
| Toggle Breakpoint | `F9` |
| Debug Console | `Ctrl+Shift+Y` |

## Additional Resources

- [VSCode Go Debugging Docs](https://github.com/golang/vscode-go/wiki/debugging)
- [Delve Documentation](https://github.com/go-delve/delve/tree/master/Documentation)
- [E2E Framework Architecture](./ARCHITECTURE.md)
- [Writing Tests Guide](./QUICK_START.md)
