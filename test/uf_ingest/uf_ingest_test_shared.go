// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package ufingest

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

// repoRoot returns the absolute path to the splunk-operator repo root by
// walking up from this source file, so helm can find the chart regardless
// of where ginkgo sets the working directory.
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile is .../test/uf_ingest/uf_ingest_test_shared.go — go up 2 levels
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

// truncateToN mirrors helm's `trunc N | trimSuffix "-"` pipeline so we can
// compute resource names that match the chart's _helpers.tpl exactly.
func truncateToN(s string, n int) string {
	if len(s) > n {
		s = s[:n]
	}
	return strings.TrimSuffix(s, "-")
}

// readOperatorSGT returns the SPLUNK_GENERAL_TERMS env value configured on the
// active operator Deployment (resolved via TestCaseEnv.OperatorDeployment so it
// works for both cluster-wide and per-testcase installs). The operator reads
// the same env (see pkg/splunk/enterprise/configuration.go) and propagates it
// to CR-managed pods, so reusing it here keeps SGT acceptance an operator-level
// concern. Returns "" when the deployment cannot be queried or the env is unset.
func readOperatorSGT(ctx context.Context, testcaseEnvInst *testenv.TestCaseEnv) string {
	ns, name := testcaseEnvInst.OperatorDeployment()
	jsonpath := `{.spec.template.spec.containers[*].env[?(@.name=="SPLUNK_GENERAL_TERMS")].value}`
	cmd := exec.CommandContext(ctx, "kubectl", "get", "deployment",
		"-n", ns, name,
		"-o", "jsonpath="+jsonpath)
	// Use Output() (stdout only) — kubectl may emit unrelated warnings on stderr
	// (e.g. deprecation notices) that would otherwise pollute the env value.
	out, err := cmd.Output()
	if err != nil {
		testcaseEnvInst.Log.Info("could not read SPLUNK_GENERAL_TERMS from operator deployment", "namespace", ns, "name", name, "err", err, "stdout", string(out))
		return ""
	}
	sgt := strings.TrimSpace(string(out))
	testcaseEnvInst.Log.Info("resolved SPLUNK_GENERAL_TERMS from operator deployment", "namespace", ns, "name", name, "value", sgt)
	return sgt
}

// RunUFToStandaloneIngestTest deploys a Standalone CR and a splunk-universalforwarder Helm
// release (Deployment mode), then asserts that log events forwarded from the UF pod appear on
// the standalone's search interface within a reasonable timeout.
//
// The test exercises:
//  1. UF Helm chart Deployment scheduling and readiness
//  2. TCP 9997 forwarding from UF to standalone (outputs.conf via --set)
//  3. Standalone ingest: events from the UF host land in _internal (or main)
//  4. Standalone search: CountSearchResults returns > 0 for a host-scoped query
func RunUFToStandaloneIngestTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv) {
	standalone, err := testcaseEnvInst.DeployAndVerifyStandalone(ctx, deployment, "")
	Expect(err).To(Succeed(), "Failed to deploy and verify Standalone")

	standalonePod := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
	ns := testcaseEnvInst.GetName()
	ufRelease := deployment.GetName() + "-uf"
	// Deployment name produced by the UF chart: <fullname>-deploy where <fullname>
	// is `<release>-splunk-universalforwarder` truncated to 44 chars (see the
	// chart's splunk-universalforwarder.fullname helper). Replicate that truncation
	// here so kubectl set env / rollout status target the actual resource name.
	ufFullname := truncateToN(ufRelease+"-splunk-universalforwarder", 44)
	ufDeploymentName := ufFullname + "-deploy"
	ufChartPath := filepath.Join(repoRoot(), "helm-chart", "splunk-universalforwarder")

	// Standalone receives forwarded data on its ClusterIP service (port 9997)
	standaloneService := fmt.Sprintf("splunk-%s-standalone-service.%s.svc.cluster.local", standalone.Name, ns)

	// Reuse the operator's explicit SGT acceptance when installing the UF chart.
	sgt := readOperatorSGT(ctx, testcaseEnvInst)
	Expect(sgt).NotTo(BeEmpty(), "SPLUNK_GENERAL_TERMS not set on operator deployment; cannot accept SGT for UF")

	// Deploy the UF chart into the test namespace, pointing at the standalone.
	installArgs := []string{
		"install", ufRelease,
		ufChartPath,
		"--namespace", ns,
		"--wait",
		"--timeout", "15m",
		"--set", fmt.Sprintf("splunkConfig.forwardServer=%s:9997", standaloneService),
		"--set", "splunkConfig.password=IntegTest1!",
		"--set", "splunkConfig.splunkGeneralTerms=" + sgt,
	}
	installCmd := exec.CommandContext(ctx, "helm", installArgs...)
	out, installErr := installCmd.CombinedOutput()
	testcaseEnvInst.Log.Info("helm install UF", "output", string(out))
	Expect(installErr).To(Succeed(), "helm install splunk-universalforwarder failed: %s", string(out))

	// Register Helm uninstall so the release is cleaned up even on failure
	DeferCleanup(func() {
		uninstallCmd := exec.Command("helm", "uninstall", ufRelease, "--namespace", ns)
		if uninstallOut, uninstallErr := uninstallCmd.CombinedOutput(); uninstallErr != nil {
			testcaseEnvInst.Log.Info("helm uninstall UF (cleanup)", "output", string(uninstallOut), "err", uninstallErr)
		}
	})

	// Wait for the UF Deployment to roll out (chart renders kind: Deployment, not DaemonSet)
	rolloutCmd := exec.CommandContext(ctx, "kubectl", "rollout", "status",
		"deployment/"+ufDeploymentName, "--namespace", ns, "--timeout=15m")
	rolloutOut, rolloutErr := rolloutCmd.CombinedOutput()
	Expect(rolloutErr).To(Succeed(), "UF Deployment did not become ready: %s", string(rolloutOut))

	// The UF ships splunkd internal metrics to _internal; wait for those events to appear
	// on the standalone indexed from a host matching the UF pod name prefix.
	searchString := fmt.Sprintf(
		`index=_internal host="%s-*" | stats count`, ufFullname,
	)

	Eventually(func() (int, error) {
		return testenv.CountSearchResults(ctx, deployment, standalonePod, searchString)
	}, 5*time.Minute, 15*time.Second).Should(BeNumerically(">", 0),
		"No _internal events from UF host %s-* appeared on standalone pod %s within timeout",
		ufFullname, standalonePod)

	testcaseEnvInst.Log.Info("UF → Standalone forwarding verified: events found on standalone",
		"ufRelease", ufRelease, "standalone", standalone.Name, "namespace", ns)
}
