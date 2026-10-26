// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package noah

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe(
	"Noah-backed C3 Kraken AWS runtime",
	Serial,
	Label("tier:noah-e2e", "sva:c3", "cloud:kraken", "variant:noah", "feature:noah", "scenario:aws-runtime"),
	func() {
		// These checks distinguish declared configuration, effective Splunk
		// configuration, and the workload identity used for real AWS requests.
		It(
			"uses Kraken workload identity without static AWS credentials",
			NodeTimeout(testenv.MediumLongTimeout),
			func(ctx SpecContext) {
				expected, err := checkKrakenExternalStorage(ctx)
				Expect(err).NotTo(HaveOccurred(), "Kraken external storage is not wired as expected")
				topology := waitForC3Ready(ctx)

				idxc := &enterpriseApi.IndexerCluster{}
				Expect(testcaseEnvInstance.GetKubeClient().Get(
					ctx,
					types.NamespacedName{Namespace: operatorNamespace, Name: clusterName},
					idxc,
				)).To(Succeed())

				Expect(idxc.Spec.ServiceAccount).To(Equal(expected.serviceAccount))
				extraEnv := envByName(idxc.Spec.ExtraEnv)
				Expect(extraEnv).NotTo(HaveKey("AWS_ACCESS_KEY_ID"))
				Expect(extraEnv).NotTo(HaveKey("AWS_SECRET_ACCESS_KEY"))
				Expect(extraEnv).To(HaveKeyWithValue("AWS_DEFAULT_REGION", expected.authRegion))

				for _, podName := range topology.IndexerPods {
					By("checking Kraken workload identity on " + podName)
					pod := &corev1.Pod{}
					Expect(testcaseEnvInstance.GetKubeClient().Get(
						ctx,
						types.NamespacedName{Namespace: operatorNamespace, Name: podName},
						pod,
					)).To(Succeed())
					Expect(pod.Spec.ServiceAccountName).To(Equal(expected.serviceAccount))

					splunkContainer, found := containerByName(pod.Spec.Containers, "splunk")
					Expect(found).To(BeTrue(), "Pod %s has no splunk container", podName)
					podEnv := envByName(splunkContainer.Env)
					Expect(podEnv).NotTo(HaveKey("AWS_ACCESS_KEY_ID"))
					Expect(podEnv).NotTo(HaveKey("AWS_SECRET_ACCESS_KEY"))

					// IRSA mutation occurs on the host-side Pod after vCluster sync, so
					// prove the identity from inside the running container rather than
					// relying on the virtual Pod's copied spec.
					_, stderr, err := deployment.PodExecCommand(
						ctx,
						podName,
						[]string{
							"/bin/sh",
							"-ec",
							`test -z "${AWS_ACCESS_KEY_ID:-}" &&
test -z "${AWS_SECRET_ACCESS_KEY:-}" &&
test -z "${AWS_SESSION_TOKEN:-}" &&
test -n "${AWS_ROLE_ARN:-}" &&
test -n "${AWS_WEB_IDENTITY_TOKEN_FILE:-}" &&
test -s "$AWS_WEB_IDENTITY_TOKEN_FILE"`,
						},
						"",
						false,
					)
					Expect(err).To(Succeed(), "workload identity is incomplete on %s: %s", podName, stderr)
				}
			},
		)

		It(
			"applies the Kraken S3 and SQS coordinates to every indexer",
			NodeTimeout(testenv.MediumLongTimeout),
			func(ctx SpecContext) {
				expected, err := checkKrakenExternalStorage(ctx)
				Expect(err).NotTo(HaveOccurred(), "Kraken external storage is not wired as expected")
				topology := waitForC3Ready(ctx)

				for _, podName := range topology.IndexerPods {
					By("checking effective SmartStore configuration on " + podName)
					settings, err := effectiveIndexesSettings(ctx, podName, "volume:remote_store")
					Expect(err).NotTo(HaveOccurred())
					for key, value := range map[string]string{
						"storageType":                   "remote",
						"path":                          expected.objectStorePath,
						"remote.s3.endpoint":            expected.objectStoreEndpoint,
						"remote.s3.signature_version":   "v4",
						"remote.s3.auth_region":         expected.authRegion,
						"remote.s3.sqs.endpoint":        expected.queueEndpoint,
						"remote.s3.sqs.queue_url":       expected.queueURL,
						"remote.s3.supports_versioning": "false",
					} {
						actual, found := settings[key]
						Expect(found).To(BeTrue(), "%s was not present in effective indexes.conf on %s", key, podName)
						Expect(actual).To(Equal(value), "%s had the wrong value on %s", key, podName)
					}

					mainSettings, err := effectiveIndexesSettings(ctx, podName, "main")
					Expect(err).NotTo(HaveOccurred())
					Expect(mainSettings).To(HaveKeyWithValue("remotePath", "volume:remote_store/main"),
						"main did not use the SmartStore volume on %s", podName)
				}
			},
		)
	},
)
