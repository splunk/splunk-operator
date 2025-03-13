// Copyright (c) 2018-2025 Splunk Inc. All rights reserved.

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
package vault

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Vault Test for SVA S1", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
	})

	AfterEach(func() {
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
		It("vault, integration, s1: Create a standalone instance with Vault integration", func() {
			// Create a service account
			saName := "splunk-service-account"
			err := testcaseEnvInst.CreateServiceAccount(saName)
			Expect(err).To(Succeed(), "Unable to create a service account")

			// Deploy and configure Vault
			roleName := "splunk-role"
			secretPath := "secret/data/splunk"

			// TODO

			// Add secrets to Vault
			// TODO

			// Create Standalone
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "IfNotPresent",
					},
					ServiceAccount: saName,
					VaultIntegration: enterpriseApi.VaultIntegration{
						Enable:       true,
						Role:         roleName,
						SecretPath:   secretPath,
						Address:      "http://vault.vault.svc.cluster.local:8200",
						OperatorRole: roleName,
					},
				},
			}
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy a standalone instance")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Stateful Set annotations
			areStsAnnotationsValid := testenv.VerifyStatefulSetVaultAnnotations(standalone.Annotations)
			Expect(areStsAnnotationsValid).To(BeTrue())

			// Verify Pod annotations
			// TODO

			// Verify secret mount
			// TODO

			// Login via splunkd
			// TODO
		})
	})

	Context("Standalone deployment (S1)", func() {
		It("vault, integration, s1: Create a standalone instance with Vault integration and update a secret value", func() {
			// Create a service account
			saName := "splunk-service-account"
			err := testcaseEnvInst.CreateServiceAccount(saName)
			Expect(err).To(Succeed(), "Unable to create a service account")

			// Deploy and configure Vault
			roleName := "splunk-role"
			secretPath := "secret/data/splunk"
			// TODO

			// Add secrets to Vault
			// TODO

			// Create Standalone
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "IfNotPresent",
					},
					ServiceAccount: saName,
					VaultIntegration: enterpriseApi.VaultIntegration{
						Enable:       true,
						Role:         roleName,
						SecretPath:   secretPath,
						Address:      "http://vault.vault.svc.cluster.local:8200",
						OperatorRole: roleName,
					},
				},
			}
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy a standalone instance")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Stateful Set annotations
			// TODO

			// Verify Pod annotations
			// TODO

			// Verify secret mount
			// TODO

			// Login via splunkd
			// TODO

			// Peform secret update
			// TODO

			// Restart
			// TODO

			// Verify Stateful Set annotations
			// TODO

			// Verify Pod annotations
			// TODO

			// Verify secret mount
			// TODO

			// Login via splunkd
			// TODO
		})
	})

	Context("Standalone deployment (S1)", func() {
		It("vault, integration, s1: Create a standalone instance with Vault integration and switch to k8s secrets", func() {
			// Create a service account
			saName := "splunk-service-account"
			err := testcaseEnvInst.CreateServiceAccount(saName)
			Expect(err).To(Succeed(), "Unable to create a service account")

			// Deploy and configure Vault
			roleName := "splunk-role"
			secretPath := "secret/data/splunk"
			// TODO

			// Add secrets to Vault
			// TODO

			// Create Standalone
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "IfNotPresent",
					},
					ServiceAccount: saName,
					VaultIntegration: enterpriseApi.VaultIntegration{
						Enable:       true,
						Role:         roleName,
						SecretPath:   secretPath,
						Address:      "http://vault.vault.svc.cluster.local:8200",
						OperatorRole: roleName,
					},
				},
			}
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy a standalone instance")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Stateful Set annotations
			// TODO

			// Verify Pod annotations
			// TODO

			// Verify secret mount
			// TODO

			// Login via splunkd
			// TODO

			// Switch to k8s secret
			standalone.Spec.VaultIntegration.Enable = false
			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Unable to update a standalone instance")

			// Verify k8s secrets
			namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
			secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
			testcaseEnvInst.Log.Info("Data in secret object", "data", secretStruct.Data)
			Expect(err).To(Succeed(), "Unable to get secret struct")

			secretObjectNames := testenv.GetVersionedSecretNames(testcaseEnvInst.GetName(), 1)
			testenv.VerifySecretsOnSecretObjects(ctx, deployment, testcaseEnvInst, secretObjectNames, secretStruct.Data, false)

			verificationPods := testenv.DumpGetPods(testcaseEnvInst.GetName())
			testenv.VerifySecretsOnPods(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			// Verify secret mount
			testenv.VerifySplunkServerConfSecrets(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)
			testenv.VerifySplunkInputConfSecrets(deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			testenv.VerifySplunkSecretViaAPI(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			// Login via splunkd
			// TODO
		})
	})

	Context("Standalone deployment (S1)", func() {
		It("vault, integration, s1: Create a standalone instance with k8s secrets and switch to Vault", func() {
			// Create a service account
			saName := "splunk-service-account"
			err := testcaseEnvInst.CreateServiceAccount(saName)
			Expect(err).To(Succeed(), "Unable to create a service account")

			// Deploy and configure Vault
			roleName := "splunk-role"
			secretPath := "secret/data/splunk"
			// TODO

			// Add secrets to Vault
			// TODO

			// Create Standalone
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "IfNotPresent",
					},
					ServiceAccount: saName,
				},
			}
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy a standalone instance")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify k8s secrets
			namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
			secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
			testcaseEnvInst.Log.Info("Data in secret object", "data", secretStruct.Data)
			Expect(err).To(Succeed(), "Unable to get secret struct")

			secretObjectNames := testenv.GetVersionedSecretNames(testcaseEnvInst.GetName(), 1)
			testenv.VerifySecretsOnSecretObjects(ctx, deployment, testcaseEnvInst, secretObjectNames, secretStruct.Data, false)

			verificationPods := testenv.DumpGetPods(testcaseEnvInst.GetName())
			testenv.VerifySecretsOnPods(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			// Verify secret mount
			testenv.VerifySplunkServerConfSecrets(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)
			testenv.VerifySplunkInputConfSecrets(deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			testenv.VerifySplunkSecretViaAPI(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			// Login via splunkd
			// TODO

			// Switch back to Vault
			standalone.Spec.VaultIntegration = enterpriseApi.VaultIntegration{
				Enable:       true,
				Role:         roleName,
				SecretPath:   secretPath,
				Address:      "http://vault.vault.svc.cluster.local:8200",
				OperatorRole: roleName,
			}
			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Unable to update a standalone instance")

			// Verify Stateful Set annotations
			// instance := &enterpriseApi.Standalone{}
			// err = deployment.GetInstance(ctx, deployment.GetName(), instance)
			// Expect(err).To(Succeed(), "Failed to get instance of Standalone")
			// annotations := instance.
			// TODO

			// Verify Pod annotations
			// TODO
			// Verify secret mount
			testenv.VerifySplunkServerConfSecrets(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)
			testenv.VerifySplunkInputConfSecrets(deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			testenv.VerifySplunkSecretViaAPI(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			// Login via splunkd
			// TODO
		})
	})
})
