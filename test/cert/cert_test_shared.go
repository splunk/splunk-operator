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

package cert

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

// NOTE: the CA-only server-role cert and tls-only no-role cert paths (certSpec,
// verifyCertMounts, verifyCertRevs, and the associated secret creation/rotation
// helpers) have been removed pending an Ansible fix (the
// "when: server_tls_file.stat.exists" guard in configure_server_cert.yml) that has
// not yet landed in the CI/CD pipeline's Splunk image. The removed code is archived at
// ~/ws-cert/docs/sok_certmanagement_ca_tls_only_tests_deferred.md for restoration once
// the fix ships. Each test below exercises only the auto-generated (cert-manager) cert
// path via autogenOnlyCertSpec/verifyAutogenCertMounted.

// serverRoleMountPath is the fixed Ansible-processed path for a Role:server cert,
// regardless of its secret name (see api/enterprise/v4/common_types.go CertRole doc).
const serverRoleMountPath = "/mnt/tls/splunk-server-tls-cert"

// verifyServerAutogenCertMounted asserts the auto-generated Role:server cert
// (tls.crt+tls.key+ca.crt) is mounted at the fixed server-role path on podName.
func verifyServerAutogenCertMounted(ctx context.Context, deployment *testenv.Deployment, podName string) {
	testenv.VerifyCertFileMounted(ctx, deployment, podName, serverRoleMountPath, "tls.crt")
	testenv.VerifyCertFileMounted(ctx, deployment, podName, serverRoleMountPath, "tls.key")
	testenv.VerifyCertFileMounted(ctx, deployment, podName, serverRoleMountPath, "ca.crt")
	logf.Log.Info("verifyServerAutogenCertMounted complete", "podName", podName)
}

// splunkServiceFQDN returns "<name>.<ns>.svc.cluster.local" for a K8s service name.
func splunkServiceFQDN(serviceName, ns string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, ns)
}

// RunS1CertTest deploys a Standalone with two certs in the initial spec:
//   - a Role:server cert missing at spec time, so the operator auto-generates it via
//     cert-manager with explicit DNSNames matching the CR's service/pod FQDNs — this
//     is the cert that actually configures the live 8089 TLS listener via Ansible's
//     configure_server_cert.yml, so it's verified both by mount and by a real curl
//     TLS handshake against port 8089
//   - a no-role cert pointing at a pre-existing secret (not auto-generated), verified
//     only by mount
//
// Both certs are then rotated (by directly mutating their Secret's tls.crt/tls.key,
// since the operator never asks cert-manager to reissue once a Certificate CR
// exists) and the certRev annotation change is verified for each.
func RunS1CertTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, testenvInstance *testenv.TestEnv) {
	ns := testcaseEnvInst.GetName()
	c := testenvInstance.GetKubeClient()
	name := deployment.GetName()

	autogenSecret := "s1-autogen-server-cert"
	existingSecret := "s1-existing-cert"
	issuerName := "s1-selfsigned-issuer"

	Expect(testenv.CreateSelfSignedIssuer(ctx, c, ns, issuerName)).To(Succeed())
	Expect(testenv.CreateCustomCertSecret(ctx, c, ns, existingSecret)).To(Succeed())

	serviceFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-standalone-service", name), ns)
	podFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-standalone-0.splunk-%s-standalone-headless", name, name), ns)

	spec := enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: enterpriseApi.Spec{ImagePullPolicy: "Always", Image: testenvInstance.GetSplunkImage()},
			Certs: []enterpriseApi.CertSpec{
				{
					SecretRef:      corev1.LocalObjectReference{Name: autogenSecret},
					Role:           enterpriseApi.CertRoleServer,
					IssuerRef:      &enterpriseApi.IssuerReference{Name: issuerName},
					DNSNames:       []string{serviceFQDN, podFQDN},
					Duration:       &metav1.Duration{Duration: 24 * time.Hour},
					RenewBefore:    &metav1.Duration{Duration: 6 * time.Hour},
					RotationPolicy: enterpriseApi.PrivateKeyRotationPolicyAlways,
				},
				{
					SecretRef: corev1.LocalObjectReference{Name: existingSecret},
				},
			},
		},
	}
	_, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
	Expect(err).To(Succeed())

	podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
	stsName := fmt.Sprintf("splunk-%s-standalone", name)

	testenv.WaitForCertSecretKeys(ctx, deployment, autogenSecret, "tls.crt", "tls.key", "ca.crt")
	testenv.WaitForPodRunning(ctx, deployment, podName)

	verifyServerAutogenCertMounted(ctx, deployment, podName)
	testenv.VerifyServerCertTLS(ctx, deployment, podName, serviceFQDN)

	existingPath := "/mnt/tls/" + existingSecret
	testenv.VerifyCertFileMounted(ctx, deployment, podName, existingPath, "tls.crt")
	testenv.VerifyCertFileMounted(ctx, deployment, podName, existingPath, "tls.key")

	testenv.VerifyCertRevAnnotation(ctx, c, ns, stsName, autogenSecret)
	testenv.VerifyCertRevAnnotation(ctx, c, ns, stsName, existingSecret)

	initialAutogenHash := testenv.GetCertRevAnnotation(ctx, c, ns, stsName, autogenSecret)
	initialExistingHash := testenv.GetCertRevAnnotation(ctx, c, ns, stsName, existingSecret)

	testenv.RotateCertSecret(ctx, c, ns, autogenSecret)
	testenv.RotateCertSecret(ctx, c, ns, existingSecret)

	testenv.VerifyCertRotation(ctx, c, ns, stsName, autogenSecret, initialAutogenHash)
	testenv.VerifyCertRotation(ctx, c, ns, stsName, existingSecret, initialExistingHash)
}

// serverAutogenCertSpec returns a single-cert Role:server spec, missing at spec time so
// the operator auto-generates it via cert-manager (issuerName) with the given DNSNames.
func serverAutogenCertSpec(secretName, issuerName string, dnsNames []string) []enterpriseApi.CertSpec {
	return []enterpriseApi.CertSpec{
		{
			SecretRef: corev1.LocalObjectReference{Name: secretName},
			Role:      enterpriseApi.CertRoleServer,
			IssuerRef: &enterpriseApi.IssuerReference{Name: issuerName},
			DNSNames:  dnsNames,
		},
	}
}

// RunC3CertTest deploys ClusterManager, IndexerCluster, and SearchHeadCluster, each with its
// own Role:server auto-generated cert (distinct secret, explicit DNSNames matching that CR's
// own service/pod FQDNs). For each CR, verifies the cert is mounted at the fixed server-role
// path and that the CR's own pod under test presents a valid TLS cert on port 8089.
func RunC3CertTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, testenvInstance *testenv.TestEnv) {
	ns := testcaseEnvInst.GetName()
	c := testenvInstance.GetKubeClient()
	name := deployment.GetName()

	issuerName := "c3-selfsigned-issuer"
	Expect(testenv.CreateSelfSignedIssuer(ctx, c, ns, issuerName)).To(Succeed())

	splunkImage := testenvInstance.GetSplunkImage()

	// Deploy LicenseManager first when a license file is configured, with its own
	// Role:server auto-generated cert (distinct secret, explicit DNSNames matching
	// its own service/pod FQDNs), matching the pattern used for the other C3 CRs below.
	lmRef := ""
	if testenvInstance.HasLicenseFile() {
		lmSecret := "c3-lm-autogen-cert"
		lmServiceFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-license-manager-service", name), ns)
		lmPodFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-license-manager-0.splunk-%s-license-manager-headless", name, name), ns)
		lmSpec := enterpriseApi.LicenseManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec:  enterpriseApi.Spec{ImagePullPolicy: "Always", Image: splunkImage},
				Certs: serverAutogenCertSpec(lmSecret, issuerName, []string{lmServiceFQDN, lmPodFQDN}),
			},
		}
		_, err := deployment.DeployLicenseManagerWithGivenSpec(ctx, deployment.GetName(), lmSpec)
		Expect(err).To(Succeed(), "Unable to deploy LicenseManager")
		lmRef = deployment.GetName()

		lmPod := fmt.Sprintf(testenv.LicenseManagerPod, deployment.GetName(), 0)
		testenv.WaitForCertSecretKeys(ctx, deployment, lmSecret, "tls.crt", "tls.key", "ca.crt")
		testenv.WaitForPodRunning(ctx, deployment, lmPod)
		verifyServerAutogenCertMounted(ctx, deployment, lmPod)
		testenv.VerifyServerCertTLS(ctx, deployment, lmPod, lmServiceFQDN)
	}

	// Deploy ClusterManager — replicas is always 1 conceptually (single instance), matching
	// autoDNSNames(SplunkClusterManager, name, ns, 1)'s pod-0 branch.
	cmSecret := "c3-cm-autogen-cert"
	cmServiceFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-cluster-manager-service", name), ns)
	cmPodFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-cluster-manager-0.splunk-%s-cluster-manager-headless", name, name), ns)
	cmSpec := enterpriseApi.ClusterManagerSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec:  enterpriseApi.Spec{ImagePullPolicy: "Always", Image: splunkImage},
			Certs: serverAutogenCertSpec(cmSecret, issuerName, []string{cmServiceFQDN, cmPodFQDN}),
			// RF/SF set to 1 so a single-replica IndexerCluster (below) isn't
			// forcibly scaled up by verifyRFPeers to match the default RF of 3.
			Defaults: "splunk:\n  idxc:\n    replication_factor: 1\n    search_factor: 1\n",
		},
	}
	if lmRef != "" {
		cmSpec.CommonSplunkSpec.LicenseManagerRef = corev1.ObjectReference{Name: lmRef}
	}
	_, err := deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
	Expect(err).To(Succeed(), "Unable to deploy ClusterManager")

	cmPod := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
	testenv.WaitForCertSecretKeys(ctx, deployment, cmSecret, "tls.crt", "tls.key", "ca.crt")
	testenv.WaitForPodRunning(ctx, deployment, cmPod)
	verifyServerAutogenCertMounted(ctx, deployment, cmPod)
	testenv.VerifyServerCertTLS(ctx, deployment, cmPod, cmServiceFQDN)

	// Deploy IndexerCluster — replicas=1, matching the RF=1 set on ClusterManager above.
	idxcName := deployment.GetName() + "-idxc"
	idxcSecret := "c3-idxc-autogen-cert"
	idxcServiceFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-indexer-service", idxcName), ns)
	idxcHeadlessFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-indexer-headless", idxcName), ns)
	_, err = deployment.DeployIndexerClusterWithGivenSpec(ctx, idxcName, enterpriseApi.IndexerClusterSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec:              enterpriseApi.Spec{ImagePullPolicy: "Always", Image: splunkImage},
			Certs:             serverAutogenCertSpec(idxcSecret, issuerName, []string{idxcServiceFQDN, "*." + idxcHeadlessFQDN}),
			ClusterManagerRef: corev1.ObjectReference{Name: deployment.GetName()},
		},
		Replicas: 1,
	})
	Expect(err).To(Succeed(), "Unable to deploy IndexerCluster")

	idxcPod := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 0)
	testenv.WaitForCertSecretKeys(ctx, deployment, idxcSecret, "tls.crt", "tls.key", "ca.crt")
	testenv.WaitForPodRunning(ctx, deployment, idxcPod)
	verifyServerAutogenCertMounted(ctx, deployment, idxcPod)
	testenv.VerifyServerCertTLS(ctx, deployment, idxcPod, idxcServiceFQDN)

	// Deploy SearchHeadCluster — DNS SANs always include the wildcard SH headless SAN plus
	// the SH and deployer service FQDNs, matching autoDNSNamesSearchHeadCluster.
	shcName := deployment.GetName() + "-shc"
	shcSecret := "c3-shc-autogen-cert"
	shcServiceFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-search-head-service", shcName), ns)
	shcHeadlessFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-search-head-headless", shcName), ns)
	shcDeployerFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-deployer-service", shcName), ns)
	_, err = deployment.DeploySearchHeadClusterWithGivenSpec(ctx, shcName, enterpriseApi.SearchHeadClusterSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec:              enterpriseApi.Spec{ImagePullPolicy: "Always", Image: splunkImage},
			Certs:             serverAutogenCertSpec(shcSecret, issuerName, []string{shcServiceFQDN, shcDeployerFQDN, "*." + shcHeadlessFQDN}),
			ClusterManagerRef: corev1.ObjectReference{Name: deployment.GetName()},
		},
		Replicas: 1,
	})
	Expect(err).To(Succeed(), "Unable to deploy SearchHeadCluster")

	deployerPod := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
	testenv.WaitForCertSecretKeys(ctx, deployment, shcSecret, "tls.crt", "tls.key", "ca.crt")
	testenv.WaitForPodRunning(ctx, deployment, deployerPod)
	verifyServerAutogenCertMounted(ctx, deployment, deployerPod)
	testenv.VerifyServerCertTLS(ctx, deployment, deployerPod, shcDeployerFQDN)

	shPod := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
	testenv.WaitForPodRunning(ctx, deployment, shPod)
	verifyServerAutogenCertMounted(ctx, deployment, shPod)
	testenv.VerifyServerCertTLS(ctx, deployment, shPod, shcServiceFQDN)
}

// RunICCertTestTopology deploys an IngestorCluster (with its required Queue/ObjectStorage
// dependencies) with its own Role:server auto-generated cert (explicit DNSNames matching
// the CR's own service/pod FQDNs), following the same deployment/mount-verification pattern
// used for ClusterManager/IndexerCluster/SearchHeadCluster in RunC3CertTest, rather than the
// deploy-then-mutate workaround used by RunICCertTest. Unlike RunC3CertTest, this does not
// perform a live TLS handshake against port 8089 — the ingestor pod's splunkd startup is slow
// enough in this environment that the kubelet startup-probe budget expires before it's ready.
func RunICCertTestTopology(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, testenvInstance *testenv.TestEnv) {
	ns := testcaseEnvInst.GetName()
	c := testenvInstance.GetKubeClient()
	name := deployment.GetName()

	issuerName := "ic-topo-selfsigned-issuer"
	Expect(testenv.CreateSelfSignedIssuer(ctx, c, ns, issuerName)).To(Succeed())

	queue, err := deployment.DeployQueue(ctx, "ic-topo-queue", enterpriseApi.QueueSpec{
		Provider: "sqs",
		SQS: enterpriseApi.SQSSpec{
			Name:       "ic-topo-queue",
			AuthRegion: "us-west-2",
			DLQ:        "ic-topo-dlq",
		},
	})
	Expect(err).To(Succeed(), "Unable to deploy Queue")

	objStorage, err := deployment.DeployObjectStorage(ctx, "ic-topo-os", enterpriseApi.ObjectStorageSpec{
		Provider: "s3",
		S3:       enterpriseApi.S3Spec{Path: "ic-topo-bucket/key"},
	})
	Expect(err).To(Succeed(), "Unable to deploy ObjectStorage")

	icSecret := "ic-topo-autogen-cert"
	icServiceFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-ingestor-service", name), ns)
	icPodFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-ingestor-0.splunk-%s-ingestor-headless", name, name), ns)

	ic := &enterpriseApi.IngestorCluster{
		TypeMeta: metav1.TypeMeta{Kind: "IngestorCluster"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: enterpriseApi.IngestorClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec:  enterpriseApi.Spec{ImagePullPolicy: "Always", Image: testenvInstance.GetSplunkImage()},
				Certs: serverAutogenCertSpec(icSecret, issuerName, []string{icServiceFQDN, icPodFQDN}),
			},
			Replicas:         1,
			QueueRef:         corev1.ObjectReference{Name: queue.Name},
			ObjectStorageRef: corev1.ObjectReference{Name: objStorage.Name},
		},
	}
	_, err = deployment.DeployIngestorClusterWithAdditionalConfiguration(ctx, ic)
	Expect(err).To(Succeed(), "Unable to deploy IngestorCluster")

	icPod := fmt.Sprintf(testenv.IngestorPod, name, 0)
	testenv.WaitForCertSecretKeys(ctx, deployment, icSecret, "tls.crt", "tls.key", "ca.crt")
	testenv.WaitForPodRunning(ctx, deployment, icPod)
	verifyServerAutogenCertMounted(ctx, deployment, icPod)
}

// RunS1CertGCOnDeleteTest deploys a Standalone with one auto-generated cert
// (cert-manager Certificate + Secret, owned by the CR) and one customer-provided
// cert (a pre-existing Secret, never owned by the CR), then deletes the CR and
// verifies:
//   - the auto-generated Certificate is garbage-collected (it carries a
//     controller ownerReference to the CR — see EnsureCertificate)
//   - the auto-generated Secret is NOT deleted: cert-manager only cascades
//     Certificate deletion into its Secret when the cluster-wide
//     --enable-certificate-owner-ref flag is set on the cert-manager controller,
//     which defaults to false and is not set by this test's cert-manager install
//     (see test/testenv/certmanager_deps.go) — so the Secret is orphaned, not
//     deleted, by today's SOK + stock cert-manager behavior
//   - the customer-provided Secret is untouched, since SOK never creates an
//     ownerReference (or any Certificate) for a secret it did not generate
func RunS1CertGCOnDeleteTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, testenvInstance *testenv.TestEnv) {
	ns := testcaseEnvInst.GetName()
	c := testenvInstance.GetKubeClient()
	name := deployment.GetName()

	autogenSecret := "s1-gc-autogen-cert"
	existingSecret := "s1-gc-existing-cert"
	issuerName := "s1-gc-selfsigned-issuer"

	Expect(testenv.CreateSelfSignedIssuer(ctx, c, ns, issuerName)).To(Succeed())
	Expect(testenv.CreateCustomCertSecret(ctx, c, ns, existingSecret)).To(Succeed())

	serviceFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-standalone-service", name), ns)
	podFQDN := splunkServiceFQDN(fmt.Sprintf("splunk-%s-standalone-0.splunk-%s-standalone-headless", name, name), ns)

	spec := enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: enterpriseApi.Spec{ImagePullPolicy: "Always", Image: testenvInstance.GetSplunkImage()},
			Certs: []enterpriseApi.CertSpec{
				{
					SecretRef: corev1.LocalObjectReference{Name: autogenSecret},
					Role:      enterpriseApi.CertRoleServer,
					IssuerRef: &enterpriseApi.IssuerReference{Name: issuerName},
					DNSNames:  []string{serviceFQDN, podFQDN},
				},
				{
					SecretRef: corev1.LocalObjectReference{Name: existingSecret},
				},
			},
		},
	}
	standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, name, spec)
	Expect(err).To(Succeed())

	podName := fmt.Sprintf(testenv.StandalonePod, name, 0)
	testenv.WaitForCertSecretKeys(ctx, deployment, autogenSecret, "tls.crt", "tls.key", "ca.crt")
	testenv.WaitForPodRunning(ctx, deployment, podName)
	verifyServerAutogenCertMounted(ctx, deployment, podName)

	Expect(deployment.DeleteCR(ctx, standalone)).To(Succeed())

	testenv.VerifyCertificateDeleted(ctx, c, ns, autogenSecret)
	testenv.VerifySecretStillExists(ctx, c, ns, autogenSecret)
	testenv.VerifySecretStillExists(ctx, c, ns, existingSecret)
}
