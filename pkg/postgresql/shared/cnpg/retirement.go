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
package cnpg

import (
	"context"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
	pgreconcile "github.com/splunk/splunk-operator/pkg/postgresql/shared/reconcile"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObserveRetirementEnvironments verifies exact identities and live authoritative routing.
func ObserveRetirementEnvironments(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster, authority corev1.ObjectReference, retirement *corev1.ObjectReference) (*cnpgv1.Cluster, *cnpgv1.Cluster, error) {
	ref := parent.Status.ProvisionerRef
	if parent.UID == "" || parent.DeletionTimestamp != nil || ref == nil || ref.UID == "" || ref.Name != authority.Name || ref.UID != authority.UID ||
		retirement != nil && (retirement.UID == authority.UID || retirement.Name == authority.Name || retirement.Namespace != parent.Namespace) {
		return nil, nil, fmt.Errorf("retirement requires distinct authoritative and retired identities")
	}
	active, err := ReadOwnedEnvironment(ctx, reader, parent, authority)
	if err != nil {
		return nil, nil, err
	}
	if active == nil || active.DeletionTimestamp != nil {
		return nil, nil, fmt.Errorf("authoritative environment is unavailable during retirement")
	}
	var retired *cnpgv1.Cluster
	if retirement != nil {
		retired, err = ReadOwnedEnvironment(ctx, reader, parent, *retirement)
		if err != nil {
			return nil, nil, err
		}
	}
	if parent.Status.Resources == nil || parent.Status.Resources.ConfigMapRef == nil {
		return nil, nil, fmt.Errorf("canonical routing is unavailable during retirement")
	}
	canonical := &corev1.ConfigMap{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: parent.Status.Resources.ConfigMapRef.Name}, canonical); err != nil {
		return nil, nil, err
	}
	endpoint, err := pgconninfo.ServiceFQDN(active.Status.WriteService, parent.Namespace)
	if err != nil {
		return nil, nil, err
	}
	if !pgendpointprotocol.CanonicalOwnedByParent(parent, canonical) || canonical.Data[pgconninfo.KeyClusterRWEndpoint] != endpoint ||
		retirement != nil && canonical.Annotations[pgconninfo.AnnotationActiveEnvironment] == retirement.Name {
		return nil, nil, fmt.Errorf("canonical routing does not target the authoritative environment")
	}
	return active, retired, nil
}

// ReadOwnedEnvironment returns nil for absence and rejects replacement or foreign Clusters.
func ReadOwnedEnvironment(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster, ref corev1.ObjectReference) (*cnpgv1.Cluster, error) {
	if ref.APIVersion != cnpgv1.SchemeGroupVersion.String() || ref.Kind != "Cluster" || ref.Name == "" || ref.UID == "" || ref.Namespace != parent.Namespace {
		return nil, fmt.Errorf("retirement requires an exact namespaced CNPG Cluster reference")
	}
	cluster := &cnpgv1.Cluster{}
	if exists, err := pgreconcile.ReadExactObject(ctx, reader, ref, cluster); err != nil || !exists {
		return nil, err
	}
	if !metav1.IsControlledBy(cluster, parent) {
		return nil, fmt.Errorf("CNPG Cluster %s has conflicting ownership or identity", ref.Name)
	}
	return cluster, nil
}

// RetainDatabase prevents declaration deletion from reclaiming live SQL.
// The caller must persist changes and read them back before deleting the object.
func RetainDatabase(database *cnpgv1.Database) bool {
	changed := database.Spec.ReclaimPolicy != cnpgv1.DatabaseReclaimRetain
	database.Spec.ReclaimPolicy = cnpgv1.DatabaseReclaimRetain
	return changed
}
