// Copyright (c) 2018-2024 Splunk Inc. All rights reserved.
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

package appframework

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	appframeworkv1 "github.com/splunk/splunk-operator/api/appframework/v1"
)

var _ = Describe("AppFrameworkRepository Controller", func() {
	var (
		ctx        context.Context
		reconciler *AppFrameworkRepositoryReconciler
		k8sClient  client.Client
		scheme     *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(appframeworkv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		k8sClient = fake.NewClientBuilder().WithScheme(scheme).Build()

		reconciler = &AppFrameworkRepositoryReconciler{
			Client:        k8sClient,
			Scheme:        scheme,
			Recorder:      record.NewFakeRecorder(100),
			JobManager:    NewJobManager(k8sClient, scheme, "test-worker:latest"),
			ClientManager: NewRemoteClientManager(k8sClient),
		}
	})

	Describe("Repository Validation", func() {
		It("should validate S3 repository configuration", func() {
			repository := &appframeworkv1.AppFrameworkRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-s3-repo",
					Namespace: "default",
				},
				Spec: appframeworkv1.AppFrameworkRepositorySpec{
					StorageType: "s3",
					Provider:    "aws",
					Endpoint:    "https://s3.amazonaws.com",
					Region:      "us-west-2",
					Path:        "test-bucket/apps/",
				},
			}

			err := reconciler.validateRepository(ctx, repository)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reject invalid storage type and provider combination", func() {
			repository := &appframeworkv1.AppFrameworkRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-invalid-repo",
					Namespace: "default",
				},
				Spec: appframeworkv1.AppFrameworkRepositorySpec{
					StorageType: "s3",
					Provider:    "azure", // Invalid combination
					Endpoint:    "https://s3.amazonaws.com",
					Path:        "test-bucket/apps/",
				},
			}

			err := reconciler.validateRepository(ctx, repository)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid provider azure for storage type s3"))
		})

		It("should validate credentials secret exists", func() {
			// Create a secret first
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "s3-creds",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"s3_access_key": []byte("test-key"),
					"s3_secret_key": []byte("test-secret"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			repository := &appframeworkv1.AppFrameworkRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-repo-with-secret",
					Namespace: "default",
				},
				Spec: appframeworkv1.AppFrameworkRepositorySpec{
					StorageType: "s3",
					Provider:    "aws",
					Endpoint:    "https://s3.amazonaws.com",
					SecretRef:   "s3-creds",
					Path:        "test-bucket/apps/",
				},
			}

			// This will fail at connectivity test since we don't have real S3
			// but should pass credential validation
			err := reconciler.validateRepository(ctx, repository)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("connectivity test failed"))
		})
	})

	Describe("Repository Reconciliation", func() {
		It("should initialize repository status", func() {
			repository := &appframeworkv1.AppFrameworkRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-repo",
					Namespace: "default",
				},
				Spec: appframeworkv1.AppFrameworkRepositorySpec{
					StorageType: "s3",
					Provider:    "aws",
					Endpoint:    "https://s3.amazonaws.com",
					Path:        "test-bucket/apps/",
				},
			}

			Expect(k8sClient.Create(ctx, repository)).To(Succeed())

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      repository.Name,
					Namespace: repository.Namespace,
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			// Check that status was initialized
			updatedRepo := &appframeworkv1.AppFrameworkRepository{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, updatedRepo)).To(Succeed())
			Expect(updatedRepo.Status.Phase).To(Equal("Pending"))
		})

		It("should add finalizer on first reconcile", func() {
			repository := &appframeworkv1.AppFrameworkRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-repo-finalizer",
					Namespace: "default",
				},
				Spec: appframeworkv1.AppFrameworkRepositorySpec{
					StorageType: "s3",
					Provider:    "aws",
					Endpoint:    "https://s3.amazonaws.com",
					Path:        "test-bucket/apps/",
				},
			}

			Expect(k8sClient.Create(ctx, repository)).To(Succeed())

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      repository.Name,
					Namespace: repository.Namespace,
				},
			}

			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())

			// Check that finalizer was added
			updatedRepo := &appframeworkv1.AppFrameworkRepository{}
			Expect(k8sClient.Get(ctx, req.NamespacedName, updatedRepo)).To(Succeed())
			Expect(updatedRepo.Finalizers).To(ContainElement(AppFrameworkFinalizer))
		})
	})

	Describe("Sync Scheduling", func() {
		It("should determine sync is needed for new repository", func() {
			repository := &appframeworkv1.AppFrameworkRepository{
				Status: appframeworkv1.AppFrameworkRepositoryStatus{
					LastSyncTime: nil, // Never synced
				},
			}

			needed, err := reconciler.shouldSync(ctx, repository)
			Expect(err).ToNot(HaveOccurred())
			Expect(needed).To(BeTrue())
		})

		It("should determine sync is needed after poll interval", func() {
			pastTime := metav1.NewTime(time.Now().Add(-10 * time.Minute))
			pollInterval := metav1.Duration{Duration: 5 * time.Minute}

			repository := &appframeworkv1.AppFrameworkRepository{
				Spec: appframeworkv1.AppFrameworkRepositorySpec{
					PollInterval: &pollInterval,
				},
				Status: appframeworkv1.AppFrameworkRepositoryStatus{
					LastSyncTime: &pastTime,
				},
			}

			needed, err := reconciler.shouldSync(ctx, repository)
			Expect(err).ToNot(HaveOccurred())
			Expect(needed).To(BeTrue())
		})

		It("should determine sync is not needed within poll interval", func() {
			recentTime := metav1.NewTime(time.Now().Add(-2 * time.Minute))
			pollInterval := metav1.Duration{Duration: 5 * time.Minute}

			repository := &appframeworkv1.AppFrameworkRepository{
				Spec: appframeworkv1.AppFrameworkRepositorySpec{
					PollInterval: &pollInterval,
				},
				Status: appframeworkv1.AppFrameworkRepositoryStatus{
					LastSyncTime: &recentTime,
				},
			}

			needed, err := reconciler.shouldSync(ctx, repository)
			Expect(err).ToNot(HaveOccurred())
			Expect(needed).To(BeFalse())
		})
	})

	Describe("Status Updates", func() {
		It("should update repository status and conditions", func() {
			repository := &appframeworkv1.AppFrameworkRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-status-repo",
					Namespace: "default",
				},
			}

			Expect(k8sClient.Create(ctx, repository)).To(Succeed())

			reconciler.updateRepositoryStatus(ctx, repository, "Ready", "Repository is ready")

			// Verify status update
			updatedRepo := &appframeworkv1.AppFrameworkRepository{}
			key := types.NamespacedName{Name: repository.Name, Namespace: repository.Namespace}
			Expect(k8sClient.Get(ctx, key, updatedRepo)).To(Succeed())

			Expect(updatedRepo.Status.Phase).To(Equal("Ready"))
			Expect(updatedRepo.Status.Conditions).To(HaveLen(1))
			Expect(updatedRepo.Status.Conditions[0].Type).To(Equal("Ready"))
			Expect(updatedRepo.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
		})
	})
})

func TestAppFrameworkRepositoryController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AppFrameworkRepository Controller Suite")
}
