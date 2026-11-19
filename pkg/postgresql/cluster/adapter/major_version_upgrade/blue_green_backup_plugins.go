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

package majorupgradeadapter

import (
	"context"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// stagedBackupPlugins injects sidecars without starting a second WAL producer.
// Activating an archiver on Pods without its sidecar forces CNPG to recreate
// the primary in place, even when the cluster requests switchover updates.
func stagedBackupPlugins(config *clustercore.MergedConfig, environment string) []cnpgv1.PluginConfiguration {
	_, plugins := clustercore.BackupConfigurationForEnvironment(config, environment)
	for i := range plugins {
		plugins[i].IsWALArchiver = new(false)
	}
	return plugins
}

// ensureAttemptObjectStore shares the ownership and policy check between
// candidate preparation and committed backup handoff. Existing objects are
// never adopted or rewritten.
func ensureAttemptObjectStore(ctx context.Context, c client.Client, owner *platformv1alpha1.PostgresCluster, environment, attempt string, config *platformv1alpha1.CNPGBarmanObjectStoreConfig) (bool, error) {
	object := clustercore.BuildObjectStore(environment+"-object-store", owner.Namespace, config)
	object.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(owner, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))})
	object.SetLabels(map[string]string{blueGreenAttemptLabel: attempt})
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(clustercore.ObjectStoreGVK)
	err := c.Get(ctx, client.ObjectKeyFromObject(object), existing)
	if apierrors.IsNotFound(err) {
		return false, c.Create(ctx, object)
	}
	if err != nil {
		return false, err
	}
	if !metav1.IsControlledBy(existing, owner) || existing.GetLabels()[blueGreenAttemptLabel] != attempt {
		return false, fmt.Errorf("target ObjectStore is not owned by this cluster and attempt")
	}
	if existing.GetDeletionTimestamp() != nil || !equality.Semantic.DeepDerivative(object.Object["spec"], existing.Object["spec"]) {
		return false, fmt.Errorf("target ObjectStore differs from the effective backup policy or is being deleted")
	}
	return true, nil
}
