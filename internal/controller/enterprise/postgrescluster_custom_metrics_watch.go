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
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	indexClusterCustomQueryConfigMaps  = "spec.monitoring.customQueriesConfigMap.name"
	indexDatabaseCustomQueryConfigMaps = "status.customMetricsPublication.contributions.customQueriesConfigMap.name"
)

func extractClusterCustomQueryConfigMapNames(obj client.Object) []string {
	pc, ok := obj.(*enterprisev4.PostgresCluster)
	if !ok || pc.Spec.Monitoring == nil {
		return nil
	}
	return distinctConfigMapNames(pc.Spec.Monitoring.CustomQueriesConfigMap)
}

func extractDatabaseCustomQueryConfigMapNames(obj client.Object) []string {
	db, ok := obj.(*enterprisev4.PostgresDatabase)
	if !ok || db.Status.CustomMetricsPublication == nil ||
		db.Status.CustomMetricsPublication.ObservedGeneration != db.Generation {
		return nil
	}
	var refs []corev1.ConfigMapKeySelector
	for i := range db.Status.CustomMetricsPublication.Contributions {
		contribution := &db.Status.CustomMetricsPublication.Contributions[i]
		if contribution.Exists {
			refs = append(refs, contribution.CustomQueriesConfigMap...)
		}
	}
	return distinctConfigMapNames(refs)
}

func distinctConfigMapNames(refs []corev1.ConfigMapKeySelector) []string {
	seen := map[string]struct{}{}
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.Name == "" {
			continue
		}
		if _, duplicate := seen[ref.Name]; duplicate {
			continue
		}
		seen[ref.Name] = struct{}{}
		names = append(names, ref.Name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// Owned ConfigMaps are covered by Owns; this predicate handles source data and
// ownership drift.
func customMetricsConfigMapPredicate() predicate.Predicate {
	skipOwned := func(obj client.Object) bool {
		owner := metav1.GetControllerOf(obj)
		if owner == nil ||
			owner.APIVersion != enterprisev4.GroupVersion.String() ||
			owner.Kind != "PostgresCluster" {
			return false
		}
		candidate, generated := generatedMetricsClusterName(obj.GetName())
		return !generated || candidate == owner.Name
	}
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool { return !skipOwned(e.Object) },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldCM, oldOK := e.ObjectOld.(*corev1.ConfigMap)
			newCM, newOK := e.ObjectNew.(*corev1.ConfigMap)
			if !oldOK || !newOK {
				return false
			}
			if !equality.Semantic.DeepEqual(metav1.GetControllerOf(oldCM), metav1.GetControllerOf(newCM)) {
				return true
			}
			if skipOwned(newCM) {
				return false
			}
			return !equality.Semantic.DeepEqual(oldCM.Data, newCM.Data) ||
				!equality.Semantic.DeepEqual(oldCM.BinaryData, newCM.BinaryData) ||
				oldCM.Annotations[cnpginfra.MonitoringCMHashAnnotation] !=
					newCM.Annotations[cnpginfra.MonitoringCMHashAnnotation]
		},
		DeleteFunc:  func(e event.DeleteEvent) bool { return !skipOwned(e.Object) },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// Indexed reads are preferred; namespace-list fallbacks preserve fan-out when
// an index or cache lookup fails.
func (r *PostgresClusterReconciler) enqueueClustersForCustomMetricsConfigMap(ctx context.Context, obj client.Object) []reconcile.Request {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil
	}
	logger := logging.FromContext(ctx).With(
		"controller", "PostgresCluster",
		"func", "enqueueClustersForCustomMetricsConfigMap",
		"configMap", cm.Name,
		"namespace", cm.Namespace,
	)

	seen := map[types.NamespacedName]struct{}{}
	var requests []reconcile.Request
	enqueue := func(name string) {
		key := types.NamespacedName{Namespace: cm.Namespace, Name: name}
		if _, duplicate := seen[key]; duplicate {
			return
		}
		seen[key] = struct{}{}
		requests = append(requests, reconcile.Request{NamespacedName: key})
	}
	_, isGeneratedName := generatedMetricsClusterName(cm.Name)
	if owner := metav1.GetControllerOf(cm); owner != nil && !isGeneratedName {
		// For non-generated CM names, seed from the owner so clusters that own
		// CMs without the generated-name convention are still enqueued.
		if owner.APIVersion == enterprisev4.GroupVersion.String() && owner.Kind == "PostgresCluster" {
			enqueue(owner.Name)
		}
		if owner.APIVersion == cnpgv1.SchemeGroupVersion.String() && owner.Kind == "Cluster" {
			enqueue(owner.Name)
		}
	}
	if isGeneratedName {
		enqueue(strings.TrimSuffix(cm.Name, "-metrics"))
	}
	if r.Client == nil {
		return requests
	}

	var clusters enterprisev4.PostgresClusterList
	if err := r.Client.List(ctx, &clusters,
		client.InNamespace(cm.Namespace),
		client.MatchingFields{indexClusterCustomQueryConfigMaps: cm.Name},
	); err != nil {
		logger.ErrorContext(ctx, "failed to list PostgresClusters for custom-metrics ConfigMap", "error", err)
		clusters = enterprisev4.PostgresClusterList{}
		if fallbackErr := r.Client.List(ctx, &clusters, client.InNamespace(cm.Namespace)); fallbackErr != nil {
			logger.ErrorContext(ctx, "failed fallback list of PostgresClusters for custom-metrics ConfigMap", "error", fallbackErr)
		} else {
			filtered := clusters.Items[:0]
			for i := range clusters.Items {
				if containsString(extractClusterCustomQueryConfigMapNames(&clusters.Items[i]), cm.Name) {
					filtered = append(filtered, clusters.Items[i])
				}
			}
			clusters.Items = filtered
		}
	}
	for i := range clusters.Items {
		enqueue(clusters.Items[i].Name)
	}

	var databases enterprisev4.PostgresDatabaseList
	if err := r.Client.List(ctx, &databases,
		client.InNamespace(cm.Namespace),
		client.MatchingFields{indexDatabaseCustomQueryConfigMaps: cm.Name},
	); err != nil {
		logger.ErrorContext(ctx, "failed to list PostgresDatabases for custom-metrics ConfigMap", "error", err)
		databases = enterprisev4.PostgresDatabaseList{}
		if fallbackErr := r.Client.List(ctx, &databases, client.InNamespace(cm.Namespace)); fallbackErr != nil {
			logger.ErrorContext(ctx, "failed fallback list of PostgresDatabases for custom-metrics ConfigMap", "error", fallbackErr)
		} else {
			filtered := databases.Items[:0]
			for i := range databases.Items {
				if containsString(extractDatabaseCustomQueryConfigMapNames(&databases.Items[i]), cm.Name) {
					filtered = append(filtered, databases.Items[i])
				}
			}
			databases.Items = filtered
		}
	}
	for i := range databases.Items {
		if ref := databases.Items[i].Spec.ClusterRef.Name; ref != "" {
			enqueue(ref)
		}
	}
	return requests
}

func generatedMetricsClusterName(configMapName string) (string, bool) {
	const suffix = "-metrics"
	if !strings.HasSuffix(configMapName, suffix) {
		return "", false
	}
	name := strings.TrimSuffix(configMapName, suffix)
	return name, name != ""
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
