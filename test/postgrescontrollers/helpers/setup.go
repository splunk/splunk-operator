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
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// NewDirectPostgresClient creates a client with the API types used by the
// PostgreSQL E2E scenarios.
func NewDirectPostgresClient() (client.Client, error) {
	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	if err := kubescheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := cnpgv1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	return client.New(restConfig, client.Options{Scheme: scheme})
}

// WaitForReadyPostgresCluster waits for a healthy primary and stops early when
// the operator reports a terminal cluster failure.
func WaitForReadyPostgresCluster(
	ctx context.Context,
	kubeClient client.Client,
	key types.NamespacedName,
) *platformv1alpha1.PostgresCluster {
	GinkgoHelper()
	var ready *platformv1alpha1.PostgresCluster
	Eventually(func(g Gomega) {
		current := &platformv1alpha1.PostgresCluster{}
		g.Expect(kubeClient.Get(ctx, key, current)).To(Succeed())
		StopIfPostgresClusterFailed(current)
		g.Expect(current.Status.Phase).To(HaveValue(Equal("Ready")))
		g.Expect(current.Status.CurrentPrimary).NotTo(BeNil())
		ready = current.DeepCopy()
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
	return ready
}

// StopIfPostgresClusterFailed ends an Eventually poll with the cluster's
// reported failure details.
func StopIfPostgresClusterFailed(cluster *platformv1alpha1.PostgresCluster) {
	GinkgoHelper()
	if cluster.Status.Phase != nil && *cluster.Status.Phase == "Failed" {
		StopTrying(postgresClusterFailure(cluster)).Now()
	}
}

func postgresClusterFailure(cluster *platformv1alpha1.PostgresCluster) string {
	failures := make([]string, 0, len(cluster.Status.Conditions))
	for _, condition := range cluster.Status.Conditions {
		if condition.Status == metav1.ConditionFalse {
			failures = append(failures, fmt.Sprintf("%s/%s: %s", condition.Type, condition.Reason, condition.Message))
		}
	}
	if len(failures) == 0 {
		failures = append(failures, "no failing condition was reported")
	}
	return fmt.Sprintf("PostgresCluster %s/%s entered Failed: %s", cluster.Namespace, cluster.Name, strings.Join(failures, "; "))
}

// CreateReadyPostgresDatabase creates one logical database and waits until the
// operator publishes its managed role credentials.
func CreateReadyPostgresDatabase(
	ctx context.Context,
	kubeClient client.Client,
	namespace, name, clusterName, databaseName string,
) *platformv1alpha1.PostgresDatabase {
	GinkgoHelper()
	database := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: clusterName},
			Databases: []platformv1alpha1.DatabaseDefinition{{
				Name:           databaseName,
				DeletionPolicy: "Delete",
			}},
		},
	}
	Expect(kubeClient.Create(ctx, database)).To(Succeed())

	key := types.NamespacedName{Name: name, Namespace: namespace}
	var ready *platformv1alpha1.PostgresDatabase
	Eventually(func(g Gomega) {
		current := &platformv1alpha1.PostgresDatabase{}
		g.Expect(kubeClient.Get(ctx, key, current)).To(Succeed())
		if current.Status.Phase != nil && *current.Status.Phase == "Failed" {
			StopTrying(PostgresDatabaseFailure(current)).Now()
		}
		g.Expect(current.Status.Phase).To(HaveValue(Equal("Ready")))
		g.Expect(current.Status.Databases).To(HaveLen(1))
		if len(current.Status.Databases) != 1 {
			return
		}
		g.Expect(current.Status.Databases[0].Name).To(Equal(databaseName))
		g.Expect(current.Status.Databases[0].AdminUserSecretRef).NotTo(BeNil())
		g.Expect(current.Status.Databases[0].RWUserSecretRef).NotTo(BeNil())
		ready = current.DeepCopy()
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
	return ready
}

func PostgresDatabaseFailure(database *platformv1alpha1.PostgresDatabase) string {
	failures := make([]string, 0, len(database.Status.Conditions))
	for _, condition := range database.Status.Conditions {
		if condition.Status == metav1.ConditionFalse {
			failures = append(failures, fmt.Sprintf("%s/%s: %s", condition.Type, condition.Reason, condition.Message))
		}
	}
	if len(failures) == 0 {
		failures = append(failures, "no failing condition was reported")
	}
	return fmt.Sprintf("PostgresDatabase %s/%s entered Failed: %s", database.Namespace, database.Name, strings.Join(failures, "; "))
}
