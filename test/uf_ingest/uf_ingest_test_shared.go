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

// RunUFToStandaloneIngestTest deploys a Standalone CR and a splunk-universalforwarder Helm
// release (DaemonSet mode), then asserts that log events forwarded from the UF pod appear on
// the standalone's search interface within a reasonable timeout.
//
// The test exercises:
//  1. UF Helm chart DaemonSet scheduling and readiness
//  2. TCP 9997 forwarding from UF to standalone (outputs.conf via --set)
//  3. Standalone ingest: events from the UF host land in _internal (or main)
//  4. Standalone search: CountSearchResults returns > 0 for a host-scoped query
func RunUFToStandaloneIngestTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv) {
	standalone, err := testcaseEnvInst.DeployAndVerifyStandalone(ctx, deployment, "", "")
	Expect(err).To(Succeed(), "Failed to deploy and verify Standalone")

	standalonePod := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
	ns := testcaseEnvInst.GetName()
	ufRelease := deployment.GetName() + "-uf"
	// DaemonSet name produced by the UF chart: <release>-splunk-universalforwarder
	dsDaemonSetName := ufRelease + "-splunk-universalforwarder"
	ufChartPath := filepath.Join(repoRoot(), "helm-chart", "splunk-universalforwarder")

	// Standalone receives forwarded data on its ClusterIP service (port 9997)
	standaloneService := fmt.Sprintf("splunk-%s-standalone-service.%s.svc.cluster.local", standalone.Name, ns)

	// Deploy the UF chart into the test namespace, pointing at the standalone
	installArgs := []string{
		"install", ufRelease,
		ufChartPath,
		"--namespace", ns,
		"--set", fmt.Sprintf("splunkConfig.forwardServer=%s:9997", standaloneService),
		"--wait",
		"--timeout", "5m",
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

	// Wait for all DaemonSet pods to be ready
	Expect(testenv.WaitForDaemonSetPodsReady(ctx, deployment, ns, dsDaemonSetName)).
		To(Succeed(), "UF DaemonSet %s pods did not become ready in namespace %s", dsDaemonSetName, ns)

	// The UF ships splunkd internal metrics to _internal; wait for those events to appear
	// on the standalone indexed from a host matching the UF pod name prefix.
	searchString := fmt.Sprintf(
		`index=_internal host="%s-*" | stats count`, dsDaemonSetName,
	)

	Eventually(func() (int, error) {
		return testenv.CountSearchResults(ctx, deployment, standalonePod, searchString)
	}, 5*time.Minute, 15*time.Second).Should(BeNumerically(">", 0),
		"No _internal events from UF host %s-* appeared on standalone pod %s within timeout",
		dsDaemonSetName, standalonePod)

	testcaseEnvInst.Log.Info("UF → Standalone forwarding verified: events found on standalone",
		"ufRelease", ufRelease, "standalone", standalone.Name, "namespace", ns)
}
