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

package licensemanager

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

// RunLMCertTest deploys a LicenseManager with certs in the initial spec, waits for the
// pod to be Running, verifies mounts and certRev annotations, then rotates the server cert.
func RunLMCertTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, testenvInstance *testenv.TestEnv) {
	ns := testcaseEnvInst.GetName()
	c := testenvInstance.GetKubeClient()

	Expect(testenv.CreateServerCertSecret(ctx, c, ns, "lm-server-cert")).To(Succeed())
	Expect(testenv.CreateCustomCertSecret(ctx, c, ns, "lm-custom-ca")).To(Succeed())

	_, err := deployment.DeployLicenseManagerWithGivenSpec(ctx, deployment.GetName(),
		enterpriseApi.LicenseManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{ImagePullPolicy: "Always", Image: testenvInstance.GetSplunkImage()},
				Certs: []enterpriseApi.CertSpec{
					{SecretRef: corev1.LocalObjectReference{Name: "lm-server-cert"}, Role: enterpriseApi.CertRoleServer},
					{SecretRef: corev1.LocalObjectReference{Name: "lm-custom-ca"}},
				},
			},
		})
	Expect(err).To(Succeed())

	lmPod := fmt.Sprintf(testenv.LicenseManagerPod, deployment.GetName(), 0)
	lmSts := fmt.Sprintf("splunk-%s-license-manager", deployment.GetName())

	testenv.WaitForPodRunning(ctx, deployment, lmPod)
	testenv.VerifyCertSecretMounted(ctx, deployment, lmPod, "/mnt/tls/splunk-server-tls-cert")
	testenv.VerifyCertSecretMounted(ctx, deployment, lmPod, "/mnt/tls/lm-custom-ca")
	testenv.VerifyCertRevAnnotation(ctx, c, ns, lmSts, "lm-server-cert")
	testenv.VerifyCertRevAnnotation(ctx, c, ns, lmSts, "lm-custom-ca")

	initialHash := testenv.GetCertRevAnnotation(ctx, c, ns, lmSts, "lm-server-cert")
	testenv.RotateCertSecret(ctx, deployment, c, ns, "lm-server-cert")
	testenv.VerifyCertRotation(ctx, c, ns, lmSts, "lm-server-cert", initialHash)
}
