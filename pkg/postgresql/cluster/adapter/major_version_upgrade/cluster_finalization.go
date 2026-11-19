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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	pgreconcile "github.com/splunk/splunk-operator/pkg/postgresql/shared/reconcile"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type finalizationInventory = platformv1alpha1.PostgresClusterFinalizationStatus

type ClusterFinalizer struct {
	client   client.Client
	reader   client.Reader
	teardown func(context.Context, *platformv1alpha1.PostgresCluster) (bool, error)
	runtime  func(*platformv1alpha1.PostgresCluster) (*finalizationSQLRuntime, error)
	recorder record.EventRecorder
}

func NewClusterFinalizer(c client.Client, reader client.Reader, recorder record.EventRecorder) *ClusterFinalizer {
	f := &ClusterFinalizer{client: c, reader: reader, recorder: recorder}
	f.teardown = f.teardownSQL
	f.runtime = f.newSQLRuntime
	return f
}

// AdvanceFinalization persists discovery before effects and waits for observed completion.
func (f *ClusterFinalizer) AdvanceFinalization(ctx context.Context, parent *platformv1alpha1.PostgresCluster) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	current := &platformv1alpha1.PostgresCluster{}
	if err := f.reader.Get(ctx, client.ObjectKeyFromObject(parent), current); err != nil {
		return false, err
	}
	if current.UID != parent.UID || current.DeletionTimestamp == nil {
		return false, fmt.Errorf("finalization parent identity changed")
	}
	parent = current
	inventory := finalizationInventory{Version: 1, ParentUID: parent.UID, Policy: ptr.Deref(parent.Spec.ClusterDeletionPolicy, "Retain")}
	recorded := parent.Status.Finalization
	if recorded != nil {
		inventory = *recorded.DeepCopy()
	}
	if inventory.Version != 1 || inventory.ParentUID != parent.UID || inventory.Policy != ptr.Deref(parent.Spec.ClusterDeletionPolicy, "Retain") || inventory.Policy != "Delete" && inventory.Policy != "Retain" {
		return false, fmt.Errorf("finalization policy or identity differs from its durable inventory")
	}
	if recorded == nil {
		class := &platformv1alpha1.PostgresClusterClass{}
		if err := f.reader.Get(ctx, client.ObjectKey{Name: parent.Spec.Class}, class); err != nil && !apierrors.IsNotFound(err) {
			return false, err
		} else if err == nil && class.Spec.TLS != nil && class.Spec.TLS.Certificates != nil {
			inventory.DeleteCA = class.Spec.TLS.Certificates.SecretRetentionPolicy != nil && *class.Spec.TLS.Certificates.SecretRetentionPolicy == platformv1alpha1.PostgresCertificateRetentionPolicyDelete
		}
	}
	refs, children, err := f.discover(ctx, parent, inventory.Resources)
	if err != nil {
		return false, err
	}
	blocked, err := f.unverifiableReplication(ctx, parent, refs)
	if err != nil {
		return false, err
	}
	inventory.SQLBlocked = inventory.SQLBlocked || blocked
	history, err := json.Marshal(parent.Status.PostgresMajorUpgradeStatus)
	if err != nil {
		return false, err
	}
	historyHash := fmt.Sprintf("%x", sha256.Sum256(history))
	if recorded == nil || !equality.Semantic.DeepEqual(refs, inventory.Resources) || inventory.HistoryHash != historyHash || inventory.SQLBlocked != recorded.SQLBlocked {
		if inventory.HistoryHash != historyHash || slices.ContainsFunc(refs, func(ref corev1.ObjectReference) bool {
			return (ref.Kind == "Cluster" || isSQLDeclaration(ref.Kind)) && !slices.ContainsFunc(inventory.Resources, func(old corev1.ObjectReference) bool { return old.UID == ref.UID && old.Kind == ref.Kind })
		}) {
			inventory.SQLDone = false
		}
		if inventory.SQLBlocked {
			inventory.SQLDone = false
		}
		inventory.HistoryHash = historyHash
		inventory.Resources = refs
		for _, ref := range refs {
			if ref.Kind == "Secret" {
				secret := &corev1.Secret{}
				if err := f.reader.Get(ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, secret); err == nil && copiedFinalizationCA(parent, secret) && !slices.Contains(inventory.CASecrets, ref.UID) {
					inventory.CASecrets = append(inventory.CASecrets, ref.UID)
				} else if err != nil && !apierrors.IsNotFound(err) {
					return false, err
				}
			}
		}
		return false, f.save(ctx, parent, inventory)
	}
	if inventory.Policy == "Retain" && inventory.SQLBlocked {
		return false, fmt.Errorf("replication declaration observed for a cleaned attempt; inspect SQL before retaining environments")
	}
	verify := func(ctx context.Context) error {
		fresh := &platformv1alpha1.PostgresCluster{}
		if err := f.reader.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
			return err
		}
		if fresh.UID != parent.UID || fresh.DeletionTimestamp == nil || fresh.Generation != parent.Generation || !equality.Semantic.DeepEqual(fresh.Status.Finalization, recorded) || !equality.Semantic.DeepEqual(fresh.Status.PostgresMajorUpgradeStatus, parent.Status.PostgresMajorUpgradeStatus) {
			return fmt.Errorf("finalization request changed before mutation")
		}
		found, currentChildren, err := f.discover(ctx, fresh, inventory.Resources)
		if err != nil {
			return err
		}
		if !equality.Semantic.DeepEqual(found, inventory.Resources) || !equality.Semantic.DeepEqual(children, currentChildren) {
			return fmt.Errorf("finalization inventory changed before mutation")
		}
		if inventory.Policy == "Retain" && inventory.SQLDone {
			return f.verifyRetainedFences(ctx, fresh)
		}
		return nil
	}
	// Remove declarative SQL intent before proving SQL absence.
	for _, ref := range refs {
		if !isSQLDeclaration(ref.Kind) {
			continue
		}
		object, err := f.load(ctx, parent, ref, inventory.Resources, children)
		if err != nil {
			return false, err
		}
		if object != nil {
			return false, f.removeDeclaration(ctx, object, inventory.Policy == "Retain", verify)
		}
	}
	if !inventory.SQLDone {
		if err := verify(ctx); err != nil {
			return false, err
		}
		bounded, stop := context.WithTimeout(ctx, 5*time.Second)
		done, sqlErr := f.teardown(bounded, parent)
		stop()
		if inventory.Policy == "Retain" && (sqlErr != nil || !done) {
			return false, sqlErr
		}
		// Delete records its bounded attempt and proceeds even when SQL is unreachable.
		inventory.SQLDone = true
		inventory.SQLWarning = sqlErr != nil || !done
		err = f.save(ctx, parent, inventory)
		if err == nil && inventory.SQLWarning && f.recorder != nil {
			f.recorder.Event(parent, corev1.EventTypeWarning, "FinalizationSQLSkipped", "Bounded SQL teardown did not complete; Delete continues with environment and storage removal.")
		}
		return false, err
	}
	for _, ref := range refs {
		if isSQLDeclaration(ref.Kind) {
			continue
		}
		object, err := f.load(ctx, parent, ref, inventory.Resources, children)
		if err != nil {
			return false, err
		}
		if object == nil {
			continue
		}
		if err := verify(ctx); err != nil {
			return false, err
		}
		keep := inventory.Policy == "Retain" || ref.Kind == "Backup" || slices.Contains(inventory.CASecrets, ref.UID) && !inventory.DeleteCA
		if ref.Kind == "Pooler" || ref.Kind == "ScheduledBackup" {
			keep = false
		}
		if keep {
			original := object.DeepCopyObject().(client.Object)
			owners := object.GetOwnerReferences()
			object.SetOwnerReferences(slices.DeleteFunc(slices.Clone(owners), func(owner metav1.OwnerReference) bool {
				if owner.UID == parent.UID {
					return true
				}
				return ref.Kind == "Backup" && slices.ContainsFunc(refs, func(root corev1.ObjectReference) bool { return root.Kind == "Cluster" && root.UID == owner.UID })
			}))
			changed := len(object.GetOwnerReferences()) != len(owners)
			if cluster, ok := object.(*cnpgv1.Cluster); ok {
				changed = clustercore.RemoveBarmanWALArchiverPlugin(cluster) || changed
			}
			if changed {
				return false, pgreconcile.PatchLocked(ctx, f.client, original, object)
			}
			if object.GetDeletionTimestamp() != nil {
				return false, fmt.Errorf("retained resource %s %s is already deleting", ref.Kind, ref.Name)
			}
			continue
		}
		if object.GetDeletionTimestamp() != nil {
			return false, nil
		}
		if ref.Kind == "Cluster" {
			done, err := preserveClusterSnapshots(ctx, f.client, f.reader, ref, verify)
			if err != nil || !done {
				return false, err
			}
			if err := verify(ctx); err != nil {
				return false, err
			}
		}
		return false, pgreconcile.DeleteExact(ctx, f.client, object, client.PropagationPolicy(metav1.DeletePropagationForeground))
	}
	if err := verify(ctx); err != nil {
		return false, err
	}
	if inventory.Policy == "Delete" {
		cleanup := &BlueGreenCleanup{client: f.client, reader: f.reader}
		return cleanup.retirementDependentsAbsent(ctx, parent.Namespace, slices.DeleteFunc(slices.Clone(refs), func(ref corev1.ObjectReference) bool { return ref.Kind == "Backup" }))
	}
	return true, nil
}

func (f *ClusterFinalizer) save(ctx context.Context, parent *platformv1alpha1.PostgresCluster, inventory finalizationInventory) error {
	encoded, err := json.Marshal(inventory)
	if err != nil {
		return err
	}
	if len(encoded) > 180000 {
		return fmt.Errorf("finalization inventory exceeds the supported status size")
	}
	original := parent.DeepCopy()
	parent.Status.Finalization = inventory.DeepCopy()
	return f.client.Status().Patch(ctx, parent, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

func (f *ClusterFinalizer) load(ctx context.Context, parent *platformv1alpha1.PostgresCluster, ref corev1.ObjectReference, roots []corev1.ObjectReference, children databaseOwners) (client.Object, error) {
	object, err := loadExactRetirementRoot(ctx, f.reader, parent.Namespace, ref)
	if err != nil || object == nil {
		return object, err
	}
	owner := metav1.GetControllerOf(object)
	if value, ok := object.(*cnpgv1.Database); ok {
		valid := children.owns(object) && slices.ContainsFunc(roots, func(root corev1.ObjectReference) bool {
			return root.Kind == "Cluster" && root.Name == value.Spec.ClusterRef.Name
		})
		if !valid {
			return nil, fmt.Errorf("finalization Database %s has conflicting provenance", ref.Name)
		}
	}
	allowedOwner := owner == nil || owner.UID == parent.UID
	if owner != nil && ref.Kind == "Backup" {
		allowedOwner = allowedOwner || slices.ContainsFunc(roots, func(root corev1.ObjectReference) bool { return root.Kind == "Cluster" && root.UID == owner.UID })
	}
	if owner != nil && ref.Kind == "Secret" {
		allowedOwner = allowedOwner || slices.ContainsFunc(roots, func(root corev1.ObjectReference) bool { return root.Kind == "Certificate" && root.UID == owner.UID })
	}
	// Detached roots remain authorized by the inventory persisted while owned.
	if !allowedOwner && ref.Kind != "Database" {
		return nil, fmt.Errorf("finalization resource %s has a conflicting controller", ref.Name)
	}
	return object, nil
}

func (f *ClusterFinalizer) removeDeclaration(ctx context.Context, object client.Object, waitForWriter bool, verify func(context.Context) error) error {
	original := object.DeepCopyObject().(client.Object)
	var finalizer string
	var changed bool
	switch value := object.(type) {
	case *cnpgv1.Database:
		changed, finalizer = pgcnpg.RetainDatabase(value), cnpgutils.DatabaseFinalizerName
	case *cnpgv1.Publication:
		changed, finalizer = retainReplicationDeclaration(value), cnpgutils.PublicationFinalizerName
	case *cnpgv1.Subscription:
		changed, finalizer = retainReplicationDeclaration(value), cnpgutils.SubscriptionFinalizerName
	default:
		return fmt.Errorf("unsupported finalization declaration %T", object)
	}
	if err := verify(ctx); err != nil {
		return err
	}
	if changed {
		return pgreconcile.PatchLocked(ctx, f.client, original, object)
	}
	if object.GetDeletionTimestamp() == nil {
		return pgreconcile.DeleteExact(ctx, f.client, object)
	}
	if subscription, ok := object.(*cnpgv1.Subscription); ok && waitForWriter {
		return quiesceRetainedSubscription(ctx, f.client, subscription)
	}
	if publication, ok := object.(*cnpgv1.Publication); ok && waitForWriter {
		return wakeDeletingDeclaration(ctx, f.client, publication, &publication.Spec.Parameters, "publish_via_partition_root", publication.Status.ObservedGeneration, finalizer)
	}
	// The persisted Retain policy prohibits SQL reclamation. Release only CNPG's finalizer.
	object.SetFinalizers(slices.DeleteFunc(slices.Clone(object.GetFinalizers()), func(name string) bool { return name == finalizer }))
	if !equality.Semantic.DeepEqual(original, object) {
		return pgreconcile.PatchLocked(ctx, f.client, original, object)
	}
	return nil
}

func (f *ClusterFinalizer) discover(ctx context.Context, parent *platformv1alpha1.PostgresCluster, previous []corev1.ObjectReference) ([]corev1.ObjectReference, databaseOwners, error) {
	refs := slices.Clone(previous)
	environments := map[string]types.UID{}
	addEnvironment := func(ref *corev1.ObjectReference) error {
		if ref == nil || ref.Name == "" || ref.UID == "" {
			return nil
		}
		if ref.Namespace != "" && ref.Namespace != parent.Namespace || ref.Kind != "" && ref.Kind != "Cluster" || ref.APIVersion != "" && ref.APIVersion != cnpgv1.SchemeGroupVersion.String() {
			return fmt.Errorf("finalization environment reference has conflicting scope")
		}
		if !slices.ContainsFunc(previous, func(old corev1.ObjectReference) bool {
			return old.Kind == "Cluster" && old.UID == ref.UID && old.Name == ref.Name
		}) {
			actual := &cnpgv1.Cluster{}
			err := f.reader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: ref.Name}, actual)
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			if err == nil && (actual.UID != ref.UID || !metav1.IsControlledBy(actual, parent)) {
				return fmt.Errorf("finalization environment %s has conflicting ownership", ref.Name)
			}
		}
		copy := *ref
		copy.APIVersion = cnpgv1.SchemeGroupVersion.String()
		copy.Kind = "Cluster"
		copy.Namespace = parent.Namespace
		var err error
		refs, _, err = mergeCleanupReferences(refs, []corev1.ObjectReference{copy})
		return err
	}
	if err := addEnvironment(parent.Status.ProvisionerRef); err != nil {
		return nil, nil, err
	}
	for _, entry := range parent.Status.PostgresMajorUpgradeStatus {
		if entry.BlueGreen == nil || entry.BlueGreen.HistoryCompacted {
			continue
		}
		for _, env := range []*platformv1alpha1.BlueGreenEnvironmentStatus{entry.BlueGreen.Blue, entry.BlueGreen.Green} {
			if env != nil {
				if err := addEnvironment(&env.Ref); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	var clusters cnpgv1.ClusterList
	if err := f.reader.List(ctx, &clusters, client.InNamespace(parent.Namespace)); err != nil {
		return nil, nil, err
	}
	for i := range clusters.Items {
		if metav1.IsControlledBy(&clusters.Items[i], parent) {
			ref, err := resourceReference(&clusters.Items[i])
			if err != nil {
				return nil, nil, err
			}
			if err := addEnvironment(&ref); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, ref := range refs {
		if ref.Kind == "Cluster" {
			environments[ref.Name] = ref.UID
		}
	}
	children, err := finalizationDatabaseOwners(ctx, f.reader, parent)
	if err != nil {
		return nil, nil, err
	}
	objects, err := namespaceObjects(ctx, f.reader, parent.Namespace, &cnpgv1.DatabaseList{}, &cnpgv1.PublicationList{}, &cnpgv1.SubscriptionList{}, &cnpgv1.PoolerList{}, &cnpgv1.ScheduledBackupList{}, &cnpgv1.BackupList{}, &corev1.SecretList{}, &corev1.ConfigMapList{}, &certv1.CertificateList{})
	if err != nil {
		return nil, nil, err
	}
	var roots []client.Object
	for _, object := range objects {
		owned := metav1.IsControlledBy(object, parent)
		owner := metav1.GetControllerOf(object)
		switch value := object.(type) {
		case *corev1.Secret:
			owned = owned || copiedFinalizationCA(parent, value)
		case *cnpgv1.Database:
			owned = children.owns(object) && environments[value.Spec.ClusterRef.Name] != ""
		case *cnpgv1.Backup:
			owned = owned || owner == nil && slices.ContainsFunc(value.OwnerReferences, func(ref metav1.OwnerReference) bool {
				return ref.UID == environments[value.Spec.Cluster.Name] && ref.Kind == "Cluster" && ref.APIVersion == cnpgv1.SchemeGroupVersion.String() || ref.UID == parent.UID && ref.Kind == "PostgresCluster" && ref.APIVersion == platformv1alpha1.GroupVersion.String()
			}) || owner != nil && owner.UID == environments[value.Spec.Cluster.Name]
		}
		if !owned {
			continue
		}
		if certificate, ok := object.(*certv1.Certificate); ok {
			secret, err := certificateLeaf(ctx, f.reader, parent, certificate)
			if err != nil {
				return nil, nil, err
			}
			if secret != nil {
				roots = append(roots, secret)
			}
		}
		roots = append(roots, object)
	}
	// A new writer identity invalidates SQL proof even when its old name is reused.
	for _, object := range roots {
		if replicationDeclaration(object) {
			observed, err := resourceReference(object)
			if err != nil {
				return nil, nil, err
			}
			refs = slices.DeleteFunc(refs, func(ref corev1.ObjectReference) bool {
				return ref.Kind == observed.Kind && ref.Name == observed.Name && ref.Namespace == observed.Namespace && ref.UID != observed.UID
			})
		}
	}
	refs, err = mergeRetirementRoots(refs, nil, roots...)
	if err != nil {
		return nil, nil, err
	}
	// Preserve Backup metadata before either its logical or physical owner disappears.
	priority := func(ref corev1.ObjectReference) int {
		if ref.Kind == "Backup" {
			return 0
		}
		if ref.Kind == "Cluster" {
			return 2
		}
		return 1
	}
	slices.SortStableFunc(refs, func(a, b corev1.ObjectReference) int { return priority(a) - priority(b) })
	return refs, children, nil
}

func finalizationOwns(parent *platformv1alpha1.PostgresCluster, object metav1.Object) bool {
	if controller := metav1.GetControllerOf(object); controller != nil {
		return controller.UID == parent.UID && controller.Name == parent.Name && controller.Kind == "PostgresCluster" && controller.APIVersion == platformv1alpha1.GroupVersion.String()
	}
	if parent.DeletionTimestamp == nil || metav1.GetControllerOf(object) != nil {
		return false
	}
	inventory := parent.Status.Finalization
	if inventory == nil || inventory.Version != 1 || inventory.ParentUID != parent.UID || inventory.Policy != "Retain" {
		return false
	}
	target, ok := object.(client.Object)
	if !ok {
		return false
	}
	targetRef, err := resourceReference(target)
	if err != nil || targetRef.Kind != "Cluster" && targetRef.Kind != "Secret" && targetRef.Kind != "ConfigMap" {
		return false
	}
	return slices.ContainsFunc(inventory.Resources, func(ref corev1.ObjectReference) bool {
		return ref.UID == object.GetUID() && ref.Name == object.GetName() && ref.Namespace == object.GetNamespace() && ref.Namespace == parent.Namespace && ref.Kind == targetRef.Kind && ref.APIVersion == targetRef.APIVersion
	})
}

func copiedFinalizationCA(parent *platformv1alpha1.PostgresCluster, secret *corev1.Secret) bool {
	return secret.Namespace == parent.Namespace && secret.Labels["app.kubernetes.io/managed-by"] == "splunk-operator" && secret.Labels[tlsport.ClusterNameLabel] == parent.Name && isParentServerTLS(secret.Labels, parent) && secret.Type == corev1.SecretTypeOpaque && len(secret.Data) == 1 && len(secret.Data["ca.crt"]) > 0 && metav1.GetControllerOf(secret) == nil
}

func isSQLDeclaration(kind string) bool {
	return kind == "Database" || kind == "Publication" || kind == "Subscription"
}

func replicationDeclaration(object client.Object) bool {
	switch object.(type) {
	case *cnpgv1.Publication, *cnpgv1.Subscription:
		return true
	default:
		return false
	}
}

func (f *ClusterFinalizer) unverifiableReplication(ctx context.Context, parent *platformv1alpha1.PostgresCluster, refs []corev1.ObjectReference) (bool, error) {
	for _, ref := range refs {
		if ref.Kind != "Publication" && ref.Kind != "Subscription" {
			continue
		}
		object, err := loadExactRetirementRoot(ctx, f.reader, parent.Namespace, ref)
		if err != nil {
			return false, err
		}
		if object == nil {
			continue
		}
		for _, entry := range parent.Status.PostgresMajorUpgradeStatus {
			if entry.BlueGreen != nil && mvutypes.IsBlueGreenAttemptCleaned(entry) && object.GetLabels()[blueGreenAttemptLabel] == entry.BlueGreen.AttemptID {
				return true, nil
			}
		}
	}
	return false, nil
}
