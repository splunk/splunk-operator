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
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	clusterCnpg "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// teardownSQL is the idempotent superuser SQL boundary for attempt teardown.
type teardownSQL interface {
	// RemoveSourceSlots reports whether no attempt slot remains on blue.
	RemoveSourceSlots(ctx context.Context, source *cnpgv1.Cluster, slots teardownSlotInventory) (bool, error)
	// DropAttemptRoles drops the control schema, then revokes and drops roles.
	DropAttemptRoles(ctx context.Context, cluster *cnpgv1.Cluster, databases, roles []string) error
	DatabaseExists(ctx context.Context, source *cnpgv1.Cluster, database string) (bool, error)
	StopReplication(ctx context.Context, source *cnpgv1.Cluster, role string, slots []string) error
	RemovePublications(ctx context.Context, source *cnpgv1.Cluster, logicalName string, databases []string) error
}

type teardownSlotInventory struct {
	names, databases []string
	systemID         string
}

const teardownSubscriberAnnotation = "platform.splunk.com/blue-green-subscriber"

// BlueGreenReplicationTeardown removes a pre-commit attempt's replication and
// temporary credentials after blue access is restored. Blue must be fully
// cleaned. Retained green loses only its replication source; its remaining
// state is deleted with the environment during cleanup. Artifacts are found by
// attempt label and ownership. SQL reclamation uses the recorded environment
// identity instead of CNPG's asynchronous name-only finalizers.
//
// Post-commit teardown needs the control identity to reach fenced blue and is
// not supported.
type BlueGreenReplicationTeardown struct {
	client client.Client
	reader client.Reader
	key    types.NamespacedName
	owner  *platformv1alpha1.PostgresCluster
	sql    teardownSQL
}

func NewBlueGreenReplicationTeardown(c client.Client, reader client.Reader, key types.NamespacedName, owner *platformv1alpha1.PostgresCluster) (*BlueGreenReplicationTeardown, error) {
	if c == nil || reader == nil || owner == nil || owner.UID == "" || key.Name == "" || key.Namespace == "" || key.Name != owner.Name || key.Namespace != owner.Namespace {
		return nil, errors.New("blue/green teardown requires clients and a persisted parent identity")
	}
	live := liveReadClient{Client: c, reader: reader}
	return &BlueGreenReplicationTeardown{client: c, reader: reader, key: key, owner: owner,
		sql: newPostgresTeardownSQL(live, key.Name, owner)}, nil
}

// RemoveReplication removes Subscriptions, Publications, blue slots, and
// green's replication source, in that order.
func (t *BlueGreenReplicationTeardown) RemoveReplication(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if err := t.validate(status); err != nil {
		return false, err
	}
	blue, ready, err := t.blue(ctx, status)
	if err != nil || !ready {
		return false, err
	}
	green, err := t.AttemptGreen(ctx, status)
	if err != nil {
		return false, err
	}
	if green == nil && status.Green != nil {
		gone, err := t.greenInstancesGone(ctx, status)
		if err != nil || !gone {
			return false, err
		}
	}
	// CNPG resolves SQL finalizer targets by name, not UID. Retain SQL before
	// deletion and clean blue ourselves; a delayed finalizer cannot touch a
	// replacement environment. Green's SQL is removed with green at cleanup.
	var slots teardownSlotInventory
	if blue != nil {
		var recorded bool
		slots, recorded, err = t.sourceSlotInventory(ctx, status, green)
		if err != nil || recorded {
			return false, err
		}
		if err := t.sql.StopReplication(ctx, blue, replicationRoleName(status.AttemptID), slots.names); err != nil {
			return false, err
		}
	}
	subscriptionsGone, err := t.deleteOwned(ctx, &cnpgv1.SubscriptionList{}, status, func(object client.Object) error {
		if green != nil {
			return t.releaseRetainedFinalizer(ctx, object, cnpgutils.SubscriptionFinalizerName)
		}
		return t.releaseMissingGreen(ctx, status, object.(*cnpgv1.Subscription))
	})
	if err != nil || !subscriptionsGone {
		return false, err
	}
	publicationsGone, err := t.deleteOwned(ctx, &cnpgv1.PublicationList{}, status, func(object client.Object) error {
		return t.releaseRetainedFinalizer(ctx, object, cnpgutils.PublicationFinalizerName)
	})
	if err != nil || !publicationsGone {
		return false, err
	}
	if blue != nil {
		// Check the frozen inventory even if a CR disappeared before its SQL
		// creation was recorded. Applied=false is never proof of SQL absence.
		if err := t.sql.RemovePublications(ctx, blue, t.key.Name, t.databaseNames(status)); err != nil {
			return false, err
		}
		slotsGone, err := t.sql.RemoveSourceSlots(ctx, blue, slots)
		if err != nil || !slotsGone {
			return false, err
		}
	}
	if green == nil {
		return true, nil
	}
	return t.removeReplicationSource(ctx, green)
}

// PostgreSQL includes the subscriber's system ID in table-copy slot names.
// Keep the UID-bound, attempt-scoped receipt on the persisted parent: unlike
// credential Secrets, it survives the credential-deletion confirmation pass.
// Read legacy Secret receipts so already-started teardowns can migrate safely.
// A newly saved receipt requires another pass before external cleanup proceeds.
func (t *BlueGreenReplicationTeardown) sourceSlotInventory(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, green *cnpgv1.Cluster) (teardownSlotInventory, bool, error) {
	inventory := teardownSlotInventory{names: t.slotNames(status), databases: t.databaseNames(status)}
	parent := &platformv1alpha1.PostgresCluster{}
	if err := t.reader.Get(ctx, t.key, parent); err != nil {
		return inventory, false, err
	}
	if parent.UID != t.owner.UID {
		return inventory, false, fmt.Errorf("%w: subscriber receipt parent was replaced", mvutypes.ErrUpgradeFlowFailed)
	}
	prefix := status.AttemptID + "/"
	saved := ""
	if receipt := parent.Annotations[teardownSubscriberAnnotation]; strings.HasPrefix(receipt, prefix) {
		saved = strings.TrimPrefix(receipt, prefix)
	}
	secret := &corev1.Secret{}
	err := t.reader.Get(ctx, types.NamespacedName{Namespace: t.key.Namespace, Name: replicationSecretName(t.key.Name, status.AttemptID)}, secret)
	if err != nil && !apierrors.IsNotFound(err) {
		return inventory, false, err
	}
	if err == nil && (secret.Labels[blueGreenAttemptLabel] != status.AttemptID || !metav1.IsControlledBy(secret, t.owner)) {
		return inventory, false, fmt.Errorf("%w: subscriber identity Secret does not belong to the attempt", mvutypes.ErrUpgradeFlowFailed)
	}
	if legacy := secret.Annotations[teardownSubscriberAnnotation]; legacy != "" {
		if saved != "" && saved != legacy {
			return inventory, false, fmt.Errorf("%w: conflicting subscriber identity receipts", mvutypes.ErrUpgradeFlowFailed)
		}
		saved = legacy
	}
	uid := ""
	if saved != "" {
		var valid bool
		uid, inventory.systemID, valid = strings.Cut(saved, "/")
		if !valid || uid == "" || status.Green != nil && string(status.Green.Ref.UID) != uid ||
			green != nil && (string(green.UID) != uid || green.Status.SystemID != "" && green.Status.SystemID != inventory.systemID) {
			return inventory, false, fmt.Errorf("%w: saved subscriber identity does not match green", mvutypes.ErrUpgradeFlowFailed)
		}
	} else if green != nil {
		uid, inventory.systemID = string(green.UID), green.Status.SystemID
	}
	if inventory.systemID == "" {
		// SQL must refuse unidentified synchronization slots.
		return inventory, false, nil
	}
	if id, parseErr := strconv.ParseUint(inventory.systemID, 10, 64); parseErr != nil || id == 0 || strconv.FormatUint(id, 10) != inventory.systemID {
		return inventory, false, fmt.Errorf("%w: invalid subscriber system ID", mvutypes.ErrUpgradeFlowFailed)
	}
	receipt := prefix + uid + "/" + inventory.systemID
	if parent.Annotations[teardownSubscriberAnnotation] == receipt {
		return inventory, false, nil
	}
	original := parent.DeepCopy()
	if parent.Annotations == nil {
		parent.Annotations = make(map[string]string)
	}
	parent.Annotations[teardownSubscriberAnnotation] = receipt
	if err := t.client.Patch(ctx, parent, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return inventory, false, fmt.Errorf("recording subscriber identity: %w", err)
	}
	return inventory, true, nil
}

// A missing Cluster alone does not prove that its old instances stopped.
// Wait for every pod owned by the exact recorded UID, including terminating
// and unready pods. Preserve finalizers belonging to other controllers.
func (t *BlueGreenReplicationTeardown) releaseMissingGreen(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, subscription *cnpgv1.Subscription) error {
	green, err := t.AttemptGreen(ctx, status)
	if err != nil || green != nil {
		return err
	}
	gone, err := t.greenInstancesGone(ctx, status)
	if err != nil || !gone {
		return err
	}
	return t.releaseRetainedFinalizer(ctx, subscription, cnpgutils.SubscriptionFinalizerName)
}

// Retain delegates SQL cleanup to us. Release only CNPG's finalizer after
// quiescence; do not depend on its controller still running to acknowledge it.
func (t *BlueGreenReplicationTeardown) releaseRetainedFinalizer(ctx context.Context, object client.Object, finalizer string) error {
	original := object.DeepCopyObject().(client.Object)
	object.SetFinalizers(slices.DeleteFunc(slices.Clone(object.GetFinalizers()), func(name string) bool { return name == finalizer }))
	if len(original.GetFinalizers()) == len(object.GetFinalizers()) {
		return nil
	}
	return t.client.Patch(ctx, object, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

func (t *BlueGreenReplicationTeardown) greenInstancesGone(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if status.Green == nil || status.Green.Ref.UID == "" {
		return false, fmt.Errorf("%w: orphaned Subscription has no recorded green identity", mvutypes.ErrUpgradeFlowFailed)
	}
	pods := &corev1.PodList{}
	if err := t.reader.List(ctx, pods, client.InNamespace(t.key.Namespace)); err != nil {
		return false, err
	}
	for _, pod := range pods.Items {
		if owner := metav1.GetControllerOf(&pod); owner != nil && owner.UID == status.Green.Ref.UID {
			return false, nil
		}
	}
	return true, nil
}

// RecoverGreen records a discovered candidate before cancellation performs any
// external step. Returning changed lets the caller persist it and retry.
func (t *BlueGreenReplicationTeardown) RecoverGreen(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if err := t.validate(status); err != nil {
		return false, err
	}
	green, err := t.AttemptGreen(ctx, status)
	if err != nil || green == nil || status.Green != nil && status.Green.Ref.Name != "" {
		return false, err
	}
	status.Green = &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Namespace: green.Namespace, Name: green.Name, UID: green.UID}}
	return true, nil
}

// AttemptGreen returns the attempt's green CNPG Cluster, or nil when it was
// never created. Green is created before its reference is recorded, so an
// unrecorded green is found by its fixed name and must carry the attempt
// label and parent ownership.
func (t *BlueGreenReplicationTeardown) AttemptGreen(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (*cnpgv1.Cluster, error) {
	if status.Green != nil && status.Green.Ref.Name != "" {
		return t.environment(ctx, status.Green)
	}
	green := &cnpgv1.Cluster{}
	err := t.reader.Get(ctx, types.NamespacedName{Namespace: t.key.Namespace, Name: candidateClusterName(t.key.Name, status.AttemptID)}, green)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if green.UID == "" || green.Labels[blueGreenAttemptLabel] != status.AttemptID || !metav1.IsControlledBy(green, t.owner) {
		return nil, fmt.Errorf("%w: CNPG Cluster %s has the attempt's green name but does not belong to it", mvutypes.ErrUpgradeFlowFailed, green.Name)
	}
	return green, nil
}

// RemoveCredentials drops the attempt roles and control schema on blue, then
// deletes the attempt Secrets. A remaining subscription still uses the
// replication role, so it fails closed. Green's control role is left with
// the retained environment: its only password is in the deleted Secret.
func (t *BlueGreenReplicationTeardown) RemoveCredentials(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if err := t.validate(status); err != nil {
		return false, err
	}
	subscriptions := &cnpgv1.SubscriptionList{}
	if err := t.reader.List(ctx, subscriptions, client.InNamespace(t.key.Namespace), client.MatchingLabels{blueGreenAttemptLabel: status.AttemptID}); err != nil {
		return false, err
	}
	if len(subscriptions.Items) > 0 {
		return false, fmt.Errorf("%w: attempt credentials cannot be removed while a logical subscription remains", mvutypes.ErrUpgradeFlowFailed)
	}
	blue, ready, err := t.blue(ctx, status)
	if err != nil || !ready {
		return false, err
	}
	if blue != nil {
		green, err := t.AttemptGreen(ctx, status)
		if err != nil {
			return false, err
		}
		slots, recorded, err := t.sourceSlotInventory(ctx, status, green)
		if err != nil || recorded {
			return false, err
		}
		if err := t.sql.StopReplication(ctx, blue, replicationRoleName(status.AttemptID), slots.names); err != nil {
			return false, err
		}
		if gone, err := t.sql.RemoveSourceSlots(ctx, blue, slots); err != nil || !gone {
			return false, err
		}
		// A later attempt's preflight rejects the reserved control schema.
		roles := []string{replicationRoleName(status.AttemptID), cutoverRoleName(status.AttemptID)}
		if err := t.sql.DropAttemptRoles(ctx, blue, t.databaseNames(status), roles); err != nil {
			return false, err
		}
	}
	return t.deleteSecrets(ctx, status.AttemptID)
}

// blue returns authoritative blue and whether teardown can proceed. An
// attempt that never recorded blue has no blue artifacts; a recorded blue
// must still exist and have a ready primary to prove its artifacts removed.
func (t *BlueGreenReplicationTeardown) blue(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (*cnpgv1.Cluster, bool, error) {
	if status.Blue == nil || status.Blue.Ref.Name == "" {
		return nil, true, nil
	}
	blue, err := t.environment(ctx, status.Blue)
	if err != nil {
		return nil, false, err
	}
	if blue == nil {
		return nil, false, fmt.Errorf("%w: authoritative blue CNPG Cluster is missing", mvutypes.ErrUpgradeFlowFailed)
	}
	return blue, clusterCnpg.PrimaryReady(blue), nil
}

func (t *BlueGreenReplicationTeardown) validate(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if t == nil || t.client == nil || t.reader == nil || t.owner == nil || t.sql == nil {
		return fmt.Errorf("%w: blue/green teardown wiring is incomplete", mvutypes.ErrUpgradeFlowFailed)
	}
	if status == nil || status.AttemptID == "" {
		return fmt.Errorf("%w: blue/green teardown requires an attempt identity", mvutypes.ErrUpgradeFlowFailed)
	}
	if mvutypes.CutoverStageOfStatus(status).Committed() {
		return fmt.Errorf("%w: pre-commit teardown cannot run after the endpoint commit", mvutypes.ErrUpgradeFlowFailed)
	}
	return nil
}

// deleteOwned first retains SQL, then deletes the exact CR. CNPG must never
// asynchronously execute SQL using its name-only ClusterRef during teardown.
func (t *BlueGreenReplicationTeardown) deleteOwned(ctx context.Context, list client.ObjectList, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, stuck func(client.Object) error) (bool, error) {
	if err := t.reader.List(ctx, list, client.InNamespace(t.key.Namespace), client.MatchingLabels{blueGreenAttemptLabel: status.AttemptID}); err != nil {
		return false, err
	}
	items, err := meta.ExtractList(list)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		object, ok := item.(client.Object)
		if !ok {
			return false, fmt.Errorf("%w: teardown list item %T is not a Kubernetes object", mvutypes.ErrUpgradeFlowFailed, item)
		}
		if !metav1.IsControlledBy(object, t.owner) {
			return false, fmt.Errorf("%w: %T %s carries this attempt's label but is not owned by its PostgresCluster", mvutypes.ErrUpgradeFlowFailed, object, object.GetName())
		}
		if err := t.validateReplicationObject(status, object); err != nil {
			return false, err
		}
		original := object.DeepCopyObject().(client.Object)
		changed := false
		switch resource := object.(type) {
		case *cnpgv1.Subscription:
			changed = resource.Spec.ReclaimPolicy != cnpgv1.SubscriptionReclaimRetain
			resource.Spec.ReclaimPolicy = cnpgv1.SubscriptionReclaimRetain
		case *cnpgv1.Publication:
			changed = resource.Spec.ReclaimPolicy != cnpgv1.PublicationReclaimRetain
			resource.Spec.ReclaimPolicy = cnpgv1.PublicationReclaimRetain
		}
		if changed {
			if err := t.client.Patch(ctx, object, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
				return false, err
			}
			continue
		}
		if object.GetDeletionTimestamp() != nil {
			if stuck != nil {
				if err := stuck(object); err != nil {
					return false, err
				}
			}
			continue
		}
		if mvutypes.CutoverStageOfStatus(status).Committed() {
			// Wait for CNPG apply; cancellation's database probe cannot reach fenced blue.
			switch object := object.(type) {
			case *cnpgv1.Publication:
				if object.Status.ObservedGeneration != object.Generation || object.Status.Applied == nil || !*object.Status.Applied {
					continue
				}
			case *cnpgv1.Subscription:
				if object.Status.ObservedGeneration != object.Generation || object.Status.Applied == nil || !*object.Status.Applied {
					continue
				}
			}
		}
		if publication, ok := object.(*cnpgv1.Publication); ok &&
			(publication.Status.ObservedGeneration != publication.Generation || publication.Status.Applied == nil || !*publication.Status.Applied) {
			// Wait until CNPG has finished its apply operation before removing
			// the CR. Otherwise an in-flight CREATE could finish after our DROP.
			// A positively missing database cannot contain that publication.
			blue, _, err := t.blue(ctx, status)
			if err != nil {
				return false, err
			}
			exists, err := t.sql.DatabaseExists(ctx, blue, publication.Spec.DBName)
			if err != nil {
				return false, err
			}
			if exists {
				continue
			}
		}
		if err := t.deleteExact(ctx, object); err != nil {
			return false, err
		}
	}
	return len(items) == 0, nil
}

func (t *BlueGreenReplicationTeardown) validateReplicationObject(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, object client.Object) error {
	var valid bool
	switch resource := object.(type) {
	case *cnpgv1.Publication:
		valid = status.Blue != nil && resource.Spec.ClusterRef.Name == status.Blue.Ref.Name &&
			slices.Contains(t.databaseNames(status), resource.Spec.DBName) && resource.Spec.Name == publicationName(t.key.Name, resource.Spec.DBName)
	case *cnpgv1.Subscription:
		greenName := candidateClusterName(t.key.Name, status.AttemptID)
		if status.Green != nil {
			greenName = status.Green.Ref.Name
		}
		valid = resource.Spec.ClusterRef.Name == greenName && slices.Contains(t.databaseNames(status), resource.Spec.DBName) &&
			resource.Spec.Name == subscriptionName(t.key.Name, resource.Spec.DBName) && resource.Spec.Parameters["slot_name"] == replicationSlotName(t.key.Name, resource.Spec.DBName)
	}
	if !valid {
		return fmt.Errorf("%w: %T %s does not match the recorded replication inventory", mvutypes.ErrUpgradeFlowFailed, object, object.GetName())
	}
	return nil
}

func (t *BlueGreenReplicationTeardown) deleteSecrets(ctx context.Context, attemptID string) (bool, error) {
	gone := true
	for _, name := range []string{replicationSecretName(t.key.Name, attemptID), controlSecretName(t.key.Name, attemptID)} {
		secret := &corev1.Secret{}
		err := t.reader.Get(ctx, types.NamespacedName{Namespace: t.key.Namespace, Name: name}, secret)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if secret.Labels[blueGreenAttemptLabel] != attemptID || !metav1.IsControlledBy(secret, t.owner) {
			return false, fmt.Errorf("%w: Secret %s does not belong to this attempt", mvutypes.ErrUpgradeFlowFailed, name)
		}
		gone = false
		if secret.DeletionTimestamp == nil {
			if err := t.deleteExact(ctx, secret); err != nil {
				return false, err
			}
		}
	}
	return gone, nil
}

// deleteExact never deletes a same-name replacement.
func (t *BlueGreenReplicationTeardown) deleteExact(ctx context.Context, object client.Object) error {
	uid := object.GetUID()
	rv := object.GetResourceVersion()
	if err := t.client.Delete(ctx, object, client.Preconditions{UID: &uid, ResourceVersion: &rv}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting %T %s: %w", object, object.GetName(), err)
	}
	return nil
}

// environment returns the recorded CNPG Cluster, or nil when it was never
// recorded or no longer exists. A same-name replacement fails closed.
func (t *BlueGreenReplicationTeardown) environment(ctx context.Context, environment *platformv1alpha1.BlueGreenEnvironmentStatus) (*cnpgv1.Cluster, error) {
	if environment == nil || environment.Ref.Name == "" {
		return nil, nil
	}
	if environment.Ref.Namespace != "" && environment.Ref.Namespace != t.key.Namespace {
		return nil, fmt.Errorf("%w: recorded environment is in another namespace", mvutypes.ErrUpgradeFlowFailed)
	}
	cluster := &cnpgv1.Cluster{}
	err := t.reader.Get(ctx, types.NamespacedName{Namespace: t.key.Namespace, Name: environment.Ref.Name}, cluster)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if environment.Ref.UID == "" || cluster.UID != environment.Ref.UID || !metav1.IsControlledBy(cluster, t.owner) {
		return nil, fmt.Errorf("%w: CNPG Cluster %s does not match the recorded environment", mvutypes.ErrUpgradeFlowFailed, cluster.Name)
	}
	return cluster, nil
}

// removeReplicationSource keeps the import source, which uses blue's superuser
// Secret rather than an attempt credential.
func (t *BlueGreenReplicationTeardown) removeReplicationSource(ctx context.Context, green *cnpgv1.Cluster) (bool, error) {
	if !slices.ContainsFunc(green.Spec.ExternalClusters, isReplicationSource) {
		return true, nil
	}
	original := green.DeepCopy()
	green.Spec.ExternalClusters = slices.DeleteFunc(slices.Clone(green.Spec.ExternalClusters), isReplicationSource)
	if err := t.client.Patch(ctx, green, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return false, fmt.Errorf("removing green replication source: %w", err)
	}
	return false, nil
}

func isReplicationSource(cluster cnpgv1.ExternalCluster) bool {
	return cluster.Name == candidateSourceExternalClusterName
}

func (t *BlueGreenReplicationTeardown) databaseNames(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) []string {
	names := make([]string, 0, len(status.DatabaseReplication))
	for _, database := range status.DatabaseReplication {
		names = append(names, database.DatabaseName)
	}
	return names
}

func (t *BlueGreenReplicationTeardown) slotNames(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) []string {
	names := make([]string, 0, len(status.DatabaseReplication))
	for _, database := range status.DatabaseReplication {
		names = append(names, replicationSlotName(t.key.Name, database.DatabaseName))
	}
	return names
}
