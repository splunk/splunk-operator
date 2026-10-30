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
package postgrescontrollers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/pem"
	"math/big"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pghelpers "github.com/splunk/splunk-operator/test/postgrescontrollers/helpers"
	"github.com/splunk/splunk-operator/test/testenv"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed testdata/postgres_mounts_probe.sh
var mountedPostgresProbe string

var _ = Describe("PostgreSQL connections from mounted Splunk resources", func() {
	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var pgClass *platformApi.PostgresClusterClass

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		pgClass = nil
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed(), "Failed to set up mounted-connection test")
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		// Keep the class available until namespace cleanup finishes PostgreSQL
		// finalization. Preserve it alongside the namespace on failed tests.
		Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed())
		if pgClass != nil && testcaseEnvInst != nil && !testcaseEnvInst.SkipTeardown {
			err := testenvInstance.GetKubeClient().Delete(ctx, pgClass)
			if !apierrors.IsNotFound(err) {
				Expect(err).To(Succeed(), "Failed to delete the test PostgresClusterClass")
			}
		}
	})

	for _, tier := range []struct{ kind, topology string }{
		{"Standalone", "s1"},
		{"SearchHeadCluster", "shc"},
	} {
		It("connects from "+tier.kind+" using mounted PostgreSQL details and verify-full",
			Label("tier:e2e-full", "sva:"+tier.topology, "cloud:any", "variant:manager", "feature:postgres", "scenario:mounted-connection"),
			NodeTimeout(testenv.MediumTimeout),
			func(ctx SpecContext) {
				namespace := testcaseEnvInst.GetName()
				c := testcaseEnvInst.GetKubeClient()
				By("creating a PostgreSOK database with CNPG default TLS")
				pgImages, imageErr := pghelpers.PostgresImages()
				Expect(imageErr).To(Succeed(), "A pinned PostgreSQL image is required for the mounted-connection test")
				pgClass = &platformApi.PostgresClusterClass{
					Name: "pg-mounts-" + namespace,
					Spec: platformApi.PostgresClusterClassSpec{
						Provisioner: "postgresql.cnpg.io",
						Config:      &platformApi.PostgresClusterClassConfig{Instances: new(int32(1))},
						CNPG:        &platformApi.CNPGConfig{},
						TLS:         &platformApi.PostgresClusterClassTLS{Certificates: &platformApi.PostgresCertificateConfig{Mode: new(platformApi.PostgresCertificateModeCNPGDefault)}},
					},
				}
				Expect(c.Create(ctx, pgClass)).To(Succeed())
				cluster := &platformApi.PostgresCluster{
					Name: "mounted-postgres", Namespace: namespace,
					Spec: platformApi.PostgresClusterSpec{
						Class: pgClass.Name, ClusterDeletionPolicy: new("Delete"), Images: pgImages,
						// Splunk 10.2's bundled client negotiates TLS 1.2. Keep
						// verify-full on the client; do not change platform defaults.
						PostgreSQLConfig: map[string]string{
							"ssl_min_protocol_version": "TLSv1.2",
							"ssl_max_protocol_version": "TLSv1.3",
						},
					},
				}
				Expect(c.Create(ctx, cluster)).To(Succeed())
				pghelpers.WaitForReadyPostgresCluster(ctx, c, client.ObjectKeyFromObject(cluster))
				database := pghelpers.CreateReadyPostgresDatabase(ctx, c, namespace, "mounted-database", cluster.Name, "appdb")

				By("deploying the Splunk workload with the dedicated database reference")
				name := deployment.GetName()
				common := enterpriseApi.CommonSplunkSpec{
					Image: testcaseEnvInst.GetSplunkImage(), ImagePullPolicy: "IfNotPresent",
					EtcVolumeStorageConfig: enterpriseApi.StorageClassSpec{EphemeralStorage: true},
					VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{EphemeralStorage: true},
				}
				mounts := []enterpriseApi.PostgresDatabaseMountSpec{{
					Name: "connection", PostgresDatabaseRef: corev1.LocalObjectReference{Name: database.Name}, Database: "appdb", IncludeAdmin: true,
				}}
				statefulSetName := "splunk-" + name + "-standalone"
				expectedPods := int32(1)
				if tier.kind == "Standalone" {
					standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, name, enterpriseApi.StandaloneSpec{
						CommonSplunkSpec: common, Replicas: 1, PostgresDatabaseMounts: mounts,
					})
					Expect(err).To(Succeed())
					Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, name, standalone)).To(Succeed())
					Expect(testenv.VerifyCRConditionForGeneration("Standalone", standalone.Name, standalone.Status.Conditions, enterpriseApi.ConditionPostgresDatabaseResolved, metav1.ConditionTrue, standalone.Generation)).To(Succeed())
				} else {
					name += "-shc"
					statefulSetName, expectedPods = "splunk-"+name+"-search-head", 3
					shc, err := deployment.DeploySearchHeadClusterWithGivenSpec(ctx, name, enterpriseApi.SearchHeadClusterSpec{
						CommonSplunkSpec: common, Replicas: expectedPods, PostgresDatabaseMounts: mounts,
					})
					Expect(err).To(Succeed())
					Expect(testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)).To(Succeed())
					Expect(c.Get(ctx, client.ObjectKeyFromObject(shc), shc)).To(Succeed())
					Expect(testenv.VerifySearchHeadClusterReadyStatus(shc, enterpriseApi.ConditionPostgresDatabaseResolved)).To(Succeed())
				}

				By("verifying the operator's StatefulSet and ready pod count")
				statefulSet := &appsv1.StatefulSet{}
				pods := &corev1.PodList{}
				Eventually(func(g Gomega) {
					g.Expect(c.Get(ctx, types.NamespacedName{Name: statefulSetName, Namespace: namespace}, statefulSet)).To(Succeed())
					g.Expect(statefulSet.Spec.Replicas).To(HaveValue(Equal(expectedPods)))
					g.Expect(statefulSet.Status.ReadyReplicas).To(Equal(expectedPods))
					g.Expect(statefulSet.Spec.Selector).NotTo(BeNil())
					if statefulSet.Spec.Selector == nil {
						return
					}
					g.Expect(c.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabels(statefulSet.Spec.Selector.MatchLabels))).To(Succeed())
					g.Expect(pods.Items).To(HaveLen(int(expectedPods)))
					for _, pod := range pods.Items {
						g.Expect(testenv.VerifyPodReady(&pod, "splunk")).To(Succeed())
					}
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				script, err := mountedPostgresProbeScript()
				Expect(err).To(Succeed())
				for _, pod := range pods.Items {
					By("proving RW CRUD and strict TLS from the splunk container in " + pod.Name)
					execCtx, cancel := context.WithTimeout(ctx, testenv.ReadinessPollTimeout)
					stdout, stderr, execErr := deployment.PodExecCommandInNamespace(execCtx, pod.Name, namespace, "splunk", []string{"/bin/bash", "-s", "--", "appdb"}, script, false)
					cancel()
					Expect(execErr).To(Succeed(), "Mounted PostgreSQL probe failed in %s: %s", pod.Name, stderr)
					Expect(strings.TrimSpace(stdout)).To(Equal("CRUD_OK\nWRONG_CA_REJECTED\nHOSTNAME_MISMATCH_REJECTED"))
				}
			})
	}
})

// Only this unrelated CA's public certificate enters the script. Its private
// key stays in the test process and mounted credentials never leave the pod.
func mountedPostgresProbeScript() (string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "postgresok-e2e-untrusted"},
		IsCA:         true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return strings.Replace(mountedPostgresProbe, "__POSTGRESOK_UNTRUSTED_CA_PEM__", string(ca), 1), nil
}
