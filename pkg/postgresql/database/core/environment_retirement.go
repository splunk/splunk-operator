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
package core

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EnvironmentRetirer keeps declaration removal separate from ordinary provisioning.
type EnvironmentRetirer interface {
	RetireEnvironment(context.Context, dbtypes.EnvironmentRetirement, func(context.Context) error) (bool, error)
}

// reconcileRetirement also runs before child deletion and terminal privilege failures.
func (r *databaseReconciliation) reconcileRetirement(ctx context.Context) (ctrl.Result, error, bool) {
	rc, child := r.rc, r.db
	if rc.APIReader == nil {
		return ctrl.Result{}, nil, false
	}
	parent, err := readEndpointParent(ctx, rc.APIReader, child)
	if apierrors.IsNotFound(err) {
		return ctrl.Result{}, nil, false
	}
	if err != nil {
		return ctrl.Result{}, err, true
	}
	if len(parent.Status.PostgresMajorUpgradeStatus) == 0 {
		return ctrl.Result{}, nil, false
	}
	entry := *mvutypes.LatestAttempt(parent.Status.PostgresMajorUpgradeStatus)
	bg := entry.BlueGreen
	if bg == nil || bg.Cleanup == nil || bg.Cleanup.State == platformv1alpha1.BlueGreenCleanupStateRetained || bg.Cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleaned {
		return ctrl.Result{}, nil, false
	}
	request, err := databaseRetirementRequest(parent, child, entry)
	if err != nil {
		return ctrl.Result{}, err, true
	}
	if len(request.Databases) == 0 {
		return ctrl.Result{}, nil, false
	}
	if rc.EnvironmentRetirer == nil {
		return ctrl.Result{}, fmt.Errorf("database environment retirement is not configured"), true
	}
	verify := func(ctx context.Context) error {
		_, err := verifyRetirementRequest(ctx, rc.APIReader, parent, child, entry)
		return err
	}
	done, err := rc.EnvironmentRetirer.RetireEnvironment(ctx, request, verify)
	if err != nil {
		return ctrl.Result{}, err, true
	}
	if !done {
		return ctrl.Result{RequeueAfter: retryDelay}, nil, true
	}
	return acknowledgeRetirement(ctx, rc, parent, child, bg, request, func(ctx context.Context) (*platformv1alpha1.PostgresDatabase, error) {
		return verifyRetirementRequest(ctx, rc.APIReader, parent, child, entry)
	})
}

// acknowledgeRetirement writes only child-owned proof after an independent absence check.
func acknowledgeRetirement(ctx context.Context, rc *ReconcileContext, parent *platformv1alpha1.PostgresCluster, child *platformv1alpha1.PostgresDatabase, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, request dbtypes.EnvironmentRetirement, verify func(context.Context) (*platformv1alpha1.PostgresDatabase, error)) (ctrl.Result, error, bool) {
	current, err := verify(ctx)
	if err != nil {
		return ctrl.Result{}, err, true
	}
	acknowledged := true
	for _, row := range bg.Cleanup.RetirementAcknowledgements {
		if row.PostgresDatabaseName == child.Name && row.PostgresDatabaseUID == string(child.UID) {
			acknowledged = acknowledged && mvutypes.RetirementAcknowledged(parent.UID, bg.AttemptID, bg.Cleanup, row, current)
		}
	}
	if acknowledged {
		*child = *current
		return ctrl.Result{}, nil, false
	}
	before := current.DeepCopy()
	proof := &platformv1alpha1.PostgresDatabaseEnvironmentRetirementStatus{
		ParentUID: parent.UID, AttemptID: bg.AttemptID, RetirementGeneration: *bg.Cleanup.RetirementGeneration,
		ObservedDatabaseGeneration: child.Generation, EnvironmentRef: request.Environment,
	}
	now := metav1.Now()
	for _, row := range request.Databases {
		proof.Databases = append(proof.Databases, platformv1alpha1.DatabaseEnvironmentRetirementStatus{
			DatabaseName: row.DatabaseName, Resources: row.Resources, AcknowledgedAt: now.DeepCopy(),
		})
	}
	current.Status.EnvironmentRetirement = proof
	if err := rc.Client.Status().Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, fmt.Errorf("persisting database retirement acknowledgement: %w", err), true
	}
	return ctrl.Result{RequeueAfter: retryDelay}, nil, true
}

// databaseRetirementRequest selects only this child's frozen environment declarations.
func databaseRetirementRequest(parent *platformv1alpha1.PostgresCluster, child *platformv1alpha1.PostgresDatabase, entry platformv1alpha1.PostgresMajorUpgradeStatus) (dbtypes.EnvironmentRetirement, error) {
	request := dbtypes.EnvironmentRetirement{Child: corev1.ObjectReference{
		APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PostgresDatabase", Name: child.Name, Namespace: child.Namespace, UID: child.UID,
	}}
	bg := entry.BlueGreen
	_, environment, err := mvutypes.BlueGreenRetirementEnvironments(entry)
	if err != nil {
		return request, err
	}
	if environment == nil && bg.Cleanup != nil && bg.Cleanup.EnvironmentRef == nil && len(bg.Cleanup.RetirementAcknowledgements) == 0 {
		return request, nil
	}
	if bg.Cleanup == nil || bg.Gates == nil || bg.Gates.Cleanup == nil || bg.Gates.Cleanup.ConsumedAt == nil ||
		bg.Cleanup.RetirementGeneration == nil || *bg.Cleanup.RetirementGeneration < 1 || bg.Cleanup.StartedAt == nil ||
		environment == nil || !equality.Semantic.DeepEqual(environment, bg.Cleanup.EnvironmentRef) || environment.Namespace != parent.Namespace {
		return request, fmt.Errorf("database retirement requires a latched exact environment request")
	}
	request.Environment = *environment
	for _, row := range bg.Cleanup.RetirementAcknowledgements {
		if row.PostgresDatabaseName != child.Name || row.PostgresDatabaseUID != string(child.UID) {
			continue
		}
		if row.PostgresDatabaseGeneration != child.Generation || row.PostgresDatabaseAbsentAt != nil {
			return request, fmt.Errorf("database retirement awaits the current child generation")
		}
		request.Databases = append(request.Databases, dbtypes.DatabaseRetirement{DatabaseName: row.DatabaseName, Resources: row.Resources})
	}
	return request, nil
}

// verifyRetirementRequest refreshes intent, child, authority, and routing before each mutation.
func verifyRetirementRequest(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster, child *platformv1alpha1.PostgresDatabase, entry platformv1alpha1.PostgresMajorUpgradeStatus) (*platformv1alpha1.PostgresDatabase, error) {
	currentParent, err := readEndpointParent(ctx, reader, child)
	if err != nil {
		return nil, err
	}
	if currentParent.UID != parent.UID || currentParent.DeletionTimestamp != nil || len(currentParent.Status.PostgresMajorUpgradeStatus) == 0 {
		return nil, fmt.Errorf("parent changed during database retirement")
	}
	currentEntry := *mvutypes.LatestAttempt(currentParent.Status.PostgresMajorUpgradeStatus)
	if currentEntry.BlueGreen == nil || currentEntry.BlueGreen.Gates == nil || currentEntry.BlueGreen.AttemptID != entry.BlueGreen.AttemptID ||
		!equality.Semantic.DeepEqual(currentEntry.BlueGreen.Cleanup, entry.BlueGreen.Cleanup) ||
		!equality.Semantic.DeepEqual(currentEntry.BlueGreen.Gates.Cleanup, entry.BlueGreen.Gates.Cleanup) {
		return nil, fmt.Errorf("retirement intent changed before mutation")
	}
	currentChild := &platformv1alpha1.PostgresDatabase{}
	if err := reader.Get(ctx, client.ObjectKeyFromObject(child), currentChild); err != nil {
		return nil, err
	}
	if currentChild.UID != child.UID || currentChild.Generation != child.Generation || !equality.Semantic.DeepEqual(currentChild.Spec, child.Spec) ||
		!equality.Semantic.DeepEqual(currentChild.DeletionTimestamp, child.DeletionTimestamp) {
		return nil, fmt.Errorf("child changed during database retirement")
	}
	if _, err := databaseRetirementRequest(currentParent, currentChild, currentEntry); err != nil {
		return nil, err
	}
	if err := verifyRetirementAuthority(ctx, reader, currentParent, currentEntry); err != nil {
		return nil, err
	}
	return currentChild, nil
}

// verifyRetirementAuthority requires canonical traffic to stay on the other exact environment.
func verifyRetirementAuthority(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster, entry platformv1alpha1.PostgresMajorUpgradeStatus) error {
	authority, retirement, err := mvutypes.BlueGreenRetirementEnvironments(entry)
	if err != nil {
		return err
	}
	if authority == nil {
		return fmt.Errorf("retirement requires the authoritative environment identity")
	}
	_, _, err = pgcnpg.ObserveRetirementEnvironments(ctx, reader, parent, *authority, retirement)
	return err
}
