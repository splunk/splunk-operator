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

package crcrud

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

// certSpec returns the two-cert spec used by all topology cert tests.
func certSpec(serverSecretName, customSecretName string) []enterpriseApi.CertSpec {
	return []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: serverSecretName}, Role: enterpriseApi.CertRoleServer},
		{SecretRef: corev1.LocalObjectReference{Name: customSecretName}},
	}
}

// RunS1CertTest deploys a Standalone with certs in the initial spec, waits for the pod
// to be Running, verifies mounts and certRev annotations, then rotates the server cert.
func RunS1CertTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, testenvInstance *testenv.TestEnv) {
	ns := testcaseEnvInst.GetName()
	c := testenvInstance.GetKubeClient()

	Expect(testenv.CreateServerCertSecret(ctx, c, ns, "s1-server-cert")).To(Succeed())
	Expect(testenv.CreateCustomCertSecret(ctx, c, ns, "s1-custom-ca")).To(Succeed())

	spec := enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec:  enterpriseApi.Spec{ImagePullPolicy: "Always", Image: testenvInstance.GetSplunkImage()},
			Certs: certSpec("s1-server-cert", "s1-custom-ca"),
		},
	}
	_, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
	Expect(err).To(Succeed())

	podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
	stsName := fmt.Sprintf("splunk-%s-standalone", deployment.GetName())

	testenv.WaitForPodRunning(ctx, deployment, podName)
	testenv.VerifyCertSecretMounted(ctx, deployment, podName, "/mnt/tls/splunk-server-tls-cert")
	testenv.VerifyCertSecretMounted(ctx, deployment, podName, "/mnt/tls/s1-custom-ca")
	testenv.VerifyCertRevAnnotation(ctx, c, ns, stsName, "s1-server-cert")
	testenv.VerifyCertRevAnnotation(ctx, c, ns, stsName, "s1-custom-ca")

	initialHash := testenv.GetCertRevAnnotation(ctx, c, ns, stsName, "s1-server-cert")
	testenv.RotateCertSecret(ctx, deployment, c, ns, "s1-server-cert")
	testenv.VerifyCertRotation(ctx, c, ns, stsName, "s1-server-cert", initialHash)
}

// RunC3CertTest deploys ClusterManager, IndexerCluster, and SearchHeadCluster with
// certs in the initial spec. Waits for the first pod of each CR to be Running, then
// verifies mounts and certRev annotations and performs cert rotation on the ClusterManager.
func RunC3CertTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, testenvInstance *testenv.TestEnv) {
	ns := testcaseEnvInst.GetName()
	c := testenvInstance.GetKubeClient()

	Expect(testenv.CreateServerCertSecret(ctx, c, ns, "c3-server-cert")).To(Succeed())
	Expect(testenv.CreateCustomCertSecret(ctx, c, ns, "c3-custom-ca")).To(Succeed())

	certs := certSpec("c3-server-cert", "c3-custom-ca")
	splunkImage := testenvInstance.GetSplunkImage()
	baseSpec := enterpriseApi.CommonSplunkSpec{
		Spec:  enterpriseApi.Spec{ImagePullPolicy: "Always", Image: splunkImage},
		Certs: certs,
	}

	// Deploy LicenseManager first when a license file is configured.
	lmRef := ""
	if testenvInstance.HasLicenseFile() {
		_, err := deployment.DeployLicenseManager(ctx, deployment.GetName())
		Expect(err).To(Succeed(), "Unable to deploy LicenseManager")
		lmRef = deployment.GetName()
	}

	// Deploy ClusterManager
	cmSpec := enterpriseApi.ClusterManagerSpec{CommonSplunkSpec: baseSpec}
	if lmRef != "" {
		cmSpec.CommonSplunkSpec.LicenseManagerRef = corev1.ObjectReference{Name: lmRef}
	}
	_, err := deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
	Expect(err).To(Succeed(), "Unable to deploy ClusterManager")

	cmPod := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
	cmSts := fmt.Sprintf("splunk-%s-cluster-manager", deployment.GetName())
	testenv.WaitForPodRunning(ctx, deployment, cmPod)
	testenv.VerifyCertSecretMounted(ctx, deployment, cmPod, "/mnt/tls/splunk-server-tls-cert")
	testenv.VerifyCertSecretMounted(ctx, deployment, cmPod, "/mnt/tls/c3-custom-ca")
	testenv.VerifyCertRevAnnotation(ctx, c, ns, cmSts, "c3-server-cert")
	testenv.VerifyCertRevAnnotation(ctx, c, ns, cmSts, "c3-custom-ca")

	// Deploy IndexerCluster
	idxcName := deployment.GetName() + "-idxc"
	_, err = deployment.DeployIndexerClusterWithGivenSpec(ctx, idxcName, enterpriseApi.IndexerClusterSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec:              enterpriseApi.Spec{ImagePullPolicy: "Always", Image: splunkImage},
			Certs:             certs,
			ClusterManagerRef: corev1.ObjectReference{Name: deployment.GetName()},
		},
		Replicas: 3,
	})
	Expect(err).To(Succeed(), "Unable to deploy IndexerCluster")

	idxcPod := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 0)
	testenv.WaitForPodRunning(ctx, deployment, idxcPod)
	testenv.VerifyCertSecretMounted(ctx, deployment, idxcPod, "/mnt/tls/splunk-server-tls-cert")
	testenv.VerifyCertSecretMounted(ctx, deployment, idxcPod, "/mnt/tls/c3-custom-ca")

	// Deploy SearchHeadCluster
	shcName := deployment.GetName() + "-shc"
	_, err = deployment.DeploySearchHeadClusterWithGivenSpec(ctx, shcName, enterpriseApi.SearchHeadClusterSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec:              enterpriseApi.Spec{ImagePullPolicy: "Always", Image: splunkImage},
			Certs:             certs,
			ClusterManagerRef: corev1.ObjectReference{Name: deployment.GetName()},
		},
		Replicas: 3,
	})
	Expect(err).To(Succeed(), "Unable to deploy SearchHeadCluster")

	shPod := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
	testenv.WaitForPodRunning(ctx, deployment, shPod)
	testenv.VerifyCertSecretMounted(ctx, deployment, shPod, "/mnt/tls/splunk-server-tls-cert")
	testenv.VerifyCertSecretMounted(ctx, deployment, shPod, "/mnt/tls/c3-custom-ca")

	// Cert rotation on ClusterManager
	initialHash := testenv.GetCertRevAnnotation(ctx, c, ns, cmSts, "c3-server-cert")
	testenv.RotateCertSecret(ctx, deployment, c, ns, "c3-server-cert")
	testenv.VerifyCertRotation(ctx, c, ns, cmSts, "c3-server-cert", initialHash)
}

// RunM4CertTest deploys a multisite cluster with certs in the initial spec.
// Verifies mounts on the first pod of each CR type once Running, then rotates the ClusterManager cert.
func RunM4CertTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, testenvInstance *testenv.TestEnv) {
	ns := testcaseEnvInst.GetName()
	c := testenvInstance.GetKubeClient()

	Expect(testenv.CreateServerCertSecret(ctx, c, ns, "m4-server-cert")).To(Succeed())
	Expect(testenv.CreateCustomCertSecret(ctx, c, ns, "m4-custom-ca")).To(Succeed())

	certs := certSpec("m4-server-cert", "m4-custom-ca")
	splunkImage := testenvInstance.GetSplunkImage()

	multisiteDefaults := `splunk:
  multisite_master: localhost
  all_sites: site1,site2,site3
  site: site1
  multisite_replication_factor_origin: 1
  multisite_replication_factor_total: 2
  multisite_search_factor_origin: 1
  multisite_search_factor_total: 2
  idxc:
    search_factor: 2
    replication_factor: 2
`
	// Deploy LicenseManager first when a license file is configured.
	lmRef := ""
	if testenvInstance.HasLicenseFile() {
		_, err := deployment.DeployLicenseManager(ctx, deployment.GetName())
		Expect(err).To(Succeed(), "Unable to deploy LicenseManager")
		lmRef = deployment.GetName()
	}

	cmSpec := enterpriseApi.ClusterManagerSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec:     enterpriseApi.Spec{ImagePullPolicy: "Always", Image: splunkImage},
			Certs:    certs,
			Defaults: multisiteDefaults,
		},
	}
	if lmRef != "" {
		cmSpec.CommonSplunkSpec.LicenseManagerRef = corev1.ObjectReference{Name: lmRef}
	}
	_, err := deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
	Expect(err).To(Succeed(), "Unable to deploy ClusterManager")

	cmPod := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
	cmSts := fmt.Sprintf("splunk-%s-cluster-manager", deployment.GetName())
	testenv.WaitForPodRunning(ctx, deployment, cmPod)
	testenv.VerifyCertSecretMounted(ctx, deployment, cmPod, "/mnt/tls/splunk-server-tls-cert")
	testenv.VerifyCertSecretMounted(ctx, deployment, cmPod, "/mnt/tls/m4-custom-ca")
	testenv.VerifyCertRevAnnotation(ctx, c, ns, cmSts, "m4-server-cert")
	testenv.VerifyCertRevAnnotation(ctx, c, ns, cmSts, "m4-custom-ca")

	// Deploy site1 IndexerCluster only (representative; avoids waiting for all 3 sites)
	site1Defaults := fmt.Sprintf("splunk:\n  multisite_master: splunk-%s-cluster-manager-service\n  site: site1\n", deployment.GetName())
	site1Name := deployment.GetName() + "-site1"
	_, err = deployment.DeployIndexerClusterWithGivenSpec(ctx, site1Name, enterpriseApi.IndexerClusterSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec:              enterpriseApi.Spec{ImagePullPolicy: "Always", Image: splunkImage},
			Certs:             certs,
			Defaults:          site1Defaults,
			ClusterManagerRef: corev1.ObjectReference{Name: deployment.GetName()},
		},
		Replicas: 1,
	})
	Expect(err).To(Succeed(), "Unable to deploy site1 IndexerCluster")

	idxcPod := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, 0)
	testenv.WaitForPodRunning(ctx, deployment, idxcPod)
	testenv.VerifyCertSecretMounted(ctx, deployment, idxcPod, "/mnt/tls/splunk-server-tls-cert")
	testenv.VerifyCertSecretMounted(ctx, deployment, idxcPod, "/mnt/tls/m4-custom-ca")

	// Deploy SearchHeadCluster
	shcName := deployment.GetName() + "-shc"
	shcDefaults := fmt.Sprintf("splunk:\n  multisite_master: splunk-%s-cluster-manager-service\n  site: site0\n", deployment.GetName())
	_, err = deployment.DeploySearchHeadClusterWithGivenSpec(ctx, shcName, enterpriseApi.SearchHeadClusterSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec:              enterpriseApi.Spec{ImagePullPolicy: "Always", Image: splunkImage},
			Certs:             certs,
			Defaults:          shcDefaults,
			ClusterManagerRef: corev1.ObjectReference{Name: deployment.GetName()},
		},
		Replicas: 3,
	})
	Expect(err).To(Succeed(), "Unable to deploy SearchHeadCluster")

	shPod := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
	testenv.WaitForPodRunning(ctx, deployment, shPod)
	testenv.VerifyCertSecretMounted(ctx, deployment, shPod, "/mnt/tls/splunk-server-tls-cert")
	testenv.VerifyCertSecretMounted(ctx, deployment, shPod, "/mnt/tls/m4-custom-ca")

	// Cert rotation on ClusterManager
	initialHash := testenv.GetCertRevAnnotation(ctx, c, ns, cmSts, "m4-server-cert")
	testenv.RotateCertSecret(ctx, deployment, c, ns, "m4-server-cert")
	testenv.VerifyCertRotation(ctx, c, ns, cmSts, "m4-server-cert", initialHash)
}
