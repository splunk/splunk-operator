// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
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

// Package cert contains integration tests for cert mounting on IngestorCluster.
// All other CR types (Standalone, ClusterManager, IndexerCluster, SearchHeadCluster,
// LicenseManager, MonitoringConsole) have cert tests co-located in their topology
// test files. IngestorCluster has no existing topology test file, so it lives here.
package cert

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"

	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Cert Phase 1 — IngestorCluster", func() {
	var (
		testcaseEnvInst *testenv.TestCaseEnv
		deployment      *testenv.Deployment
		ctx             context.Context
	)

	BeforeEach(func() {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed())
		ctx = context.TODO()
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
	})

	It("cert, integration, ingestor: IngestorCluster mounts server-role and no-role certs and detects rotation", func() {
		ns := testcaseEnvInst.GetName()
		c := testenvInstance.GetKubeClient()

		// Create pre-baked TLS secrets directly — no cert-manager dependency
		Expect(testenv.CreateServerCertSecret(ctx, c, ns, "ic-server-cert")).To(Succeed())
		Expect(testenv.CreateCustomCertSecret(ctx, c, ns, "ic-custom-ca")).To(Succeed())

		// Deploy IngestorCluster with certs in the initial spec.
		// WaitForPodRunning is called AFTER VerifyCertRevAnnotation to avoid a race
		// where the first pod starts before the StatefulSet is updated with cert volumes.
		ic, err := deployment.DeployIngestorCluster(ctx, deployment.GetName(), 1,
			corev1.ObjectReference{}, corev1.ObjectReference{}, "")
		Expect(err).To(Succeed())
		ic.Spec.Certs = []enterpriseApi.CertSpec{
			{SecretRef: corev1.LocalObjectReference{Name: "ic-server-cert"}, Role: enterpriseApi.CertRoleServer},
			{SecretRef: corev1.LocalObjectReference{Name: "ic-custom-ca"}},
		}
		Expect(deployment.UpdateCR(ctx, ic)).To(Succeed())

		icSts := fmt.Sprintf("splunk-%s-ingestor", deployment.GetName())
		// Wait for the certRev annotation on the StatefulSet first — this confirms
		// the operator has reconciled and the cert volumes are in the pod template.
		// Only then wait for the pod (which will start with the cert volumes present).
		testenv.VerifyCertRevAnnotation(ctx, c, ns, icSts, "ic-server-cert")

		icPod := fmt.Sprintf(testenv.IngestorPod, deployment.GetName(), 0)
		testenv.WaitForPodRunning(ctx, deployment, icPod)
		testenv.VerifyCertSecretMounted(ctx, deployment, icPod, "/mnt/tls/splunk-server-tls-cert")
		testenv.VerifyCertSecretMounted(ctx, deployment, icPod, "/mnt/tls/ic-custom-ca")
		testenv.VerifyCertRevAnnotation(ctx, c, ns, icSts, "ic-custom-ca")

		// Cert rotation
		initialHash := testenv.GetCertRevAnnotation(ctx, c, ns, icSts, "ic-server-cert")
		testenv.RotateCertSecret(ctx, deployment, c, ns, "ic-server-cert")
		testenv.VerifyCertRotation(ctx, c, ns, icSts, "ic-server-cert", initialHash)
	})
})
