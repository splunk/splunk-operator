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

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/internal/controller/testutils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pkg/errors"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	enterprise "github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"k8s.io/client-go/tools/record"
)

var _ = Describe("IngestorCluster Controller", Label("integration"), func() {
	BeforeEach(func() {
		time.Sleep(2 * time.Second)
	})

	AfterEach(func() {

	})

	Context("IngestorCluster Management", func() {

		It("Get IngestorCluster custom resource should fail", func() {
			namespace := "ns-splunk-ing-1"
			ApplyIngestorCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			_, err := GetIngestorCluster("test", nsSpecs.Name)
			Expect(err.Error()).Should(Equal("ingestorclusters.enterprise.splunk.com \"test\" not found"))

			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create IngestorCluster custom resource with annotations should pause", func() {
			namespace := "ns-splunk-ing-2"
			annotations := make(map[string]string)
			annotations[enterpriseApi.IngestorClusterPausedAnnotation] = "true"
			ApplyIngestorCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			queue := &enterpriseApi.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "queue",
					Namespace: nsSpecs.Name,
				},
				Spec: enterpriseApi.QueueSpec{
					Provider: "sqs",
					SQS: enterpriseApi.SQSSpec{
						Name:       "smartbus-queue",
						AuthRegion: "us-west-2",
						DLQ:        "smartbus-dlq",
						Endpoint:   "https://sqs.us-west-2.amazonaws.com",
					},
				},
			}
			os := &enterpriseApi.ObjectStorage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "os",
					Namespace: nsSpecs.Name,
				},
				Spec: enterpriseApi.ObjectStorageSpec{
					Provider: "s3",
					S3: enterpriseApi.S3Spec{
						Endpoint: "https://s3.us-west-2.amazonaws.com",
						Path:     "ingestion/smartbus-test",
					},
				},
			}
			CreateIngestorCluster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady, os, queue)
			icSpec, _ := GetIngestorCluster("test", nsSpecs.Name)
			annotations = map[string]string{}
			icSpec.Annotations = annotations
			icSpec.Status.Phase = "Ready"
			UpdateIngestorCluster(icSpec, enterpriseApi.PhaseReady, os, queue)
			DeleteIngestorCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Create IngestorCluster custom resource should succeeded", func() {
			namespace := "ns-splunk-ing-3"
			ApplyIngestorCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			annotations := make(map[string]string)
			queue := &enterpriseApi.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "queue",
					Namespace: nsSpecs.Name,
				},
				Spec: enterpriseApi.QueueSpec{
					Provider: "sqs",
					SQS: enterpriseApi.SQSSpec{
						Name:       "smartbus-queue",
						AuthRegion: "us-west-2",
						DLQ:        "smartbus-dlq",
						Endpoint:   "https://sqs.us-west-2.amazonaws.com",
					},
				},
			}
			os := &enterpriseApi.ObjectStorage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "os",
					Namespace: nsSpecs.Name,
				},
				Spec: enterpriseApi.ObjectStorageSpec{
					Provider: "s3",
					S3: enterpriseApi.S3Spec{
						Endpoint: "https://s3.us-west-2.amazonaws.com",
						Path:     "ingestion/smartbus-test",
					},
				},
			}
			CreateIngestorCluster("test", nsSpecs.Name, annotations, enterpriseApi.PhaseReady, os, queue)
			DeleteIngestorCluster("test", nsSpecs.Name)
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("Cover Unused methods", func() {
			namespace := "ns-splunk-ing-4"
			ApplyIngestorCluster = func(ctx context.Context, client client.Client, instance *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
				return reconcile.Result{}, nil
			}
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			queue := &enterpriseApi.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "queue",
					Namespace: nsSpecs.Name,
				},
				Spec: enterpriseApi.QueueSpec{
					Provider: "sqs",
					SQS: enterpriseApi.SQSSpec{
						Name:       "smartbus-queue",
						AuthRegion: "us-west-2",
						DLQ:        "smartbus-dlq",
						Endpoint:   "https://sqs.us-west-2.amazonaws.com",
					},
				},
			}
			os := &enterpriseApi.ObjectStorage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "os",
					Namespace: nsSpecs.Name,
				},
				Spec: enterpriseApi.ObjectStorageSpec{
					Provider: "s3",
					S3: enterpriseApi.S3Spec{
						Endpoint: "https://s3.us-west-2.amazonaws.com",
						Path:     "ingestion/smartbus-test",
					},
				},
			}

			ctx := context.TODO()
			builder := fake.NewClientBuilder().WithStatusSubresource(&enterpriseApi.IngestorCluster{})
			c := builder.Build()
			instance := IngestorClusterReconciler{
				Client: c,
				Scheme: scheme.Scheme,
			}
			request := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test",
					Namespace: namespace,
				},
			}
			_, err := instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())

			icSpec := testutils.NewIngestorCluster("test", namespace, "image", os, queue)
			Expect(c.Create(ctx, icSpec)).Should(Succeed())

			annotations := make(map[string]string)
			annotations[enterpriseApi.IngestorClusterPausedAnnotation] = "true"
			icSpec.Annotations = annotations
			Expect(c.Update(ctx, icSpec)).Should(Succeed())

			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			// verify Paused=True condition was written
			Expect(c.Get(ctx, request.NamespacedName, icSpec)).Should(Succeed())
			pausedCond := meta.FindStatusCondition(icSpec.Status.Conditions, string(enterpriseApi.ConditionPaused))
			Expect(pausedCond).ToNot(BeNil())
			Expect(pausedCond.Status).To(Equal(metav1.ConditionTrue))

			annotations = map[string]string{}
			icSpec.Annotations = annotations
			Expect(c.Update(ctx, icSpec)).Should(Succeed())

			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			// verify Paused=False condition was written
			Expect(c.Get(ctx, request.NamespacedName, icSpec)).Should(Succeed())
			pausedCond = meta.FindStatusCondition(icSpec.Status.Conditions, string(enterpriseApi.ConditionPaused))
			Expect(pausedCond).ToNot(BeNil())
			Expect(pausedCond.Status).To(Equal(metav1.ConditionFalse))

			icSpec.DeletionTimestamp = &metav1.Time{}
			_, err = instance.Reconcile(ctx, request)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Reconcile emits Stalled Warning on every terminal failure reconcile", func() {
			namespace := "ns-splunk-ing-stalled"
			ctx := context.TODO()
			builder := fake.NewClientBuilder().WithStatusSubresource(&enterpriseApi.IngestorCluster{})
			c := builder.Build()
			recorder := record.NewFakeRecorder(10)
			reconciler := IngestorClusterReconciler{
				Client:   c,
				Scheme:   scheme.Scheme,
				Recorder: recorder,
			}
			objStorage := &enterpriseApi.ObjectStorage{
				ObjectMeta: metav1.ObjectMeta{Name: "os", Namespace: namespace},
				Spec:       enterpriseApi.ObjectStorageSpec{Provider: "s3", S3: enterpriseApi.S3Spec{Path: "test/path"}},
			}
			queue := &enterpriseApi.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "queue", Namespace: namespace},
				Spec: enterpriseApi.QueueSpec{
					Provider: "sqs",
					SQS:      enterpriseApi.SQSSpec{Name: "q", AuthRegion: "us-west-2", DLQ: "dlq"},
				},
			}
			icSpec := testutils.NewIngestorCluster("test", namespace, "image", objStorage, queue)
			Expect(c.Create(ctx, icSpec)).Should(Succeed())

			ApplyIngestorCluster = func(ctx context.Context, cl client.Client, instance *enterpriseApi.IngestorCluster) (reconcile.Result, error) {
				return reconcile.Result{}, splcommon.NewTerminalError("ValidateSpecFailed", "test terminal failure", fmt.Errorf("test"))
			}

			request := reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test", Namespace: namespace},
			}

			// First reconcile: Stalled=False → Stalled=True — Stalled event expected
			_, err := reconciler.Reconcile(ctx, request)
			Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
			Eventually(recorder.Events).Should(Receive(MatchRegexp(`^Warning ` + enterprise.EventReasonStalled + ` `)))

			// Second reconcile: Stalled=True → Stalled=True — Warning fires on every stalled reconcile
			_, err = reconciler.Reconcile(ctx, request)
			Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
			Eventually(recorder.Events).Should(Receive(MatchRegexp(`^Warning ` + enterprise.EventReasonStalled + ` `)))
		})

	})

	Context("Queue spec immutability", func() {

		It("should allow idempotent update without endpoint", func() {
			namespace := "ns-imm-queue-1"
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			queue := &enterpriseApi.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "queue-no-ep",
					Namespace: namespace,
				},
				Spec: enterpriseApi.QueueSpec{
					Provider: "sqs",
					SQS: enterpriseApi.SQSSpec{
						Name:       "test-queue",
						AuthRegion: "us-west-2",
						DLQ:        "test-dlq",
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), queue)).Should(Succeed())

			fetched := &enterpriseApi.Queue{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Name: queue.Name, Namespace: namespace,
			}, fetched)).Should(Succeed())

			Expect(k8sClient.Update(context.Background(), fetched)).Should(Succeed())

			Expect(k8sClient.Delete(context.Background(), fetched)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("should allow idempotent update with endpoint", func() {
			namespace := "ns-imm-queue-2"
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			queue := &enterpriseApi.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "queue-with-ep",
					Namespace: namespace,
				},
				Spec: enterpriseApi.QueueSpec{
					Provider: "sqs",
					SQS: enterpriseApi.SQSSpec{
						Name:       "test-queue",
						AuthRegion: "us-west-2",
						DLQ:        "test-dlq",
						Endpoint:   "https://sqs.us-west-2.amazonaws.com",
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), queue)).Should(Succeed())

			fetched := &enterpriseApi.Queue{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Name: queue.Name, Namespace: namespace,
			}, fetched)).Should(Succeed())

			Expect(k8sClient.Update(context.Background(), fetched)).Should(Succeed())

			Expect(k8sClient.Delete(context.Background(), fetched)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		DescribeTable("should reject immutability violations",
			func(namespace, queueName string, initialSpec enterpriseApi.QueueSpec, mutate func(*enterpriseApi.Queue), wantErr string) {
				nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
				Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

				queue := &enterpriseApi.Queue{
					ObjectMeta: metav1.ObjectMeta{Name: queueName, Namespace: namespace},
					Spec:       initialSpec,
				}
				Expect(k8sClient.Create(context.Background(), queue)).Should(Succeed())

				fetched := &enterpriseApi.Queue{}
				Expect(k8sClient.Get(context.Background(), types.NamespacedName{
					Name: queue.Name, Namespace: namespace,
				}, fetched)).Should(Succeed())

				mutate(fetched)
				err := k8sClient.Update(context.Background(), fetched)
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring(wantErr))

				Expect(k8sClient.Delete(context.Background(), queue)).Should(Succeed())
				Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
			},
			Entry("changes endpoint",
				"ns-imm-queue-3", "queue-change-ep",
				enterpriseApi.QueueSpec{Provider: "sqs", SQS: enterpriseApi.SQSSpec{Name: "test-queue", AuthRegion: "us-west-2", DLQ: "test-dlq", Endpoint: "https://sqs.us-west-2.amazonaws.com"}},
				func(q *enterpriseApi.Queue) { q.Spec.SQS.Endpoint = "https://sqs.eu-west-1.amazonaws.com" },
				"sqs.endpoint is immutable once created",
			),
			Entry("changes provider",
				"ns-imm-queue-4", "queue-change-prov",
				enterpriseApi.QueueSpec{Provider: "sqs", SQS: enterpriseApi.SQSSpec{Name: "test-queue", AuthRegion: "us-west-2", DLQ: "test-dlq"}},
				func(q *enterpriseApi.Queue) { q.Spec.Provider = "sqs_cp" },
				"provider is immutable once created",
			),
			Entry("adds endpoint after creation",
				"ns-imm-queue-5", "queue-add-ep",
				enterpriseApi.QueueSpec{Provider: "sqs", SQS: enterpriseApi.SQSSpec{Name: "test-queue", AuthRegion: "us-west-2", DLQ: "test-dlq"}},
				func(q *enterpriseApi.Queue) { q.Spec.SQS.Endpoint = "https://sqs.us-west-2.amazonaws.com" },
				"sqs.endpoint is immutable once created",
			),
			Entry("removes endpoint after creation",
				"ns-imm-queue-6", "queue-remove-ep",
				enterpriseApi.QueueSpec{Provider: "sqs", SQS: enterpriseApi.SQSSpec{Name: "test-queue", AuthRegion: "us-west-2", DLQ: "test-dlq", Endpoint: "https://sqs.us-west-2.amazonaws.com"}},
				func(q *enterpriseApi.Queue) { q.Spec.SQS.Endpoint = "" },
				"sqs.endpoint is immutable once created",
			),
		)

		It("should allow metadata update when endpoint is unset (regression)", func() {
			namespace := "ns-imm-queue-7"
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			queue := &enterpriseApi.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "queue-meta", Namespace: namespace},
				Spec: enterpriseApi.QueueSpec{
					Provider: "sqs",
					SQS: enterpriseApi.SQSSpec{
						Name:       "test-queue",
						AuthRegion: "us-west-2",
						DLQ:        "test-dlq",
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), queue)).Should(Succeed())

			fetched := &enterpriseApi.Queue{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Name: queue.Name, Namespace: namespace,
			}, fetched)).Should(Succeed())

			if fetched.Labels == nil {
				fetched.Labels = map[string]string{}
			}
			fetched.Labels["foo"] = "bar"
			Expect(k8sClient.Update(context.Background(), fetched)).Should(Succeed())

			Expect(k8sClient.Delete(context.Background(), queue)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})
	})

	Context("IngestorCluster spec mutability", func() {

		It("should allow update that changes queueRef and objectStorageRef", func() {
			namespace := "ns-mut-ing-1"
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			queue := &enterpriseApi.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "queue-mut",
					Namespace: namespace,
				},
				Spec: enterpriseApi.QueueSpec{
					Provider: "sqs",
					SQS: enterpriseApi.SQSSpec{
						Name:       "test-queue",
						AuthRegion: "us-west-2",
						DLQ:        "test-dlq",
					},
				},
			}
			os := &enterpriseApi.ObjectStorage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "os-mut",
					Namespace: namespace,
				},
				Spec: enterpriseApi.ObjectStorageSpec{
					Provider: "s3",
					S3: enterpriseApi.S3Spec{
						Path: "ingestion/test",
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), queue)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), os)).Should(Succeed())

			ing := &enterpriseApi.IngestorCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ing-mut",
					Namespace: namespace,
				},
				Spec: enterpriseApi.IngestorClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "IfNotPresent",
						},
					},
					Replicas: 1,
					QueueRef: corev1.ObjectReference{
						Name:      queue.Name,
						Namespace: namespace,
					},
					ObjectStorageRef: corev1.ObjectReference{
						Name:      os.Name,
						Namespace: namespace,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), ing)).Should(Succeed())

			fetched := &enterpriseApi.IngestorCluster{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Name: ing.Name, Namespace: namespace,
			}, fetched)).Should(Succeed())

			fetched.Spec.QueueRef.Name = "different-queue"
			fetched.Spec.ObjectStorageRef.Name = "different-os"
			Expect(k8sClient.Update(context.Background(), fetched)).Should(Succeed())

			Expect(k8sClient.Delete(context.Background(), ing)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), os)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), queue)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("should allow idempotent update with same queueRef and objectStorageRef", func() {
			namespace := "ns-mut-ing-2"
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			queue := &enterpriseApi.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "queue-idem",
					Namespace: namespace,
				},
				Spec: enterpriseApi.QueueSpec{
					Provider: "sqs",
					SQS: enterpriseApi.SQSSpec{
						Name:       "test-queue",
						AuthRegion: "us-west-2",
						DLQ:        "test-dlq",
					},
				},
			}
			os := &enterpriseApi.ObjectStorage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "os-idem",
					Namespace: namespace,
				},
				Spec: enterpriseApi.ObjectStorageSpec{
					Provider: "s3",
					S3: enterpriseApi.S3Spec{
						Path: "ingestion/test",
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), queue)).Should(Succeed())
			Expect(k8sClient.Create(context.Background(), os)).Should(Succeed())

			ing := &enterpriseApi.IngestorCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ing-idem",
					Namespace: namespace,
				},
				Spec: enterpriseApi.IngestorClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "IfNotPresent",
						},
					},
					Replicas: 1,
					QueueRef: corev1.ObjectReference{
						Name:      queue.Name,
						Namespace: namespace,
					},
					ObjectStorageRef: corev1.ObjectReference{
						Name:      os.Name,
						Namespace: namespace,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), ing)).Should(Succeed())

			fetched := &enterpriseApi.IngestorCluster{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Name: ing.Name, Namespace: namespace,
			}, fetched)).Should(Succeed())

			Expect(k8sClient.Update(context.Background(), fetched)).Should(Succeed())

			Expect(k8sClient.Delete(context.Background(), ing)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), os)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), queue)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})
	})

	Context("IndexerCluster spec mutability", func() {

		It("should allow idempotent update with queueRef and objectStorageRef", func() {
			namespace := "ns-imm-idxc-1"
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			idxc := &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "idxc-imm",
					Namespace: namespace,
				},
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "IfNotPresent",
						},
					},
					Replicas: 1,
					QueueRef: &corev1.ObjectReference{
						Name:      "my-queue",
						Namespace: namespace,
					},
					ObjectStorageRef: &corev1.ObjectReference{
						Name:      "my-os",
						Namespace: namespace,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), idxc)).Should(Succeed())

			Eventually(func() error {
				latest := &enterpriseApi.IndexerCluster{}
				if err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name: idxc.Name, Namespace: namespace,
				}, latest); err != nil {
					return err
				}
				return k8sClient.Update(context.Background(), latest)
			}, timeout, interval).Should(Succeed())

			fetched := &enterpriseApi.IndexerCluster{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Name: idxc.Name, Namespace: namespace,
			}, fetched)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), fetched)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("should allow update that changes queueRef and objectStorageRef", func() {
			namespace := "ns-imm-idxc-2"
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			idxc := &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "idxc-change-q",
					Namespace: namespace,
				},
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "IfNotPresent",
						},
					},
					Replicas: 1,
					QueueRef: &corev1.ObjectReference{
						Name:      "my-queue",
						Namespace: namespace,
					},
					ObjectStorageRef: &corev1.ObjectReference{
						Name:      "my-os",
						Namespace: namespace,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), idxc)).Should(Succeed())

			Eventually(func() error {
				latest := &enterpriseApi.IndexerCluster{}
				if err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name: idxc.Name, Namespace: namespace,
				}, latest); err != nil {
					return err
				}
				latest.Spec.QueueRef.Name = "different-queue"
				latest.Spec.ObjectStorageRef.Name = "different-os"
				return k8sClient.Update(context.Background(), latest)
			}, timeout, interval).Should(Succeed())

			fetched := &enterpriseApi.IndexerCluster{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Name: idxc.Name, Namespace: namespace,
			}, fetched)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), fetched)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("should allow idempotent update without queueRef and objectStorageRef", func() {
			namespace := "ns-imm-idxc-3"
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			idxc := &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "idxc-no-refs",
					Namespace: namespace,
				},
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "IfNotPresent",
						},
					},
					Replicas: 1,
				},
			}
			Expect(k8sClient.Create(context.Background(), idxc)).Should(Succeed())

			Eventually(func() error {
				fetched := &enterpriseApi.IndexerCluster{}
				if err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name: idxc.Name, Namespace: namespace,
				}, fetched); err != nil {
					return err
				}
				return k8sClient.Update(context.Background(), fetched)
			}, timeout, interval).Should(Succeed())

			fetched := &enterpriseApi.IndexerCluster{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Name: idxc.Name, Namespace: namespace,
			}, fetched)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), fetched)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})
	})

	Context("CommonSplunkSpec certs uniqueness", func() {

		It("should accept a resource with no certs entries", func() {
			namespace := "ns-certs-0"
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			idxc := &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "idxc-certs-empty", Namespace: namespace},
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{ImagePullPolicy: "IfNotPresent"},
					},
					Replicas: 1,
				},
			}
			Expect(k8sClient.Create(context.Background(), idxc)).Should(Succeed())

			fetched := &enterpriseApi.IndexerCluster{}
			Eventually(func() error {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name: idxc.Name, Namespace: namespace,
				}, fetched); err != nil {
					return err
				}
				Expect(fetched.Spec.Certs).To(BeEmpty())
				fetched.Spec.Certs = []enterpriseApi.CertSpec{}
				return k8sClient.Update(context.Background(), fetched)
			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(context.Background(), idxc)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		It("should accept one server, one input and multiple no-role certs", func() {
			namespace := "ns-certs-1"
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			idxc := &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "idxc-certs-ok", Namespace: namespace},
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{ImagePullPolicy: "IfNotPresent"},
						Certs: []enterpriseApi.CertSpec{
							{SecretRef: corev1.LocalObjectReference{Name: "server-cert"}, Role: enterpriseApi.CertRoleServer},
							{SecretRef: corev1.LocalObjectReference{Name: "input-cert"}, Role: enterpriseApi.CertRoleInput},
							{SecretRef: corev1.LocalObjectReference{Name: "ca-1"}},
							{SecretRef: corev1.LocalObjectReference{Name: "ca-2"}},
						},
					},
					Replicas: 1,
				},
			}
			Expect(k8sClient.Create(context.Background(), idxc)).Should(Succeed())

			Expect(k8sClient.Delete(context.Background(), idxc)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})

		DescribeTable("should reject duplicate cert roles on create",
			func(namespace, idxcName string, role enterpriseApi.CertRole, certA, certB string) {
				nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
				Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

				idxc := &enterpriseApi.IndexerCluster{
					ObjectMeta: metav1.ObjectMeta{Name: idxcName, Namespace: namespace},
					Spec: enterpriseApi.IndexerClusterSpec{
						CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
							Spec: enterpriseApi.Spec{ImagePullPolicy: "IfNotPresent"},
							Certs: []enterpriseApi.CertSpec{
								{SecretRef: corev1.LocalObjectReference{Name: certA}, Role: role},
								{SecretRef: corev1.LocalObjectReference{Name: certB}, Role: role},
							},
						},
						Replicas: 1,
					},
				}
				err := k8sClient.Create(context.Background(), idxc)
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("at most one entry per role is allowed"))

				Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
			},
			Entry("role=server", "ns-certs-2", "idxc-certs-dup-server", enterpriseApi.CertRoleServer, "server-a", "server-b"),
			Entry("role=input", "ns-certs-3", "idxc-certs-dup-input", enterpriseApi.CertRoleInput, "input-a", "input-b"),
		)

		It("should reject duplicate role added by update", func() {
			namespace := "ns-certs-4"
			nsSpecs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			Expect(k8sClient.Create(context.Background(), nsSpecs)).Should(Succeed())

			idxc := &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "idxc-certs-update", Namespace: namespace},
				Spec: enterpriseApi.IndexerClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						Spec: enterpriseApi.Spec{ImagePullPolicy: "IfNotPresent"},
						Certs: []enterpriseApi.CertSpec{
							{SecretRef: corev1.LocalObjectReference{Name: "server-a"}, Role: enterpriseApi.CertRoleServer},
						},
					},
					Replicas: 1,
				},
			}
			Expect(k8sClient.Create(context.Background(), idxc)).Should(Succeed())

			Eventually(func() string {
				fetched := &enterpriseApi.IndexerCluster{}
				if err := k8sClient.Get(context.Background(), types.NamespacedName{
					Name: idxc.Name, Namespace: namespace,
				}, fetched); err != nil {
					return err.Error()
				}

				fetched.Spec.Certs = append(fetched.Spec.Certs, enterpriseApi.CertSpec{
					SecretRef: corev1.LocalObjectReference{Name: "server-b"},
					Role:      enterpriseApi.CertRoleServer,
				})
				err := k8sClient.Update(context.Background(), fetched)
				if err == nil {
					return ""
				}
				return err.Error()
			}, timeout, interval).Should(ContainSubstring("at most one entry per role is allowed"))

			Expect(k8sClient.Delete(context.Background(), idxc)).Should(Succeed())
			Expect(k8sClient.Delete(context.Background(), nsSpecs)).Should(Succeed())
		})
	})
})

func GetIngestorCluster(name string, namespace string) (*enterpriseApi.IngestorCluster, error) {
	By("Expecting IngestorCluster custom resource to be retrieved successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ic := &enterpriseApi.IngestorCluster{}

	err := k8sClient.Get(context.Background(), key, ic)
	if err != nil {
		return nil, err
	}

	return ic, err
}

func CreateIngestorCluster(name string, namespace string, annotations map[string]string, status enterpriseApi.Phase, os *enterpriseApi.ObjectStorage, queue *enterpriseApi.Queue) *enterpriseApi.IngestorCluster {
	By("Expecting IngestorCluster custom resource to be created successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	ingSpec := &enterpriseApi.IngestorCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: enterpriseApi.IngestorClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "IfNotPresent",
				},
			},
			Replicas: 3,
			QueueRef: corev1.ObjectReference{
				Name:      queue.Name,
				Namespace: queue.Namespace,
			},
			ObjectStorageRef: corev1.ObjectReference{
				Name:      os.Name,
				Namespace: os.Namespace,
			},
		},
	}

	Expect(k8sClient.Create(context.Background(), ingSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	ic := &enterpriseApi.IngestorCluster{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ic)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ic.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), ic)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return ic
}

func UpdateIngestorCluster(instance *enterpriseApi.IngestorCluster, status enterpriseApi.Phase, os *enterpriseApi.ObjectStorage, queue *enterpriseApi.Queue) *enterpriseApi.IngestorCluster {
	By("Expecting IngestorCluster custom resource to be updated successfully")

	key := types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}

	icSpec := testutils.NewIngestorCluster(instance.Name, instance.Namespace, "image", os, queue)
	icSpec.ResourceVersion = instance.ResourceVersion
	Expect(k8sClient.Update(context.Background(), icSpec)).Should(Succeed())
	time.Sleep(2 * time.Second)

	ic := &enterpriseApi.IngestorCluster{}
	Eventually(func() bool {
		_ = k8sClient.Get(context.Background(), key, ic)
		if status != "" {
			fmt.Printf("status is set to %v", status)
			ic.Status.Phase = status
			Expect(k8sClient.Status().Update(context.Background(), ic)).Should(Succeed())
			time.Sleep(2 * time.Second)
		}
		return true
	}, timeout, interval).Should(BeTrue())

	return ic
}

func DeleteIngestorCluster(name string, namespace string) {
	By("Expecting IngestorCluster custom resource to be deleted successfully")

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	Eventually(func() error {
		ic := &enterpriseApi.IngestorCluster{}
		_ = k8sClient.Get(context.Background(), key, ic)
		err := k8sClient.Delete(context.Background(), ic)
		return err
	}, timeout, interval).Should(Succeed())
}
