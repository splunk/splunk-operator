/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgtestfixture "github.com/splunk/splunk-operator/internal/testutil/postgres"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("PostgresCluster finalization status authority", Label("postgres"), func() {
	It("separates ordinary cluster edits from controller deletion evidence", func() {
		ctx := context.Background()
		role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{GenerateName: "cluster-editor-", Namespace: "default"}, Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{platformv1alpha1.GroupVersion.Group}, Resources: []string{"postgresclusters"}, Verbs: []string{"create", "get", "update", "patch", "delete"},
		}}}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, role)).To(Succeed()) })
		binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{GenerateName: "cluster-editor-", Namespace: "default"},
			RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name},
			Subjects: []rbacv1.Subject{{Kind: "User", Name: role.Name}},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, binding)).To(Succeed()) })
		actorConfig := rest.CopyConfig(cfg)
		actorConfig.Impersonate = rest.ImpersonationConfig{UserName: role.Name, Groups: []string{"system:authenticated"}}
		actor, err := client.New(actorConfig, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		proof := &platformv1alpha1.PostgresClusterFinalizationStatus{Version: 1, ParentUID: "forged", Policy: "Delete", HistoryHash: "forged", SQLDone: true, SQLBlocked: true,
			Resources: []corev1.ObjectReference{{APIVersion: "v1", Kind: "Secret", Namespace: "default", Name: "unrelated", UID: "unrelated"}},
		}
		parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{GenerateName: "finalization-authority-", Namespace: "default"},
			Spec:   platformv1alpha1.PostgresClusterSpec{Class: "finalization-authority", Images: pgtestfixture.Images()},
			Status: platformv1alpha1.PostgresClusterStatus{Finalization: proof},
		}
		Eventually(func() error { return actor.Create(ctx, parent) }).Should(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, parent)).To(Succeed()) })
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		Expect(parent.Status.Finalization).To(BeNil())
		parent.Status.Finalization = proof
		Expect(actor.Update(ctx, parent)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		Expect(parent.Status.Finalization).To(BeNil())
		parent.Status.Finalization = proof
		Expect(apierrors.IsForbidden(actor.Status().Update(ctx, parent))).To(BeTrue())
		Expect(apierrors.IsForbidden(actor.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "default"}}))).To(BeTrue())
		proof.ParentUID = parent.UID
		parent.Status.Finalization = proof
		Expect(k8sClient.Status().Update(ctx, parent)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		Expect(parent.Status.Finalization).To(Equal(proof))
		copy := parent.DeepCopy()
		copy.Status.Finalization.Resources[0].UID = "changed"
		Expect(parent.Status.Finalization.Resources[0].UID).To(Equal(proof.Resources[0].UID))
	})
})
