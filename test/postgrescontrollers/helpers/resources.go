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
package helpers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PostgresDatabaseChildren identifies the provider and credential resources
// published for one PostgresDatabase status entry.
type PostgresDatabaseChildren struct {
	database  types.NamespacedName
	configMap types.NamespacedName
	secrets   []types.NamespacedName
}

func DatabaseSecretUIDs(
	ctx context.Context,
	kubeClient client.Client,
	database *enterprisev4.PostgresDatabase,
) map[types.UID]struct{} {
	GinkgoHelper()
	result := make(map[types.UID]struct{}, 2)
	for _, ref := range []*corev1.SecretKeySelector{
		database.Status.Databases[0].AdminUserSecretRef,
		database.Status.Databases[0].RWUserSecretRef,
	} {
		secret := &corev1.Secret{}
		Expect(kubeClient.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: database.Namespace}, secret)).To(Succeed())
		result[secret.UID] = struct{}{}
	}
	return result
}

func PostgresDatabaseChildResources(database *enterprisev4.PostgresDatabase) PostgresDatabaseChildren {
	GinkgoHelper()
	Expect(database.Status.Databases).To(HaveLen(1))
	status := database.Status.Databases[0]
	Expect(status.DatabaseRef).NotTo(BeNil())
	Expect(status.ConfigMapRef).NotTo(BeNil())
	Expect(status.AdminUserSecretRef).NotTo(BeNil())
	Expect(status.RWUserSecretRef).NotTo(BeNil())

	return PostgresDatabaseChildren{
		database:  types.NamespacedName{Name: status.DatabaseRef.Name, Namespace: database.Namespace},
		configMap: types.NamespacedName{Name: status.ConfigMapRef.Name, Namespace: database.Namespace},
		secrets: []types.NamespacedName{
			{Name: status.AdminUserSecretRef.Name, Namespace: database.Namespace},
			{Name: status.RWUserSecretRef.Name, Namespace: database.Namespace},
		},
	}
}

func ExpectPostgresDatabaseChildrenPresent(
	ctx context.Context,
	kubeClient client.Client,
	children PostgresDatabaseChildren,
) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		g.Expect(kubeClient.Get(ctx, children.database, &cnpgv1.Database{})).To(Succeed())
		g.Expect(kubeClient.Get(ctx, children.configMap, &corev1.ConfigMap{})).To(Succeed())
		for _, key := range children.secrets {
			g.Expect(kubeClient.Get(ctx, key, &corev1.Secret{})).To(Succeed())
		}
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
}

func ExpectPostgresDatabaseChildrenDeleted(
	ctx context.Context,
	kubeClient client.Client,
	children PostgresDatabaseChildren,
) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		g.Expect(apierrors.IsNotFound(kubeClient.Get(ctx, children.database, &cnpgv1.Database{}))).To(BeTrue())
		g.Expect(apierrors.IsNotFound(kubeClient.Get(ctx, children.configMap, &corev1.ConfigMap{}))).To(BeTrue())
		for _, key := range children.secrets {
			g.Expect(apierrors.IsNotFound(kubeClient.Get(ctx, key, &corev1.Secret{}))).To(BeTrue())
		}
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
}

func PostgresClusterSuperuserSecretUID(
	ctx context.Context,
	kubeClient client.Client,
	cluster *enterprisev4.PostgresCluster,
) types.UID {
	GinkgoHelper()
	Expect(cluster.Status.Resources).NotTo(BeNil())
	Expect(cluster.Status.Resources.SuperUserSecretRef).NotTo(BeNil())

	secret := &corev1.Secret{}
	Expect(kubeClient.Get(ctx, types.NamespacedName{
		Name:      cluster.Status.Resources.SuperUserSecretRef.Name,
		Namespace: cluster.Namespace,
	}, secret)).To(Succeed())
	return secret.UID
}
