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
	"fmt"
	"sort"

	"github.com/onsi/ginkgo/v2"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// GeneratedMetricEntries parses the generated CNPG queries payload.
func GeneratedMetricEntries(configMap *corev1.ConfigMap) (map[string]cnpginfra.MetricEntry, error) {
	raw, found := configMap.Data[cnpginfra.MonitoringCMKey]
	if !found {
		return nil, fmt.Errorf("generated ConfigMap %s/%s has no %q key",
			configMap.Namespace, configMap.Name, cnpginfra.MonitoringCMKey)
	}
	entries := map[string]cnpginfra.MetricEntry{}
	if err := yaml.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("parsing generated ConfigMap %s/%s: %w", configMap.Namespace, configMap.Name, err)
	}
	return entries, nil
}

// CustomMetricsFailureDump collects custom-metrics state after a failed spec.
func CustomMetricsFailureDump(
	kubeClient client.Client,
	metricsClient kubernetes.Interface,
	clusterKey, databaseKey types.NamespacedName,
) func(ginkgo.SpecContext) {
	return func(ctx ginkgo.SpecContext) {
		cluster := &enterprisev4.PostgresCluster{}
		if err := kubeClient.Get(ctx, clusterKey, cluster); err != nil {
			fmt.Fprintf(ginkgo.GinkgoWriter, "custom metrics diagnostics: getting PostgresCluster: %v\n", err)
		} else {
			condition := meta.FindStatusCondition(cluster.Status.Conditions, "CustomMetricsReady")
			fmt.Fprintf(ginkgo.GinkgoWriter, "PostgresCluster %s phase=%v generation=%d observedGeneration=%v primary=%v customMetricsCondition=%+v\n",
				cluster.Name, cluster.Status.Phase, cluster.Generation, cluster.Status.ObservedGeneration,
				cluster.Status.CurrentPrimary, condition)
			if cluster.Status.CustomMetricsStatus != nil {
				for _, acknowledgement := range cluster.Status.CustomMetricsStatus.DatabaseContributions {
					fmt.Fprintf(ginkgo.GinkgoWriter, "  acknowledgement owner=%s uid=%s database=%s desired=%s applied=%s status=%s reason=%s\n",
						acknowledgement.PostgresDatabaseName, acknowledgement.PostgresDatabaseUID,
						acknowledgement.DatabaseName, acknowledgement.DesiredRevision,
						acknowledgement.AppliedRevision, acknowledgement.Status, acknowledgement.Reason)
				}
			}
		}

		database := &enterprisev4.PostgresDatabase{}
		if err := kubeClient.Get(ctx, databaseKey, database); err != nil {
			fmt.Fprintf(ginkgo.GinkgoWriter, "custom metrics diagnostics: getting PostgresDatabase: %v\n", err)
		} else {
			condition := meta.FindStatusCondition(database.Status.Conditions, "CustomMetricsReady")
			fmt.Fprintf(ginkgo.GinkgoWriter, "PostgresDatabase %s uid=%s phase=%v generation=%d observedGeneration=%v customMetricsCondition=%+v\n",
				database.Name, database.UID, database.Status.Phase, database.Generation,
				database.Status.ObservedGeneration, condition)
			if database.Status.CustomMetricsPublication != nil {
				for _, contribution := range database.Status.CustomMetricsPublication.Contributions {
					fmt.Fprintf(ginkgo.GinkgoWriter, "  contribution database=%s revision=%s exists=%t\n",
						contribution.DatabaseName, contribution.Revision, contribution.Exists)
				}
			}
		}

		generated := &corev1.ConfigMap{}
		generatedKey := types.NamespacedName{Name: clusterKey.Name + "-metrics", Namespace: clusterKey.Namespace}
		if err := kubeClient.Get(ctx, generatedKey, generated); err != nil {
			fmt.Fprintf(ginkgo.GinkgoWriter, "custom metrics diagnostics: getting generated ConfigMap: %v\n", err)
		} else {
			keys := []string{}
			if entries, err := GeneratedMetricEntries(generated); err == nil {
				for key := range entries {
					keys = append(keys, key)
				}
				sort.Strings(keys)
			}
			owner := generated.GetOwnerReferences()
			fmt.Fprintf(ginkgo.GinkgoWriter, "generated ConfigMap %s ownerReferences=%v resourceVersion=%s hash=%s queryKeys=%v\n",
				generated.Name, owner, generated.ResourceVersion,
				generated.Annotations[cnpginfra.MonitoringCMHashAnnotation], keys)
		}

		cnpg := &cnpgv1.Cluster{}
		if err := kubeClient.Get(ctx, clusterKey, cnpg); err != nil {
			fmt.Fprintf(ginkgo.GinkgoWriter, "custom metrics diagnostics: getting CNPG Cluster: %v\n", err)
		} else {
			var selectors any = "disabled"
			if cnpg.Spec.Monitoring != nil {
				selectors = cnpg.Spec.Monitoring.CustomQueriesConfigMap
			}
			fmt.Fprintf(ginkgo.GinkgoWriter, "CNPG Cluster selectors=%v observedMetricConfigMaps=%v\n",
				selectors, cnpg.Status.ConfigMapResourceVersion.Metrics)
		}

		if metricsClient != nil {
			families, err := ScrapePostgresMetrics(ctx, kubeClient, metricsClient, clusterKey)
			if err != nil {
				fmt.Fprintf(ginkgo.GinkgoWriter, "custom metrics diagnostics: scraping metrics: %v\n", err)
			} else {
				fmt.Fprintf(ginkgo.GinkgoWriter, "managed metric families=%v\n", managedMetricFamilyNames(families))
			}
		}
	}
}
