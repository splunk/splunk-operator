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
	"slices"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgreconcile "github.com/splunk/splunk-operator/pkg/postgresql/shared/reconcile"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BlueGreenCleanup retires the non-authoritative environment after child-owned cleanup.
type BlueGreenCleanup struct {
	client   client.Client
	reader   client.Reader
	key      types.NamespacedName
	ownerUID types.UID
}

func NewBlueGreenCleanup(c client.Client, reader client.Reader, key types.NamespacedName, parent *platformv1alpha1.PostgresCluster) (*BlueGreenCleanup, error) {
	if c == nil || reader == nil || parent == nil || parent.UID == "" || key.Name != parent.Name || key.Namespace != parent.Namespace {
		return nil, fmt.Errorf("cleanup requires clients and a persisted namespaced parent identity")
	}
	return &BlueGreenCleanup{client: c, reader: reader, key: key, ownerUID: parent.UID}, nil
}

// retireResources advances one external mutation per reconciliation.
func (c *BlueGreenCleanup) retireResources(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, active *cnpgv1.Cluster, verify func(context.Context) error) (bool, error) {
	protected, err := publishedCleanupSecrets(ctx, c.reader, parent)
	if err != nil {
		return false, err
	}
	refs := slices.Clone(bg.Cleanup.RetiredResources)
	priority := func(kind string) int {
		switch kind {
		case "Publication", "Subscription":
			return 0
		case "ScheduledBackup", "Pooler":
			return 1
		case "Cluster":
			return 2
		case "Certificate":
			return 3
		default:
			return 4
		}
	}
	slices.SortStableFunc(refs, func(a, b corev1.ObjectReference) int { return priority(a.Kind) - priority(b.Kind) })
	for _, ref := range refs {
		object, err := loadExactRetirementRoot(ctx, c.reader, parent.Namespace, ref)
		if err != nil {
			return false, err
		}
		if object == nil {
			continue
		}
		if err := validateCleanupRoot(parent, bg, active, protected, object); err != nil {
			return false, err
		}
		if err := verify(ctx); err != nil {
			return false, err
		}
		if object.GetDeletionTimestamp() != nil {
			switch value := object.(type) {
			case *cnpgv1.Subscription:
				return false, quiesceRetainedSubscription(ctx, c.client, value)
			case *cnpgv1.Publication:
				if value.Spec.ReclaimPolicy != cnpgv1.PublicationReclaimRetain {
					return false, fmt.Errorf("publication writer quiescence requires deleting Retain intent")
				}
				return false, wakeDeletingDeclaration(ctx, c.client, value, &value.Spec.Parameters, "publish_via_partition_root", value.Status.ObservedGeneration, cnpgutils.PublicationFinalizerName)
			default:
				return false, nil
			}
		}
		if ref.Kind == "Cluster" {
			ready, err := c.preserveRetirementSecrets(ctx, parent, active, protected, ref, verify)
			if err != nil || !ready {
				return false, err
			}
			ready, err = c.preserveRetirementBackups(ctx, ref, verify)
			if err != nil || !ready {
				return false, err
			}
			ready, err = preserveClusterSnapshots(ctx, c.client, c.reader, ref, verify)
			if err != nil || !ready {
				return false, err
			}
		}
		original := object.DeepCopyObject().(client.Object)
		retained := retainReplicationDeclaration(object)
		if err := verify(ctx); err != nil {
			return false, err
		}
		if retained {
			return false, pgreconcile.PatchLocked(ctx, c.client, original, object)
		}
		return false, pgreconcile.DeleteExact(ctx, c.client, object, client.PropagationPolicy(metav1.DeletePropagationForeground))
	}
	return c.retirementDependentsAbsent(ctx, parent.Namespace, refs)
}

func retainReplicationDeclaration(object client.Object) bool {
	switch value := object.(type) {
	case *cnpgv1.Publication:
		changed := value.Spec.ReclaimPolicy != cnpgv1.PublicationReclaimRetain
		value.Spec.ReclaimPolicy = cnpgv1.PublicationReclaimRetain
		return changed
	case *cnpgv1.Subscription:
		changed := value.Spec.ReclaimPolicy != cnpgv1.SubscriptionReclaimRetain
		value.Spec.ReclaimPolicy = cnpgv1.SubscriptionReclaimRetain
		return changed
	default:
		return false
	}
}

func validateCleanupRoot(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, active *cnpgv1.Cluster, protected map[string]bool, object client.Object) error {
	if environment, ok := object.(*cnpgv1.Cluster); ok {
		ref := bg.Cleanup.EnvironmentRef
		if ref != nil && environment.UID == ref.UID && environment.Name == ref.Name && environment.UID != active.UID && metav1.IsControlledBy(object, parent) {
			return nil
		}
	} else if disposableCleanupRoot(parent, bg, active, bg.Cleanup.EnvironmentRef, protected, object) {
		return nil
	}
	if secret, ok := object.(*corev1.Secret); ok && secret.Type == corev1.SecretTypeTLS && isParentServerTLS(secret.Labels, parent) && !protectedCleanupSecret(parent, active, secret.Name, protected) {
		owner := metav1.GetControllerOf(secret)
		if owner == nil || owner.UID == parent.UID || slices.ContainsFunc(bg.Cleanup.RetiredResources, func(ref corev1.ObjectReference) bool { return ref.Kind == "Certificate" && ref.UID == owner.UID }) {
			return nil
		}
	}
	return fmt.Errorf("retirement root %s is protected or has conflicting ownership", object.GetName())
}

// preserveRetirementBackups prevents environment GC from deleting recovery metadata.
func (c *BlueGreenCleanup) preserveRetirementBackups(ctx context.Context, environment corev1.ObjectReference, verify func(context.Context) error) (bool, error) {
	var backups cnpgv1.BackupList
	if err := c.reader.List(ctx, &backups, client.InNamespace(environment.Namespace)); err != nil {
		return false, err
	}
	for i := range backups.Items {
		backup := &backups.Items[i]
		if backup.Spec.Cluster.Name != environment.Name {
			continue
		}
		if backup.Status.Phase != cnpgv1.BackupPhaseCompleted && backup.Status.Phase != cnpgv1.BackupPhaseFailed && backup.Status.Phase != cnpgv1.BackupPhaseDefinitionInvalid {
			return false, nil
		}
		original := backup.DeepCopy()
		backup.OwnerReferences = slices.DeleteFunc(backup.OwnerReferences, func(owner metav1.OwnerReference) bool {
			return retirementOwner(owner, environment)
		})
		if len(backup.OwnerReferences) == len(original.OwnerReferences) {
			continue
		}
		if err := verify(ctx); err != nil {
			return false, err
		}
		return false, pgreconcile.PatchLocked(ctx, c.client, original, backup)
	}
	return true, nil
}

func (c *BlueGreenCleanup) preserveRetirementSecrets(ctx context.Context, parent *platformv1alpha1.PostgresCluster, active *cnpgv1.Cluster, protected map[string]bool, environment corev1.ObjectReference, verify func(context.Context) error) (bool, error) {
	var secrets corev1.SecretList
	if err := c.reader.List(ctx, &secrets, client.InNamespace(parent.Namespace)); err != nil {
		return false, err
	}
	for i := range secrets.Items {
		secret := &secrets.Items[i]
		if !protectedCleanupSecret(parent, active, secret.Name, protected) {
			continue
		}
		original := secret.DeepCopy()
		changed := false
		alreadyOwned := slices.ContainsFunc(secret.OwnerReferences, func(owner metav1.OwnerReference) bool { return owner.UID == active.UID })
		if alreadyOwned {
			secret.OwnerReferences = slices.DeleteFunc(secret.OwnerReferences, func(owner metav1.OwnerReference) bool {
				if retirementOwner(owner, environment) {
					changed = true
					return true
				}
				return false
			})
		}
		for j := range secret.OwnerReferences {
			owner := &secret.OwnerReferences[j]
			if retirementOwner(*owner, environment) {
				owner.Name, owner.UID = active.Name, active.UID
				changed = true
			}
		}
		if !changed {
			continue
		}
		if secret.DeletionTimestamp != nil {
			return false, fmt.Errorf("required Secret %s is already deleting", secret.Name)
		}
		if err := verify(ctx); err != nil {
			return false, err
		}
		return false, pgreconcile.PatchLocked(ctx, c.client, original, secret)
	}
	return true, nil
}

// Foreground deletion keeps the root while GC removes its native dependents.
func (c *BlueGreenCleanup) retirementDependentsAbsent(ctx context.Context, namespace string, roots []corev1.ObjectReference) (bool, error) {
	owners := map[types.UID]bool{}
	for _, root := range roots {
		owners[root.UID] = true
	}
	objects, err := namespaceObjects(ctx, c.reader, namespace, &corev1.PodList{}, &corev1.PersistentVolumeClaimList{}, &corev1.ServiceList{}, &appsv1.DeploymentList{})
	if err != nil {
		return false, err
	}
	for _, object := range objects {
		for _, owner := range object.GetOwnerReferences() {
			if owners[owner.UID] {
				return false, nil
			}
		}
	}
	return true, nil
}

func loadExactRetirementRoot(ctx context.Context, reader client.Reader, namespace string, ref corev1.ObjectReference) (client.Object, error) {
	if ref.Namespace != namespace {
		return nil, fmt.Errorf("retirement root is outside its parent namespace")
	}
	object, err := cleanupRootObject(ref)
	if err != nil {
		return nil, err
	}
	if exists, err := pgreconcile.ReadExactObject(ctx, reader, ref, object); err != nil || !exists {
		return nil, err
	}
	return object, nil
}
