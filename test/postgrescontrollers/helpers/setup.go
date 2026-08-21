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

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateReadyPostgresDatabase creates one logical database and waits until the
// operator publishes its managed role credentials.
func CreateReadyPostgresDatabase(
	ctx context.Context,
	kubeClient client.Client,
	namespace, name, clusterName, databaseName string,
) *enterprisev4.PostgresDatabase {
	GinkgoHelper()
	database := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: clusterName},
			Databases: []enterprisev4.DatabaseDefinition{{
				Name:           databaseName,
				DeletionPolicy: "Delete",
			}},
		},
	}
	Expect(kubeClient.Create(ctx, database)).To(Succeed())

	key := types.NamespacedName{Name: name, Namespace: namespace}
	var ready *enterprisev4.PostgresDatabase
	Eventually(func(g Gomega) {
		current := &enterprisev4.PostgresDatabase{}
		g.Expect(kubeClient.Get(ctx, key, current)).To(Succeed())
		if current.Status.Phase != nil && *current.Status.Phase == "Failed" {
			StopTrying(postgresDatabaseFailure(current)).Now()
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

func postgresDatabaseFailure(database *enterprisev4.PostgresDatabase) string {
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
