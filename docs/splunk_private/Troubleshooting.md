---
title: Troubleshooting
parent: Internal Onboarding
nav_order: 4
---

# Troubleshooting

## Integration Test Debugging flow

Follow the steps in this section to run and debug integration tests on your local machine. Cluster bootstrap intentionally left as a Kraken placeholder for now.

1. Prepare a disposable integration-test cluster.
   - Follow the [Kraken setup](https://kraken.splunkdev.page/kraken-docs/), currently in a Closed Beta phase. <!-- TODO: update this with how to create or claim the cluster, how to set kubeconfig, the expected namespace/operator prerequisites, and the correct `TEST_CLUSTER_PLATFORM` value when the docs are finalized. -->
   - Confirm `kubectl` is pointed at the test cluster and that the operator namespace and operator deployment are ready before running suites.
     ```bash
     kubectl config current-context
     ```

2. Configure the local repo for test runs.
   - Open the repo in your preferred editor or IDE, or work directly from a terminal.
   - Make sure your editor, IDE, or shell uses the repo's Go toolchain version.
   - Create a local `test.env` for credentials and suite inputs. Keep secrets out of commits.
   - Start with the values required by the suite being debugged, such as:

     ```bash
     TEST_CLUSTER_PLATFORM=<kraken-platform-value>
     S3_REGION=us-west-2
     TEST_S3_BUCKET=splk-test-data-bucket
     TEST_BUCKET=splk-test-data-bucket
     TEST_INDEXES_S3_BUCKET=splk-integration-data-bucket
     ENTERPRISE_LICENSE_LOCATION=test_licenses/
     ```

   - Populate only the platform, bucket, and license values required by the suite being debugged.

3. Configure your test and debug runner.
   - Whether you use the CLI, GoLand, VS Code, or another Go-capable IDE, configure the runner to use:
      - A long test timeout, such as `1200m`.
      - Verbose test output.
      - The environment variables from your local `test.env` file.
   - For CLI runs, load `test.env` in your shell before starting the suite:

     ```bash
     set -a
     source ./test.env
     set +a
     ```

   - For IDE runs, configure the equivalent test timeout, verbose output, and environment-file support. In VS Code, that can be done with `.vscode/settings.json`:

     ```json
     {
       "go.testTimeout": "1200m",
       "go.testFlags": ["-v"],
       "go.testEnvFile": "${workspaceFolder}/test.env"
     }
     ```

   - For operator debugging, create a launch/debug configuration in your IDE when needed. In the current repo, point the debug target at `cmd/main.go` and set `RELATED_IMAGE_SPLUNK_ENTERPRISE` to the Splunk Enterprise image under test. In VS Code, that can be done with `.vscode/launch.json`:
      ```json
      {
         // Use IntelliSense to learn about possible attributes.
         // Hover to view descriptions of existing attributes.
         // For more information, visit: https://go.microsoft.com/fwlink/?linkid=830387
         "version": "0.2.0",
         "configurations": [
            {
               "name": "Debug Package",
               "type": "go",
               "request": "launch",
               "mode": "debug",
               "program": "${workspaceFolder}/cmd/main.go",
               "env": {
                  "RELATED_IMAGE_SPLUNK_ENTERPRISE":"splunk/splunk:X.X.X"
               },
            }
         ]
      }
      ```

4. Isolate one test.
   - Find the target suite under `test/` and identify the `*_test.go` case plus its `*_suite_test.go` entry point.
   - Prefer a narrow Ginkgo focus through your runner or CLI. See the public [Integration Testing](../develop/IntegrationTesting.md#run-a-specific-test-by-name-or-label) doc for more examples.
   - If your runner cannot pass Ginkgo focus flags reliably, or you are using an IDE, temporarily add `F` before `Context` or `It` to focus the case, or use `XContext` or `XIt` to skip cases that should not run. **Remove any temporary focus or skip markers before committing**.

5. Run or debug the suite.
   - Start from the suite's `Test...` entry point with your CLI command or IDE test action.
   - In IDEs that provide inline run/debug controls, such as VS Code CodeLens or GoLand gutter actions, use the control above the suite entry point.
   - When debugging, set breakpoints on or before the suspected failure and step through variable values, CR state, helper calls, and teardown behavior.

6. Capture a CLI run when you need a durable log.
   - Capture the equivalent `go test` command from your runner output or run a suite directly:

     ```bash
     set -a
     source ./test.env
     set +a
     /opt/homebrew/bin/go test -timeout 1200m -run "^TestBasic$" github.com/splunk/splunk-operator/test/appframework_aws/s1 -v > /tmp/sok-test-output.log 2>&1 &
     ```

   - Tail the output as the test runs:

     ```bash
     tail -f /tmp/sok-test-output.log
     ```

   - Suggestion: Add targeted debug output when breakpoints are not enough.
      - Add temporary logs near the suspected hang or failure, using a unique token such as your name or ticket key.
      - Prefer `GinkgoWriter.Printf(...)` or the testenv logger in `It`, `BeforeEach`, and helper paths.
      - `tail -f /tmp/sok-test-output.log | rg "<identifier>"` can filter the stream.
      - Remove noisy temporary logs once the failure mode is understood, unless they are useful permanent diagnostics.

7. Inspect the cluster status when a breakpoint is hit.
   - Be cautious not to influence its state. Only look at what alredy exists without changing it.
   - Set the namespace from the paused test's log output:

      ```bash
      export TEST_NAMESPACE=<test-namespace>
      ```

   - Inspect the Splunk resources and Kubernetes resources owned by the test namespace:

      ```bash
      kubectl get standalones,searchheadclusters,indexerclusters,clustermanagers,clustermasters,monitoringconsoles,licensemanagers,licensemasters,ingestorclusters,objectstorages,queues -n "$TEST_NAMESPACE" -o wide
      kubectl get pods,statefulsets,services,persistentvolumeclaims,configmaps,secrets -n "$TEST_NAMESPACE" -o wide
      kubectl get events -n "$TEST_NAMESPACE" --sort-by='.lastTimestamp'
      ```

   - Drill into a stuck Splunk CR, pod, or StatefulSet:

      ```bash
      kubectl describe <splunk-cr-kind> -n "$TEST_NAMESPACE" <name>
      kubectl get <splunk-cr-kind> -n "$TEST_NAMESPACE" <name> -o yaml
      kubectl describe pod -n "$TEST_NAMESPACE" <pod-name>
      kubectl describe statefulset -n "$TEST_NAMESPACE" <statefulset-name>
      ```

   - Check operator logs while execution is paused:

      ```bash
      kubectl logs -n splunk-operator deployment/splunk-operator-controller-manager -c manager --tail=200
      ```

8. Stop a stuck run deliberately.
   - In an IDE, use its stop or cancel-test action. In VS Code, use `Cmd + Shift + P` and `Go: Cancel Running Tests`.
   - From a terminal, find the `go test` or `ginkgo` process and stop it:

     ```bash
     ps -ef | rg "go test|ginkgo"
     kill <pid>
     ```

   - If the process ignores a normal stop signal, use `kill -9 <pid>`.
   - A canceled run usually skips normal teardown, so assume cluster leftovers exist.

9. Clean the test cluster before the next run.
   - Run `./tools/cleanup.sh` from the repo when a test hangs, is canceled, or leaves CRs/namespaces behind.
   - Re-check namespaces, pods, PVCs, and Splunk CRs before rerunning the same case.
   - It might be useful to run `go clean -testcache` to be able to re-run the test case.

## Helpful AI Prompts

To connect to GitLab, Jira, and other internal systems, setup [Ghost](http://go/ai-handbook), and make sure to authenticate before running any of the prompts.

This repo includes an [AGENTS.md](../../AGENTS.md) file that includes information helpful to AI tools to run, debug, and contribute to SOK. Use one of the following prompts to start debugging using the issue you are seeing.

### Debug A Unit Or Integration Test

```
I’m debugging a failing Splunk Operator test. Use this repo as the source of truth.

Test/package:
<test name, package, or file>

Failure output:
<paste logs>

Please:
1. Identify the likely failing assertion or setup step.
2. Trace the relevant code path through api/, internal/controller/, pkg/splunk/, and test/testenv/ as needed.
3. Check whether this looks like a product bug, test flake, environment issue, or bad test expectation.
4. Suggest the smallest fix and the verification command I should run.
```

```
I want you to be conservative. Don’t edit code yet.

Given this test failure:
<paste test logs, pod events, or CI job link>

Please debug it like a Splunk Operator maintainer:
- What failed, in plain English
- The likely controller/resource involved
- Search for recent or nearby code that affects that behavior
- Whether the failure is deterministic or flaky
- The most suspicious files/functions/root causes
- A minimal debugging plan
- The exact test command to rerun, ideally scoped to the failing package/spec
```

### Debug A Bug or Customer Issue

```
I’m debugging a customer issue with Splunk Operator. Use the repo, docs, and known operator behavior as the source of truth.

Customer symptom:
<symptom>

Environment/context:
<operator version, CR kind/version, Kubernetes provider, namespace, Splunk image, relevant config>

Logs/events/YAML:
<paste CR, operator logs, pod events, describe output>

Please:
1. Summarize what is happening.
2. Map the symptom to the likely reconciliation path.
3. Identify missing evidence I should collect.
4. Suggest likely causes, ranked by confidence.
5. Give customer-safe remediation steps and any risks.
```

```
Help me turn this customer report into a structured investigation.

Customer report:
<paste report>

Please extract:
- A concise problem statement
- Impact and urgency
- Affected Splunk Operator resources
- Relevant CR fields/configuration
- Logs/events still needed
- Possible operator bug vs customer configuration issue
- A step-by-step debugging checklist
```


## Links to troubleshooting pages

- [Support Main Troubleshooting Decision Tree](https://splunk.atlassian.net/wiki/spaces/SUP/pages/1079054074643/Quick+Start+-+Main+Troubleshooting+Decision+Tree)
