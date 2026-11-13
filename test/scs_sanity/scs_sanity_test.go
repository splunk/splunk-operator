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
package scssanity

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

// Run by gitlab-ci/scs-sanity-gate.sh's run_scs_sanity_operator(), after verify_operator,
// unconditionally, including on a brand-new environment with no pre-existing Helm release.
// These checks need only the operator and read access, never a Splunk stack or tenant.
//
// One Ordered container with ContinueOnFailure keeps Ginkgo from shuffling the specs and lets the
// rest report after a failure. The gate runs the binary three times by label (see
// gitlab-ci/scs-sanity-gate.sh): the operator-health specs first, then the canary specs, then the
// phase:post-canary-recheck specs again, so a crash the canary provoked still fails the operator checks.
// ensureCanary returns the run already set up, or sets it up, skipping the spec when the gate did not
// enable the canary for this cluster.
func ensureCanary(ctx SpecContext, existing *canaryRun) *canaryRun {
	if existing != nil {
		return existing
	}
	run, skipReason, warning, err := newCanaryRun(ctx, os.Getenv, operatorNamespace,
		testcaseEnvInstance.GetKubeClient(), testcaseEnvInstance.GetAPIReader(), testenv.RandomDNSName)
	Expect(err).To(Succeed())
	if skipReason != "" {
		Skip(skipReason)
	}
	if warning != nil {
		GinkgoWriter.Printf("WARNING: %v\n", warning)
	}
	return run
}

var _ = Describe("SCS post-upgrade sanity", Ordered, ContinueOnFailure, Label("tier:scs-sanity", "feature:scs-sanity"), func() {
	var canary *canaryRun

	AfterAll(func() {
		if canary == nil {
			return
		}
		warnings, err := canary.cleanupAtEnd()
		for _, w := range warnings {
			GinkgoWriter.Println("WARNING: " + w)
		}
		Expect(err).To(Succeed(), "canary cleanup left resources in namespace %s", canary.cfg.Namespace)
	})

	It("operator rollout is healthy", Label("phase:post-upgrade-operator"), func(ctx SpecContext) {
		kubeClient := testcaseEnvInstance.GetKubeClient()

		// Retried rather than sampled once: these run immediately after the Helm upgrade, while the
		// rollout and the leader-election handover are still settling.
		Expect(retryUntil(ctx, operatorRolloutTimeout, testenv.PollInterval, func(ctx context.Context) error {
			return operatorDeploymentHealthy(ctx, kubeClient, operatorNamespace, operatorName, targetOperatorImg)
		})).
			To(Succeed(), "operator Deployment %s/%s is not fully healthy", operatorNamespace, operatorName)

		apiReader := testcaseEnvInstance.GetAPIReader()
		Expect(retryUntil(ctx, leaderElectionTimeout, testenv.PollInterval, func(ctx context.Context) error {
			return operatorLeaderElected(ctx, apiReader, operatorNamespace)
		})).
			To(Succeed(), "operator has no active leader in %s", operatorNamespace)
	})

	It("every SOK CRD is Established", Label("phase:post-upgrade-operator"), func(ctx SpecContext) {
		crds, err := listCRDs(ctx, testcaseEnvInstance.GetAPIReader())
		Expect(err).To(Succeed())
		Expect(evaluateCRDsEstablished(crds)).To(Succeed())
	})

	It("reconciles a canary Standalone to Ready", Label("phase:post-upgrade-canary"), func(ctx SpecContext) {
		canary = ensureCanary(ctx, canary)
		Expect(canary.env.create(ctx, newCanaryStandalone(canary.cfg.Namespace, canary.names, 1), "standalones.enterprise.splunk.com")).To(Succeed())
		tracker := newWaitTracker(time.Now())
		Expect(retryPhase(ctx, "canary Standalone reaching Ready", canary.cfg.ReadyTimeout, canaryPollInterval, func(ctx context.Context) error {
			return canary.env.standaloneAt(ctx, canary.names, 1, tracker)
		})).To(Succeed(), canary.diagnose("canary Standalone did not reach Ready"))
		canary.standaloneReady = true
	})

	It("scales the canary Standalone from 1 to 2 and back to 1", Label("phase:post-upgrade-canary"), func(ctx SpecContext) {
		canary = ensureCanary(ctx, canary)
		if !canary.standaloneReady {
			Skip("the canary Standalone never reached Ready, so there is nothing to scale")
		}
		for _, step := range []struct {
			want    int32
			timeout time.Duration
		}{{2, canary.cfg.ScaleUpTimeout}, {1, canary.cfg.ScaleDownTimeout}} {
			want := step.want
			Expect(canary.env.scaleStandalone(ctx, canary.names, want)).To(Succeed())
			phase := fmt.Sprintf("canary Standalone settling at %d replicas", want)
			tracker := newWaitTracker(time.Now())
			Expect(retryPhase(ctx, phase, step.timeout, canaryPollInterval, func(ctx context.Context) error {
				return canary.env.standaloneAt(ctx, canary.names, want, tracker)
			})).To(Succeed(), canary.diagnose(phase+" failed"))
		}
	})

	// Advisory: it retries briefly, then reports findings without failing, and skips when the
	// deploy identity cannot list the SOK CRs cluster-wide. Hard PDB assertions come with the indexer scenarios.
	It("every IndexerCluster and IngestorCluster still has its PodDisruptionBudget", Label("phase:post-upgrade-operator"), func(ctx SpecContext) {
		advice := adviseOnPDBFleet(ctx, testcaseEnvInstance.GetAPIReader(), fleetAdvisoryTimeout, fleetAdvisoryInterval)
		if advice.Skip != "" {
			Skip(advice.Skip)
		}
		GinkgoWriter.Printf("checked PodDisruptionBudgets for %d cluster(s)\n", advice.Checked)
		if len(advice.Warnings) > 0 {
			text := "ADVISORY PodDisruptionBudget findings (not failing the gate): " + strings.Join(advice.Warnings, "; ")
			GinkgoWriter.Println(text)
			AddReportEntry("pdb-fleet-advisory", text)
		}
	})

	// These two stay last, and carry the recheck label, so they also cover anything the canary provoked.
	It("operator pods have not restarted since the rollout", Label("phase:post-upgrade-operator", "phase:post-canary-recheck"), func(ctx SpecContext) {
		apiReader := testcaseEnvInstance.GetAPIReader()
		pods, err := listOperatorPods(ctx, apiReader, operatorNamespace, operatorName)
		Expect(err).To(Succeed())
		baseline, err := loadOperatorPodBaseline(os.Getenv("SCS_SANITY_OPERATOR_POD_BASELINE"))
		Expect(err).To(Succeed())
		Expect(evaluateOperatorPodRestarts(pods, baseline)).To(Succeed(), "operator in %s is crash-looping or was restarted", operatorNamespace)
	})

	// A Forbidden log read fails: preflight_read_permissions checks pods/log first, so reaching
	// this means the permission vanished, and a skipped panic scan is not a gate.
	It("operator logs contain no panic", Label("phase:post-upgrade-operator", "phase:post-canary-recheck"), func(ctx SpecContext) {
		apiReader := testcaseEnvInstance.GetAPIReader()
		pods, err := listOperatorPods(ctx, apiReader, operatorNamespace, operatorName)
		Expect(err).To(Succeed())

		cutoff, err := parseUpgradeStart(os.Getenv("SCS_SANITY_UPGRADE_START_TIME"))
		Expect(err).To(Succeed())
		fetch, err := newPodLogFetcher()
		Expect(err).To(Succeed())
		Expect(scanOperatorLogsForPanics(ctx, pods, fetch, cutoff)).To(Succeed(), "operator logs could not be scanned or contain a panic")
	})
})
