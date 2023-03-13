// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
package crcrud

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Crcrud test for SVA S1", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var defaultCPULimits string
	var newCPULimits string
	var ctx context.Context

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
		defaultCPULimits = "4"
		newCPULimits = "2"
		ctx = context.TODO()
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
			testcaseEnvInst.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}
		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}
	})

	Context("Standalone deployment (S1)", func() {
		It("managercrcrud, integration, s1: can deploy a standalone instance, change its CR, update the instance", func() {

			// Deploy Standalone
			mcRef := deployment.GetName()
			standalone, err := deployment.DeployStandalone(ctx, deployment.GetName(), mcRef, "")
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Verify Standalone goes to ready state
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Verify CPU limits before updating the CR
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyCPULimits(deployment, testcaseEnvInst.GetName(), standalonePodName, defaultCPULimits)

			// Change CPU limits to trigger CR update
			standalone.Spec.Resources.Limits = corev1.ResourceList{
				"cpu": resource.MustParse(newCPULimits),
			}
			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with updated CR ")

			// Verify Standalone is updating
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), enterpriseApi.PhaseUpdating)

			// Verify Standalone goes to ready state
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), enterpriseApi.PhaseReady)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Verify CPU limits after updating the CR
			testenv.VerifyCPULimits(deployment, testcaseEnvInst.GetName(), standalonePodName, newCPULimits)
		})
	})
})
