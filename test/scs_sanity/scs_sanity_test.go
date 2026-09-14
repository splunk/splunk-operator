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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

// State threaded across the ordered specs in this Describe: discovered once in check 1/2 and
// reused by checks 3+ so every spec targets the exact same tenant IngestorCluster/pod. The
// discovery/readiness/HEC-ingest logic itself lives in scs_sanity_helpers.go as plain
// (ctx, ...) -> (result, error) functions; this file owns threading that state across specs and
// making the Gomega assertions on the returned errors.
var (
	ingestor    *enterpriseApi.IngestorCluster
	ingestorPod string
	ingestorNS  string
)

// Phase 1: run by gitlab-ci/scs-sanity-gate.sh's capture_tenant_baseline(), before the Helm
// upgrade. Snapshots the tenant's current state and writes it to SCS_SANITY_BASELINE_FILE so
// the post-upgrade phase (a separate ginkgo invocation, run after the real upgrade completes)
// can diff against it.
var _ = Describe("SCS pre-upgrade baseline capture", Ordered, Label("tier:scs-sanity", "phase:pre-upgrade", "feature:scs-sanity"), func() {

	It("discovers the tenant IngestorCluster and confirms it is Ready", func(ctx SpecContext) {
		kubeClient := testcaseEnvInstance.GetKubeClient()

		var err error
		ingestor, err = discoverIngestor(ctx, kubeClient, ingestorName, ingestorNamespace, operatorNamespace)
		Expect(err).To(Succeed(), "failed to discover target IngestorCluster")
		ingestorNS = ingestor.GetNamespace()
		ingestorPod = ingestorPodName(ingestor)

		ingestor, err = waitIngestorReady(ctx, kubeClient, ingestor.GetName(), ingestorNS, testenv.MediumTimeout, testenv.PollInterval)
		Expect(err).To(Succeed(), "IngestorCluster %s/%s did not reach a steady Ready phase", ingestorNS, ingestorName)
	})

	It("captures pod restart counts and a pre-upgrade HEC marker, then writes the baseline file", func(ctx SpecContext) {
		Expect(ingestor).NotTo(BeNil(), "IngestorCluster was not discovered by the previous spec")
		Expect(baselineFile).NotTo(BeEmpty(), "SCS_SANITY_BASELINE_FILE must be set for the pre-upgrade phase")

		restarts, err := snapshotPodRestarts(ctx, testcaseEnvInstance.GetKubeClient(), ingestorNS, ingestorPod)
		Expect(err).To(Succeed(), "failed to snapshot restart counts for ingestor pod %s", ingestorPod)

		// Use the same ACK-only path as the post-upgrade phase (assertHECIngest) rather than the
		// tenant's real HEC token: this probe must never inject sanity markers into customer-facing
		// tenant data on a shared SCS tenant.
		Expect(assertHECIngest(ctx, deployment, ingestorPod, ingestorNS, "before-"+testenv.RandomDNSName(8), testenv.MediumTimeout, testenv.PollInterval)).
			To(Succeed(), "pre-upgrade HEC ingest gate failed on pod %s", ingestorPod)

		baseline := tenantBaseline{
			IngestorNamespace: ingestorNS,
			IngestorName:      ingestor.GetName(),
			Phase:             ingestor.Status.Phase,
			Replicas:          ingestor.Status.Replicas,
			ReadyReplicas:     ingestor.Status.ReadyReplicas,
			ResourceVersion:   ingestor.GetResourceVersion(),
			Spec:              ingestor.Spec,
			PodRestarts:       restarts,
		}
		Expect(writeTenantBaseline(baselineFile, baseline)).To(Succeed(),
			"failed to write pre-upgrade tenant baseline to %s", baselineFile)
	})
})

// Phase 2a: run by gitlab-ci/scs-sanity-gate.sh's run_scs_sanity_operator(), after
// verify_operator, UNCONDITIONALLY — including on a brand-new environment with no pre-existing
// Helm release (RELEASE_PRESENT=false). These checks only need the operator Deployment/Lease
// itself, never an existing tenant, so they must not be skipped on a fresh install: that's
// exactly the case where verifying the operator came up healthy matters most.
var _ = Describe("SCS post-upgrade operator health", Ordered, Label("tier:scs-sanity", "phase:post-upgrade-operator", "feature:scs-sanity"), func() {

	It("operator rollout is healthy", func(ctx SpecContext) {
		kubeClient := testcaseEnvInstance.GetKubeClient()

		Expect(operatorDeploymentHealthy(ctx, kubeClient, operatorNamespace, operatorName, targetOperatorImg)).
			To(Succeed(), "operator Deployment %s/%s is not fully healthy", operatorNamespace, operatorName)

		Expect(operatorLeaderElected(ctx, kubeClient, operatorNamespace)).
			To(Succeed(), "operator has no active leader in %s", operatorNamespace)
	})
})

// Phase 2b: run by gitlab-ci/scs-sanity-gate.sh's run_scs_sanity_tenant(), after the operator
// health checks above, but ONLY when RELEASE_PRESENT=true (an existing tenant to compare
// against). Re-runs the ingestor/HEC checks against the upgraded operator, then reads back the
// phase-1 baseline to assert no tenant disruption.
var _ = Describe("SCS deployment sanity", Ordered, Label("tier:scs-sanity", "phase:post-upgrade-tenant", "feature:scs-sanity"), func() {

	It("the tenant IngestorCluster is reconciled and Ready", func(ctx SpecContext) {
		kubeClient := testcaseEnvInstance.GetKubeClient()

		var err error
		ingestor, err = discoverIngestor(ctx, kubeClient, ingestorName, ingestorNamespace, operatorNamespace)
		Expect(err).To(Succeed(), "failed to discover target IngestorCluster")
		ingestorNS = ingestor.GetNamespace()
		ingestorPod = ingestorPodName(ingestor)

		ingestor, err = waitIngestorReady(ctx, kubeClient, ingestor.GetName(), ingestorNS, testenv.MediumTimeout, testenv.PollInterval)
		Expect(err).To(Succeed(), "IngestorCluster %s/%s did not reach a steady Ready phase", ingestorNS, ingestorName)
	})

	It("HEC ingest on the ingestor pod is accepted and reflected in its own _internal metrics", func(ctx SpecContext) {
		Expect(ingestor).NotTo(BeNil(), "IngestorCluster was not discovered by the previous spec")
		Expect(assertHECIngest(ctx, deployment, ingestorPod, ingestorNS, testenv.RandomDNSName(12), testenv.MediumTimeout, testenv.PollInterval)).
			To(Succeed(), "post-upgrade HEC ingest gate failed on pod %s", ingestorPod)
	})

	It("shows no tenant disruption relative to the pre-upgrade baseline", func(ctx SpecContext) {
		Expect(ingestor).NotTo(BeNil(), "IngestorCluster was not discovered by the previous spec")
		Expect(baselineFile).NotTo(BeEmpty(), "SCS_SANITY_BASELINE_FILE must be set for the post-upgrade phase")

		baseline, err := readTenantBaseline(baselineFile)
		Expect(err).To(Succeed(), "failed to read pre-upgrade tenant baseline from %s", baselineFile)
		Expect(baseline.IngestorNamespace).To(Equal(ingestorNS), "baseline was captured for a different tenant namespace")
		Expect(baseline.IngestorName).To(Equal(ingestor.GetName()), "baseline was captured for a different tenant name")

		// Operator-level: the tenant's spec must be byte-identical to its pre-upgrade snapshot —
		// the operator image swap must not have mutated the tenant CR — and its pod must not
		// have restarted as a side effect of the upgrade.
		Expect(ingestor.Spec).To(Equal(baseline.Spec), "tenant IngestorCluster %s/%s spec changed across the operator upgrade", ingestorNS, ingestor.GetName())

		afterRestarts, err := snapshotPodRestarts(ctx, testcaseEnvInstance.GetKubeClient(), ingestorNS, ingestorPod)
		Expect(err).To(Succeed(), "failed to snapshot post-upgrade restart counts for ingestor pod %s", ingestorPod)
		Expect(diffPodRestarts(ingestorPod, baseline.PodRestarts, afterRestarts)).To(Succeed())

		// Data-path: a fresh marker event must still be accepted post-upgrade, proving ingest
		// continuity across the SOK image swap (not just that the pod is still Running).
		Expect(assertHECIngest(ctx, deployment, ingestorPod, ingestorNS, "after-"+testenv.RandomDNSName(8), testenv.MediumTimeout, testenv.PollInterval)).
			To(Succeed(), "post-upgrade non-disruption HEC ingest gate failed on pod %s", ingestorPod)
	})
})
