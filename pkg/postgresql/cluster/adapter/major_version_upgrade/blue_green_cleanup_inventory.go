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
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// cleanupInventory combines current children, historical participants, and actual declarations.
func cleanupInventory(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, environment *corev1.ObjectReference) ([]platformv1alpha1.BlueGreenRetirementAcknowledgementStatus, error) {
	if environment == nil {
		return nil, nil
	}
	var children platformv1alpha1.PostgresDatabaseList
	if err := reader.List(ctx, &children, client.InNamespace(parent.Namespace)); err != nil {
		return nil, err
	}
	type childIdentity struct {
		name string
		uid  types.UID
	}
	current := map[childIdentity]*platformv1alpha1.PostgresDatabase{}
	owners := databaseOwners{}
	rows := map[[3]string]*platformv1alpha1.BlueGreenRetirementAcknowledgementStatus{}
	add := func(child, uid, database string) *platformv1alpha1.BlueGreenRetirementAcknowledgementStatus {
		owners[types.UID(uid)] = child
		key := [3]string{child, uid, database}
		if rows[key] == nil {
			rows[key] = &platformv1alpha1.BlueGreenRetirementAcknowledgementStatus{PostgresDatabaseName: child, PostgresDatabaseUID: uid, DatabaseName: database}
		}
		return rows[key]
	}
	for i := range children.Items {
		child := &children.Items[i]
		if child.Spec.ClusterRef.Name != parent.Name {
			continue
		}
		current[childIdentity{child.Name, child.UID}] = child
		owners[child.UID] = child.Name
		for _, database := range child.Spec.Databases {
			add(child.Name, string(child.UID), database.Name)
		}
	}
	for _, database := range bg.DatabaseReplication {
		add(database.PostgresDatabaseName, database.PostgresDatabaseUID, database.DatabaseName)
	}
	if bg.Cleanup != nil {
		for _, row := range mvutypes.RetirementRoster(bg.Cleanup) {
			owners[types.UID(row.PostgresDatabaseUID)] = row.PostgresDatabaseName
			copy := row.DeepCopy()
			rows[[3]string{row.PostgresDatabaseName, row.PostgresDatabaseUID, row.DatabaseName}] = copy
		}
	}
	var databases cnpgv1.DatabaseList
	if err := reader.List(ctx, &databases, client.InNamespace(parent.Namespace)); err != nil {
		return nil, err
	}
	for i := range databases.Items {
		database := &databases.Items[i]
		if database.Spec.ClusterRef.Name != environment.Name {
			continue
		}
		owner := metav1.GetControllerOf(database)
		if owner == nil || owner.Kind != "PostgresDatabase" || owner.APIVersion != platformv1alpha1.GroupVersion.String() {
			return nil, fmt.Errorf("retired environment has an unrecognized Database owner: %s", database.Name)
		}
		if !owners.owns(database) {
			return nil, fmt.Errorf("retired Database %s belongs to an unrelated child", database.Name)
		}
		row := add(owner.Name, string(owner.UID), database.Spec.Name)
		resources, err := mergeRetirementRoots(row.Resources, nil, database)
		if err != nil {
			return nil, err
		}
		row.Resources = resources
	}
	var result []platformv1alpha1.BlueGreenRetirementAcknowledgementStatus
	for _, row := range rows {
		child := current[childIdentity{row.PostgresDatabaseName, types.UID(row.PostgresDatabaseUID)}]
		if child != nil {
			row.PostgresDatabaseGeneration = child.Generation
		} else {
			row.PostgresDatabaseGeneration = 0
		}
		result = append(result, *row)
	}
	slices.SortFunc(result, func(a, b platformv1alpha1.BlueGreenRetirementAcknowledgementStatus) int {
		if n := cmp.Compare(a.PostgresDatabaseName, b.PostgresDatabaseName); n != 0 {
			return n
		}
		if n := cmp.Compare(a.PostgresDatabaseUID, b.PostgresDatabaseUID); n != 0 {
			return n
		}
		return cmp.Compare(a.DatabaseName, b.DatabaseName)
	})
	return result, nil
}

// cleanupRoots finds disposable parent-owned roots while protecting promoted resources.
func cleanupRoots(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, active *cnpgv1.Cluster, retirement *corev1.ObjectReference, protected map[string]bool) ([]corev1.ObjectReference, error) {
	var refs []corev1.ObjectReference
	if retirement != nil {
		refs = append(refs, *retirement)
	}
	objects, err := namespaceObjects(ctx, reader, parent.Namespace, &cnpgv1.PoolerList{}, &cnpgv1.ScheduledBackupList{}, &cnpgv1.PublicationList{}, &cnpgv1.SubscriptionList{}, &corev1.SecretList{}, &certv1.CertificateList{})
	if err != nil {
		return nil, err
	}
	var owned []client.Object
	for _, object := range objects {
		if !disposableCleanupRoot(parent, bg, active, retirement, protected, object) {
			continue
		}
		owned = append(owned, object)
		if certificate, ok := object.(*certv1.Certificate); ok {
			secret, err := certificateLeaf(ctx, reader, parent, certificate)
			if err != nil {
				return nil, err
			}
			if secret != nil {
				owned = append(owned, secret)
			}
		}
	}
	previous := []corev1.ObjectReference(nil)
	if bg.Cleanup != nil {
		previous = bg.Cleanup.RetiredResources
	}
	return mergeRetirementRoots(previous, refs, owned...)
}

// disposableCleanupRoot excludes active dependencies and all recovery backups.
func disposableCleanupRoot(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, active *cnpgv1.Cluster, retirement *corev1.ObjectReference, protected map[string]bool, object client.Object) bool {
	if secret, ok := object.(*corev1.Secret); ok && len(secret.OwnerReferences) == 0 {
		return retirement != nil && secret.Namespace == parent.Namespace && secret.Name == retirement.Name+"-server-ca" &&
			bg.AttemptID != "" && secret.Labels[blueGreenAttemptLabel] == bg.AttemptID &&
			secret.Labels["app.kubernetes.io/managed-by"] == "splunk-operator" && secret.Labels[tlsport.ClusterNameLabel] == parent.Name &&
			isParentServerTLS(secret.Labels, parent) && secret.Type == corev1.SecretTypeOpaque &&
			len(secret.Data) == 1 && len(secret.Data["ca.crt"]) > 0 && !protectedCleanupSecret(parent, active, secret.Name, protected)
	}
	if !metav1.IsControlledBy(object, parent) {
		return false
	}
	switch object := object.(type) {
	case *cnpgv1.Pooler:
		return retirement != nil && object.Spec.Cluster.Name == retirement.Name
	case *cnpgv1.ScheduledBackup:
		return retirement != nil && object.Spec.Cluster.Name == retirement.Name
	case *cnpgv1.Publication:
		return attemptOwned(object, parent, bg.AttemptID) && attemptEnvironment(bg, object.Spec.ClusterRef.Name)
	case *cnpgv1.Subscription:
		return attemptOwned(object, parent, bg.AttemptID) && attemptEnvironment(bg, object.Spec.ClusterRef.Name)
	case *corev1.Secret:
		return attemptOwned(object, parent, bg.AttemptID) && !protectedCleanupSecret(parent, active, object.Name, protected)
	case *certv1.Certificate:
		return isParentServerTLS(object.Labels, parent) && !protectedCleanupSecret(parent, active, object.Spec.SecretName, protected)
	}
	return false
}

func attemptOwned(object client.Object, parent *platformv1alpha1.PostgresCluster, attempt string) bool {
	return attempt != "" && object.GetLabels()[blueGreenAttemptLabel] == attempt && metav1.IsControlledBy(object, parent)
}

func attemptEnvironment(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, name string) bool {
	return bg.Blue != nil && name == bg.Blue.Ref.Name || bg.Green != nil && name == bg.Green.Ref.Name
}

func protectedCleanupSecret(parent *platformv1alpha1.PostgresCluster, active *cnpgv1.Cluster, name string, published map[string]bool) bool {
	if active == nil {
		return true
	}
	return published[name] || slices.ContainsFunc(active.Spec.ImagePullSecrets, func(ref cnpgv1.LocalObjectReference) bool { return ref.Name == name }) ||
		active.UsesSecret(name) || name == active.GetServerTLSSecretName() || name == active.GetServerCASecretName() ||
		name == active.GetClientCASecretName() || name == active.GetReplicationSecretName() ||
		parent.Status.Resources != nil && parent.Status.Resources.SuperUserSecretRef != nil && name == parent.Status.Resources.SuperUserSecretRef.Name
}

// publishedCleanupSecrets preserves credentials and trust already exposed to applications.
func publishedCleanupSecrets(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster) (map[string]bool, error) {
	result := map[string]bool{}
	if parent.Status.Resources != nil && parent.Status.Resources.ConfigMapRef != nil {
		canonical := &corev1.ConfigMap{}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: parent.Status.Resources.ConfigMapRef.Name}, canonical); err != nil {
			return nil, err
		}
		if !metav1.IsControlledBy(canonical, parent) {
			return nil, fmt.Errorf("canonical access metadata has a conflicting owner")
		}
		for _, key := range []string{pgconninfo.KeySuperUserSecretRef, pgconninfo.KeyServerCASecretRef} {
			name, _, _ := strings.Cut(canonical.Data[key], "/")
			if name != "" {
				result[name] = true
			}
		}
	}
	var children platformv1alpha1.PostgresDatabaseList
	if err := reader.List(ctx, &children, client.InNamespace(parent.Namespace)); err != nil {
		return nil, err
	}
	for _, child := range children.Items {
		if child.Spec.ClusterRef.Name != parent.Name {
			continue
		}
		for _, database := range child.Status.Databases {
			for _, ref := range []*corev1.SecretKeySelector{database.AdminUserSecretRef, database.RWUserSecretRef} {
				if ref != nil {
					result[ref.Name] = true
				}
			}
		}
		for _, database := range child.Spec.Databases {
			if database.PasswordConfig != nil {
				result[database.PasswordConfig.ExternalAdminSecretRef.Name] = true
				result[database.PasswordConfig.ExternalRWSecretRef.Name] = true
			}
		}
	}
	return result, nil
}

// mergeCleanupReferences keeps exact roots after deletion and rejects name reuse.
func mergeCleanupReferences(previous, observed []corev1.ObjectReference) ([]corev1.ObjectReference, bool, error) {
	result := slices.Clone(previous)
	changed := false
	for _, ref := range observed {
		if ref.UID == "" || ref.Name == "" || ref.Namespace == "" {
			return nil, false, fmt.Errorf("cleanup inventory requires exact resource identities")
		}
		found := false
		for _, old := range result {
			if old.APIVersion == ref.APIVersion && old.Kind == ref.Kind && old.Namespace == ref.Namespace && old.Name == ref.Name {
				if old.UID != ref.UID {
					return nil, false, fmt.Errorf("cleanup resource %s %s was replaced", ref.Kind, ref.Name)
				}
				found = true
				break
			}
		}
		if !found {
			result = append(result, ref)
			changed = true
		}
	}
	slices.SortFunc(result, func(a, b corev1.ObjectReference) int {
		if n := cmp.Compare(a.Kind, b.Kind); n != 0 {
			return n
		}
		if n := cmp.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		return cmp.Compare(a.UID, b.UID)
	})
	return result, changed, nil
}
