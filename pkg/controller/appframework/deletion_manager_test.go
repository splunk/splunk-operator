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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	appframeworkv1 "github.com/splunk/splunk-operator/api/appframework/v1"
)

var _ = Describe("DeletionManager", func() {
	var (
		ctx         context.Context
		deletionMgr *DeletionManager
		deployment  *appframeworkv1.AppFrameworkDeployment
		scheme      *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(appframeworkv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		deletionMgr = NewDeletionManager(k8sClient)

		deployment = &appframeworkv1.AppFrameworkDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-deployment",
				Namespace: "default",
			},
			Spec: appframeworkv1.AppFrameworkDeploymentSpec{
				AppName:   "test-app.tgz",
				Operation: "uninstall",
				DeletionPolicy: &appframeworkv1.DeletionPolicy{
					Enabled: &[]bool{true}[0],
					Safeguards: &appframeworkv1.DeletionSafeguards{
						ProtectedApps:         []string{"splunk_app_aws", "enterprise_security"},
						RequireConfirmation:   []string{"critical_*"},
						BulkDeletionThreshold: &[]int32{5}[0],
						BackupBeforeDeletion:  &[]bool{true}[0],
					},
				},
			},
		}
	})

	Describe("Protected Apps Check", func() {
		It("should allow deletion of non-protected apps", func() {
			deployment.Spec.AppName = "custom-app.tgz"
			err := deletionMgr.CheckSafeguards(ctx, deployment)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should block deletion of protected apps", func() {
			deployment.Spec.AppName = "splunk_app_aws"
			err := deletionMgr.CheckSafeguards(ctx, deployment)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is protected and cannot be automatically deleted"))
		})

		It("should handle wildcard protection patterns", func() {
			deployment.Spec.DeletionPolicy.Safeguards.ProtectedApps = []string{"splunk_*"}
			deployment.Spec.AppName = "splunk_custom_app.tgz"
			err := deletionMgr.CheckSafeguards(ctx, deployment)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("matches protected pattern"))
		})
	})

	Describe("Confirmation Requirements", func() {
		It("should require confirmation for apps matching patterns", func() {
			deployment.Spec.AppName = "critical_security_app.tgz"
			err := deletionMgr.CheckSafeguards(ctx, deployment)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("requires manual confirmation"))
		})

		It("should allow deletion when confirmation is provided", func() {
			deployment.Spec.AppName = "critical_security_app.tgz"
			deployment.Annotations = map[string]string{
				"appframework.splunk.com/deletion-confirmed":    "true",
				"appframework.splunk.com/deletion-confirmed-at": time.Now().Format(time.RFC3339),
			}
			err := deletionMgr.CheckSafeguards(ctx, deployment)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reject expired confirmations", func() {
			deployment.Spec.AppName = "critical_security_app.tgz"
			expiredTime := time.Now().Add(-25 * time.Hour) // Older than 24 hours
			deployment.Annotations = map[string]string{
				"appframework.splunk.com/deletion-confirmed":    "true",
				"appframework.splunk.com/deletion-confirmed-at": expiredTime.Format(time.RFC3339),
			}
			err := deletionMgr.CheckSafeguards(ctx, deployment)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("confirmation for app critical_security_app.tgz has expired"))
		})
	})

	Describe("Approval Workflow", func() {
		BeforeEach(func() {
			deployment.Spec.DeletionPolicy.Safeguards.ApprovalWorkflow = &appframeworkv1.ApprovalWorkflow{
				Required:  true,
				Approvers: []string{"admin@company.com", "ops-team"},
				Timeout:   &metav1.Duration{Duration: 24 * time.Hour},
			}
		})

		It("should require approval when workflow is enabled", func() {
			err := deletionMgr.CheckSafeguards(ctx, deployment)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("requires approval for deletion"))
		})

		It("should allow deletion with valid approval", func() {
			deployment.Annotations = map[string]string{
				"appframework.splunk.com/deletion-approved":    "true",
				"appframework.splunk.com/deletion-approved-by": "admin@company.com",
				"appframework.splunk.com/deletion-approved-at": time.Now().Format(time.RFC3339),
			}
			err := deletionMgr.CheckSafeguards(ctx, deployment)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reject approval from unauthorized users", func() {
			deployment.Annotations = map[string]string{
				"appframework.splunk.com/deletion-approved":    "true",
				"appframework.splunk.com/deletion-approved-by": "unauthorized@company.com",
				"appframework.splunk.com/deletion-approved-at": time.Now().Format(time.RFC3339),
			}
			err := deletionMgr.CheckSafeguards(ctx, deployment)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("approved by unauthorized user"))
		})

		It("should reject expired approvals", func() {
			expiredTime := time.Now().Add(-25 * time.Hour)
			deployment.Annotations = map[string]string{
				"appframework.splunk.com/deletion-approved":    "true",
				"appframework.splunk.com/deletion-approved-by": "admin@company.com",
				"appframework.splunk.com/deletion-approved-at": expiredTime.Format(time.RFC3339),
			}
			err := deletionMgr.CheckSafeguards(ctx, deployment)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("approval has expired"))
		})
	})

	Describe("Wildcard Matching", func() {
		It("should match prefix wildcards", func() {
			matched := deletionMgr.wildcardMatch("splunk_app_aws", "splunk_*")
			Expect(matched).To(BeTrue())
		})

		It("should match suffix wildcards", func() {
			matched := deletionMgr.wildcardMatch("my_app.tgz", "*.tgz")
			Expect(matched).To(BeTrue())
		})

		It("should match middle wildcards", func() {
			matched := deletionMgr.wildcardMatch("splunk_custom_app", "*custom*")
			Expect(matched).To(BeTrue())
		})

		It("should match universal wildcard", func() {
			matched := deletionMgr.wildcardMatch("any_app", "*")
			Expect(matched).To(BeTrue())
		})

		It("should not match when pattern doesn't match", func() {
			matched := deletionMgr.wildcardMatch("custom_app", "splunk_*")
			Expect(matched).To(BeFalse())
		})
	})

	Describe("Manual Approval Check", func() {
		It("should return false when no approval is found", func() {
			approved, err := deletionMgr.CheckApproval(ctx, deployment)
			Expect(err).ToNot(HaveOccurred())
			Expect(approved).To(BeFalse())
		})

		It("should return true when approval annotation exists", func() {
			deployment.Annotations = map[string]string{
				"appframework.splunk.com/deletion-approved": "true",
			}
			approved, err := deletionMgr.CheckApproval(ctx, deployment)
			Expect(err).ToNot(HaveOccurred())
			Expect(approved).To(BeTrue())
		})
	})

	Describe("Deletion Conditions Validation", func() {
		BeforeEach(func() {
			deployment.Spec.DeletionConditions = []appframeworkv1.DeletionCondition{
				{
					Type:           "AppNotUsed",
					CheckCommand:   []string{"/bin/check-app-usage.sh"},
					ExpectedResult: "empty",
					Timeout:        &metav1.Duration{Duration: 300 * time.Second},
					FailurePolicy:  "Fail",
				},
			}
		})

		It("should validate deletion conditions", func() {
			err := deletionMgr.ValidateDeletionConditions(ctx, deployment)
			Expect(err).ToNot(HaveOccurred()) // Mock implementation returns true
		})
	})
})

func TestDeletionManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DeletionManager Suite")
}
