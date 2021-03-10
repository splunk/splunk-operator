// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package updatecr

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("update cr test", func() {

	var deployment *testenv.Deployment
	var storage string

	BeforeEach(func() {
		var err error
		deployment, err = testenvInstance.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
		storage = "mnt-splunk-etc"
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
			testenvInstance.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}
	})

	Context("Standalone deployment (S1)", func() {
		It("update: can deploy a standalone instance, change its CR, update the instance ", func() {

			// Deploy Standalone
			standalone, err := deployment.DeployStandalone(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Verify standalone goes to ready state
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify no "mnt-splunk-etc" volume exists before update, as EphemeralStorage is not set by default
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyEphemeralStorage(deployment, testenvInstance.GetName(), standalonePodName, false, storage)

			// Change EphemeralStorage to "true" (this will create a volume named "mnt-splunk-etc") to trigger CR update
			standalone.Spec.EtcVolumeStorageConfig.EphemeralStorage = true
			err = deployment.UpdateCR(standalone)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with updated ImagePullPolicy")

			// Verify standalone is updating
			testenv.VerifyStandaloneUpdating(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify standalone goes to ready state
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify "mnt-splunk-etc" volume exists after update
			testenv.VerifyEphemeralStorage(deployment, testenvInstance.GetName(), standalonePodName, true, storage)
		})
	})
})
